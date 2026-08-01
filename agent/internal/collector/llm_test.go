package collector

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sentinel/agent/internal/config"
	"sentinel/agent/internal/models"
)

// Trimmed but format-faithful samples of what each runtime serves on
// /metrics, including the HELP/TYPE comment lines and (for vLLM) the
// model_name label carried on every series.
const vllmMetrics = `
# HELP vllm:num_requests_running Number of requests currently running on GPU.
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{model_name="meta-llama/Llama-3.1-8B-Instruct"} 3.0
# HELP vllm:num_requests_waiting Number of requests waiting to be processed.
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting{model_name="meta-llama/Llama-3.1-8B-Instruct"} 7.0
# HELP vllm:gpu_cache_usage_perc GPU KV-cache usage. 1 means 100 percent usage.
# TYPE vllm:gpu_cache_usage_perc gauge
vllm:gpu_cache_usage_perc{model_name="meta-llama/Llama-3.1-8B-Instruct"} 0.42
vllm:prompt_tokens_total{model_name="meta-llama/Llama-3.1-8B-Instruct"} 1000.0
vllm:generation_tokens_total{model_name="meta-llama/Llama-3.1-8B-Instruct"} 500.0
vllm:gpu_prefix_cache_queries_total{model_name="meta-llama/Llama-3.1-8B-Instruct"} 800.0
vllm:gpu_prefix_cache_hits_total{model_name="meta-llama/Llama-3.1-8B-Instruct"} 600.0
vllm:num_preemptions_total{model_name="meta-llama/Llama-3.1-8B-Instruct"} 2.0
vllm:time_to_first_token_seconds_sum{model_name="meta-llama/Llama-3.1-8B-Instruct"} 10.0
vllm:time_to_first_token_seconds_count{model_name="meta-llama/Llama-3.1-8B-Instruct"} 100.0
vllm:time_per_output_token_seconds_sum{model_name="meta-llama/Llama-3.1-8B-Instruct"} 5.0
vllm:time_per_output_token_seconds_count{model_name="meta-llama/Llama-3.1-8B-Instruct"} 250.0
`

const llamaCppMetrics = `
# HELP llamacpp:prompt_tokens_total Number of prompt tokens processed.
# TYPE llamacpp:prompt_tokens_total counter
llamacpp:prompt_tokens_total 2048
# HELP llamacpp:tokens_predicted_total Number of generation tokens processed.
# TYPE llamacpp:tokens_predicted_total counter
llamacpp:tokens_predicted_total 1024
# HELP llamacpp:kv_cache_usage_ratio KV-cache usage. 1 means 100 percent usage.
# TYPE llamacpp:kv_cache_usage_ratio gauge
llamacpp:kv_cache_usage_ratio 0.25
llamacpp:kv_cache_tokens 4096
llamacpp:requests_processing 2
llamacpp:requests_deferred 1
llamacpp:predicted_tokens_seconds 40
`

// Metric names as emitted by vLLM v0.9+ (captured from a live server running
// Qwen3.6-35B on a GB10). Relative to the older naming in vllmMetrics above,
// the gpu_ prefixes are gone and time_per_output_token became
// inter_token_latency. Both spellings must map, since a mismatch silently
// yields nil — visually identical to a runtime that can't report the metric.
const vllmModernMetrics = `
vllm:num_requests_running{model_name="qwen35B"} 2.0
vllm:num_requests_waiting{model_name="qwen35B"} 4.0
vllm:kv_cache_usage_perc{model_name="qwen35B"} 0.63
vllm:prompt_tokens_total{model_name="qwen35B"} 2000.0
vllm:generation_tokens_total{model_name="qwen35B"} 900.0
vllm:prefix_cache_queries_total{model_name="qwen35B"} 500.0
vllm:prefix_cache_hits_total{model_name="qwen35B"} 410.0
vllm:external_prefix_cache_hits_total{model_name="qwen35B"} 7.0
vllm:num_preemptions_total{model_name="qwen35B"} 1.0
vllm:time_to_first_token_seconds_sum{model_name="qwen35B"} 20.0
vllm:time_to_first_token_seconds_count{model_name="qwen35B"} 50.0
vllm:inter_token_latency_seconds_sum{model_name="qwen35B"} 8.0
vllm:inter_token_latency_seconds_count{model_name="qwen35B"} 400.0
`

func TestMapVLLMModernMetricNames(t *testing.T) {
	var s models.LLMSample
	var ctr llmCounterState
	mapVLLM(parseProm(vllmModernMetrics), &s, &ctr)

	assertFloat(t, "kv_cache_usage_ratio", s.KVCacheUsageRatio, 0.63)
	assertInt(t, "prefix_cache_queries_total", s.PrefixCacheQueriesTotal, 500)
	assertInt(t, "prefix_cache_hits_total", s.PrefixCacheHitsTotal, 410)
	assertInt(t, "generated_tokens_total", s.GeneratedTokensTotal, 900)

	if s.RequestsRunning == nil || *s.RequestsRunning != 2 {
		t.Errorf("requests_running = %v, want 2", s.RequestsRunning)
	}

	// The renamed inter-token-latency histogram must feed the TPOT counters.
	if ctr.tpotSum != 8 || ctr.tpotCount != 400 {
		t.Errorf("tpot histogram = %v/%v, want 8/400", ctr.tpotSum, ctr.tpotCount)
	}
	if ctr.ttftSum != 20 || ctr.ttftCount != 50 {
		t.Errorf("ttft histogram = %v/%v, want 20/50", ctr.ttftSum, ctr.ttftCount)
	}

	// external_prefix_cache_* is a different subsystem (offloaded KV) and
	// must not be mistaken for the local prefix cache.
	if ctr.prefixHits != 410 {
		t.Errorf("prefix hits = %v, want 410 (not the external counter)", ctr.prefixHits)
	}
}

// The older naming must keep working, so upgrading the agent doesn't break
// hosts still on an earlier vLLM.
func TestMapVLLMLegacyMetricNamesStillWork(t *testing.T) {
	var s models.LLMSample
	var ctr llmCounterState
	mapVLLM(parseProm(vllmMetrics), &s, &ctr)

	assertFloat(t, "kv_cache_usage_ratio", s.KVCacheUsageRatio, 0.42)
	assertInt(t, "prefix_cache_hits_total", s.PrefixCacheHitsTotal, 600)
	if ctr.tpotSum != 5 || ctr.tpotCount != 250 {
		t.Errorf("legacy tpot histogram = %v/%v, want 5/250", ctr.tpotSum, ctr.tpotCount)
	}
}

func TestModelNameFromVLLMLabel(t *testing.T) {
	c := New(config.LLM{Autodetect: boolPtr(false)})
	state := &llmEndpointState{cfg: config.LLMEndpoint{URL: "http://127.0.0.1:8035"}}

	got := c.llmModelName("http://127.0.0.1:8035", state, parseProm(vllmModernMetrics), runtimeVLLM, time.Now())
	if got != "qwen35B" {
		t.Errorf("model = %q, want qwen35B (from the model_name label, no /v1/models call)", got)
	}
}

func TestParsePromLabelsAndValues(t *testing.T) {
	m := parseProm(vllmMetrics)

	if got, ok := m.value("vllm:gpu_cache_usage_perc"); !ok || got != 0.42 {
		t.Errorf("gpu_cache_usage_perc = %v (ok=%v), want 0.42", got, ok)
	}
	if got := m.label("vllm:num_requests_running", "model_name"); got != "meta-llama/Llama-3.1-8B-Instruct" {
		t.Errorf("model_name label = %q, want the Llama model id", got)
	}
	if !m.hasPrefix("vllm:") {
		t.Error("hasPrefix(vllm:) = false, want true")
	}
	if m.hasPrefix("llamacpp:") {
		t.Error("hasPrefix(llamacpp:) = true on vLLM output, want false")
	}
	// Comment lines must not become metrics.
	if _, ok := m.value("# HELP"); ok {
		t.Error("comment line was parsed as a metric")
	}
}

func TestParsePromUnlabeledMetrics(t *testing.T) {
	m := parseProm(llamaCppMetrics)

	if got, ok := m.value("llamacpp:kv_cache_usage_ratio"); !ok || got != 0.25 {
		t.Errorf("kv_cache_usage_ratio = %v (ok=%v), want 0.25", got, ok)
	}
	if got, ok := m.value("llamacpp:kv_cache_tokens"); !ok || got != 4096 {
		t.Errorf("kv_cache_tokens = %v (ok=%v), want 4096", got, ok)
	}
	if !m.hasPrefix("llamacpp:") {
		t.Error("hasPrefix(llamacpp:) = false, want true")
	}
}

func TestParsePromSkipsMalformedLines(t *testing.T) {
	m := parseProm("good_metric 1\nthis is not a metric line\nbad_metric{unclosed 2\nother_good 3\n")

	if _, ok := m.value("good_metric"); !ok {
		t.Error("good_metric missing; a malformed line should not abort the parse")
	}
	if _, ok := m.value("other_good"); !ok {
		t.Error("other_good missing; parsing should continue past malformed lines")
	}
}

func TestMapVLLM(t *testing.T) {
	var s models.LLMSample
	var ctr llmCounterState
	mapVLLM(parseProm(vllmMetrics), &s, &ctr)

	assertFloat(t, "kv_cache_usage_ratio", s.KVCacheUsageRatio, 0.42)
	assertInt(t, "prompt_tokens_total", s.PromptTokensTotal, 1000)
	assertInt(t, "generated_tokens_total", s.GeneratedTokensTotal, 500)
	assertInt(t, "prefix_cache_hits_total", s.PrefixCacheHitsTotal, 600)

	if s.RequestsRunning == nil || *s.RequestsRunning != 3 {
		t.Errorf("requests_running = %v, want 3", s.RequestsRunning)
	}
	if s.RequestsWaiting == nil || *s.RequestsWaiting != 7 {
		t.Errorf("requests_waiting = %v, want 7", s.RequestsWaiting)
	}

	// Counters are captured for rate derivation but no rate exists yet.
	if ctr.prefixQueries != 800 || ctr.ttftCount != 100 {
		t.Errorf("counters not captured: %+v", ctr)
	}
	if s.PrefixCacheHitRatio != nil || s.GeneratedTokensPerSec != nil {
		t.Error("mapping alone must not populate derived rates")
	}
}

func TestMapLlamaCpp(t *testing.T) {
	var s models.LLMSample
	var ctr llmCounterState
	mapLlamaCpp(parseProm(llamaCppMetrics), &s, &ctr)

	assertFloat(t, "kv_cache_usage_ratio", s.KVCacheUsageRatio, 0.25)
	assertInt(t, "kv_cache_tokens", s.KVCacheTokens, 4096)
	assertInt(t, "generated_tokens_total", s.GeneratedTokensTotal, 1024)

	// 40 tokens/sec => 25ms per output token.
	assertFloat(t, "tpot_ms_avg", s.TPOTMsAvg, 25)

	// llama.cpp exposes nothing that soundly yields these, so they must stay
	// nil rather than be reported as zero.
	if s.PrefixCacheHitRatio != nil || s.PrefixCacheHitsTotal != nil {
		t.Error("prefix cache fields should be nil for llama.cpp")
	}
	if s.TTFTMsAvg != nil {
		t.Error("ttft_ms_avg should be nil for llama.cpp")
	}
}

func TestApplyLLMRates(t *testing.T) {
	base := time.Now()
	prev := llmCounterState{
		promptTokens: 1000, genTokens: 500,
		prefixQueries: 800, prefixHits: 600,
		preemptions: 2,
		ttftSum:     10, ttftCount: 100,
		tpotSum: 5, tpotCount: 250,
		at: base,
	}
	// Ten seconds later: +200 generated tokens, +100 prefix queries of which
	// 75 hit, +1 preemption, +10 TTFT observations totalling 2s.
	cur := llmCounterState{
		promptTokens: 1400, genTokens: 700,
		prefixQueries: 900, prefixHits: 675,
		preemptions: 3,
		ttftSum:     12, ttftCount: 110,
		tpotSum: 5.5, tpotCount: 275,
		at: base.Add(10 * time.Second),
	}

	var s models.LLMSample
	applyLLMRates(&s, prev, cur)

	assertFloat(t, "prompt_tokens_per_sec", s.PromptTokensPerSec, 40)
	assertFloat(t, "generated_tokens_per_sec", s.GeneratedTokensPerSec, 20)
	assertFloat(t, "preemptions_per_sec", s.PreemptionsPerSec, 0.1)
	// Windowed, not cumulative: 75/100, not 675/900.
	assertFloat(t, "prefix_cache_hit_ratio", s.PrefixCacheHitRatio, 0.75)
	// 2s over 10 new observations = 200ms.
	assertFloat(t, "ttft_ms_avg", s.TTFTMsAvg, 200)
	// 0.5s over 25 new tokens = 20ms.
	assertFloat(t, "tpot_ms_avg", s.TPOTMsAvg, 20)
}

// A restarted runtime (llama-swap does this on every model switch) resets its
// counters to zero. That must not surface as a negative or absurd rate.
func TestApplyLLMRatesIgnoresCounterReset(t *testing.T) {
	base := time.Now()
	prev := llmCounterState{promptTokens: 1000, genTokens: 500, preemptions: 5, at: base}
	cur := llmCounterState{promptTokens: 10, genTokens: 5, preemptions: 0, at: base.Add(10 * time.Second)}

	var s models.LLMSample
	applyLLMRates(&s, prev, cur)

	if s.PromptTokensPerSec != nil {
		t.Errorf("prompt_tokens_per_sec = %v after counter reset, want nil", *s.PromptTokensPerSec)
	}
	if s.GeneratedTokensPerSec != nil {
		t.Errorf("generated_tokens_per_sec = %v after counter reset, want nil", *s.GeneratedTokensPerSec)
	}
	if s.PreemptionsPerSec != nil {
		t.Errorf("preemptions_per_sec = %v after counter reset, want nil", *s.PreemptionsPerSec)
	}
}

// With no traffic between scrapes there are no new observations to average,
// so a stale hit ratio or latency must not be invented.
func TestApplyLLMRatesIdleWindow(t *testing.T) {
	base := time.Now()
	ctr := llmCounterState{prefixQueries: 900, prefixHits: 675, ttftSum: 12, ttftCount: 110, at: base}
	next := ctr
	next.at = base.Add(10 * time.Second)

	var s models.LLMSample
	applyLLMRates(&s, ctr, next)

	if s.PrefixCacheHitRatio != nil {
		t.Errorf("prefix_cache_hit_ratio = %v with no queries in window, want nil", *s.PrefixCacheHitRatio)
	}
	if s.TTFTMsAvg != nil {
		t.Errorf("ttft_ms_avg = %v with no requests in window, want nil", *s.TTFTMsAvg)
	}
	// Throughput legitimately is zero when nothing was generated.
	assertFloat(t, "generated_tokens_per_sec", s.GeneratedTokensPerSec, 0)
}

// Exercises the whole path an endpoint takes: HTTP scrape, runtime sniffing,
// model lookup, and rate derivation across two ticks.
func TestCollectLLMEndToEnd(t *testing.T) {
	generated := 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			body := strings.Replace(llamaCppMetrics,
				"llamacpp:tokens_predicted_total 1024",
				fmt.Sprintf("llamacpp:tokens_predicted_total %d", generated), 1)
			fmt.Fprint(w, body)
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"qwen2.5-coder-7b"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(config.LLM{
		Autodetect: boolPtr(false),
		Endpoints:  []config.LLMEndpoint{{URL: srv.URL}},
	})

	start := time.Now()
	first := c.collectLLM(start)
	if len(first) != 1 {
		t.Fatalf("first scrape returned %d samples, want 1", len(first))
	}
	if first[0].Runtime != runtimeLlamaCpp {
		t.Errorf("runtime = %q, want %q", first[0].Runtime, runtimeLlamaCpp)
	}
	if first[0].Model != "qwen2.5-coder-7b" {
		t.Errorf("model = %q, want qwen2.5-coder-7b", first[0].Model)
	}
	if first[0].GeneratedTokensPerSec != nil {
		t.Error("first scrape must not report a rate; there is nothing to diff against")
	}

	// Second tick, ten seconds later, after 200 more tokens.
	generated = 1224
	second := c.collectLLM(start.Add(10 * time.Second))
	if len(second) != 1 {
		t.Fatalf("second scrape returned %d samples, want 1", len(second))
	}
	assertFloat(t, "generated_tokens_per_sec", second[0].GeneratedTokensPerSec, 20)
}

// An endpoint serving something that isn't a supported runtime must be
// ignored rather than reported with empty metrics.
func TestCollectLLMIgnoresUnknownExporter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "go_goroutines 12\nprocess_cpu_seconds_total 3.4\n")
	}))
	defer srv.Close()

	c := New(config.LLM{Autodetect: boolPtr(false), Endpoints: []config.LLMEndpoint{{URL: srv.URL}}})

	if samples := c.collectLLM(time.Now()); len(samples) != 0 {
		t.Errorf("got %d samples from an unrelated exporter, want 0", len(samples))
	}
}

func TestCollectLLMUnreachableEndpoint(t *testing.T) {
	// Nothing is listening here; this must be silent, not fatal.
	c := New(config.LLM{Autodetect: boolPtr(false), Endpoints: []config.LLMEndpoint{{URL: "http://127.0.0.1:1"}}})

	if samples := c.collectLLM(time.Now()); len(samples) != 0 {
		t.Errorf("got %d samples from an unreachable endpoint, want 0", len(samples))
	}
}

func boolPtr(b bool) *bool { return &b }

func assertFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", name, want)
		return
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func assertInt(t *testing.T, name string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %d", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", name, *got, want)
	}
}
