package llmscrape

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Pre-0.9 vLLM naming, kept so upgrading doesn't break hosts still running an
// older server.
const vllmLegacyMetrics = `
# HELP vllm:num_requests_running Number of requests currently running on GPU.
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{model_name="meta-llama/Llama-3.1-8B-Instruct"} 3.0
vllm:num_requests_waiting{model_name="meta-llama/Llama-3.1-8B-Instruct"} 7.0
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

// vLLM v0.9+ naming, captured from a live server (Qwen3.6-35B on a GB10):
// the gpu_ prefixes are gone and TPOT became inter_token_latency.
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

const llamaCppMetrics = `
# HELP llamacpp:prompt_tokens_total Number of prompt tokens processed.
# TYPE llamacpp:prompt_tokens_total counter
llamacpp:prompt_tokens_total 2048
llamacpp:tokens_predicted_total 1024
llamacpp:kv_cache_usage_ratio 0.25
llamacpp:kv_cache_tokens 4096
llamacpp:requests_processing 2
llamacpp:requests_deferred 1
llamacpp:predicted_tokens_seconds 40
`

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

func TestParsePromLabelsAndValues(t *testing.T) {
	m := parseProm(vllmLegacyMetrics)

	if got, ok := m.value("vllm:gpu_cache_usage_perc"); !ok || got != 0.42 {
		t.Errorf("gpu_cache_usage_perc = %v (ok=%v), want 0.42", got, ok)
	}
	if got := m.label("vllm:num_requests_running", "model_name"); got != "meta-llama/Llama-3.1-8B-Instruct" {
		t.Errorf("model_name label = %q, want the Llama model id", got)
	}
	if !m.hasPrefix("vllm:") || m.hasPrefix("llamacpp:") {
		t.Error("runtime prefix detection is wrong for vLLM output")
	}
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

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

func TestMapVLLMModernNames(t *testing.T) {
	var s Sample
	var ctr counterState
	mapVLLM(parseProm(vllmModernMetrics), &s, &ctr)

	assertFloat(t, "kv_cache_usage_ratio", s.KVCacheUsageRatio, 0.63)
	assertInt(t, "prefix_cache_queries_total", s.PrefixCacheQueriesTotal, 500)
	assertInt(t, "prefix_cache_hits_total", s.PrefixCacheHitsTotal, 410)
	assertInt(t, "generated_tokens_total", s.GeneratedTokensTotal, 900)

	if s.RequestsRunning == nil || *s.RequestsRunning != 2 {
		t.Errorf("requests_running = %v, want 2", s.RequestsRunning)
	}
	if ctr.tpotSum != 8 || ctr.tpotCount != 400 {
		t.Errorf("tpot histogram = %v/%v, want 8/400 (inter_token_latency)", ctr.tpotSum, ctr.tpotCount)
	}
	// external_prefix_cache_* is offloaded KV, a different subsystem; folding
	// it into the local prefix cache would inflate the hit rate.
	if ctr.prefixHits != 410 {
		t.Errorf("prefix hits = %v, want 410 (not the external counter)", ctr.prefixHits)
	}
}

func TestMapVLLMLegacyNamesStillWork(t *testing.T) {
	var s Sample
	var ctr counterState
	mapVLLM(parseProm(vllmLegacyMetrics), &s, &ctr)

	assertFloat(t, "kv_cache_usage_ratio", s.KVCacheUsageRatio, 0.42)
	assertInt(t, "prefix_cache_hits_total", s.PrefixCacheHitsTotal, 600)
	if ctr.tpotSum != 5 || ctr.tpotCount != 250 {
		t.Errorf("legacy tpot histogram = %v/%v, want 5/250", ctr.tpotSum, ctr.tpotCount)
	}
}

func TestMapLlamaCpp(t *testing.T) {
	var s Sample
	var ctr counterState
	mapLlamaCpp(parseProm(llamaCppMetrics), &s, &ctr)

	assertFloat(t, "kv_cache_usage_ratio", s.KVCacheUsageRatio, 0.25)
	assertInt(t, "kv_cache_tokens", s.KVCacheTokens, 4096)
	assertFloat(t, "tpot_ms_avg", s.TPOTMsAvg, 25) // 40 tok/s => 25ms/token

	if s.PrefixCacheHitRatio != nil || s.PrefixCacheHitsTotal != nil || s.TTFTMsAvg != nil {
		t.Error("llama.cpp cannot report prefix cache or TTFT; those must stay nil, not zero")
	}
}

// ---------------------------------------------------------------------------
// Rate derivation
// ---------------------------------------------------------------------------

func TestApplyRates(t *testing.T) {
	base := time.Now()
	prev := counterState{
		promptTokens: 1000, genTokens: 500,
		prefixQueries: 800, prefixHits: 600, preemptions: 2,
		ttftSum: 10, ttftCount: 100, tpotSum: 5, tpotCount: 250,
		at: base,
	}
	cur := counterState{
		promptTokens: 1400, genTokens: 700,
		prefixQueries: 900, prefixHits: 675, preemptions: 3,
		ttftSum: 12, ttftCount: 110, tpotSum: 5.5, tpotCount: 275,
		at: base.Add(10 * time.Second),
	}

	var s Sample
	applyRates(&s, prev, cur)

	assertFloat(t, "prompt_tokens_per_sec", s.PromptTokensPerSec, 40)
	assertFloat(t, "generated_tokens_per_sec", s.GeneratedTokensPerSec, 20)
	assertFloat(t, "preemptions_per_sec", s.PreemptionsPerSec, 0.1)
	// Windowed, not cumulative: 75/100, not 675/900.
	assertFloat(t, "prefix_cache_hit_ratio", s.PrefixCacheHitRatio, 0.75)
	assertFloat(t, "ttft_ms_avg", s.TTFTMsAvg, 200)
	assertFloat(t, "tpot_ms_avg", s.TPOTMsAvg, 20)
}

func TestApplyRatesIgnoresCounterReset(t *testing.T) {
	base := time.Now()
	prev := counterState{promptTokens: 1000, genTokens: 500, preemptions: 5, at: base}
	cur := counterState{promptTokens: 10, genTokens: 5, preemptions: 0, at: base.Add(10 * time.Second)}

	var s Sample
	applyRates(&s, prev, cur)

	if s.PromptTokensPerSec != nil || s.GeneratedTokensPerSec != nil || s.PreemptionsPerSec != nil {
		t.Error("a restarted runtime resets counters; rates must be nil, not negative")
	}
}

func TestApplyRatesIdleWindow(t *testing.T) {
	base := time.Now()
	ctr := counterState{prefixQueries: 900, prefixHits: 675, ttftSum: 12, ttftCount: 110, at: base}
	next := ctr
	next.at = base.Add(10 * time.Second)

	var s Sample
	applyRates(&s, ctr, next)

	if s.PrefixCacheHitRatio != nil {
		t.Errorf("hit ratio = %v with no queries in window; a stale figure must not be invented", *s.PrefixCacheHitRatio)
	}
	if s.TTFTMsAvg != nil {
		t.Errorf("ttft = %v with no requests in window, want nil", *s.TTFTMsAvg)
	}
	// Throughput legitimately is zero when nothing was generated.
	assertFloat(t, "generated_tokens_per_sec", s.GeneratedTokensPerSec, 0)
}

// ---------------------------------------------------------------------------
// Scraper
// ---------------------------------------------------------------------------

func TestScrapeDerivesRatesAcrossCalls(t *testing.T) {
	generated := 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			fmt.Fprint(w, strings.Replace(llamaCppMetrics,
				"llamacpp:tokens_predicted_total 1024",
				fmt.Sprintf("llamacpp:tokens_predicted_total %d", generated), 1))
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"qwen2.5-coder-7b"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	sc := NewScraper(2 * time.Second)
	start := time.Now()

	first, err := sc.Scrape(context.Background(), Endpoint{URL: srv.URL}, start)
	if err != nil {
		t.Fatalf("first scrape: %v", err)
	}
	if first.Runtime != RuntimeLlamaCpp {
		t.Errorf("runtime = %q, want %q", first.Runtime, RuntimeLlamaCpp)
	}
	if first.Model != "qwen2.5-coder-7b" {
		t.Errorf("model = %q, want qwen2.5-coder-7b", first.Model)
	}
	if first.GeneratedTokensPerSec != nil {
		t.Error("first scrape must not report a rate; nothing to diff against")
	}

	generated = 1224
	second, err := sc.Scrape(context.Background(), Endpoint{URL: srv.URL}, start.Add(10*time.Second))
	if err != nil {
		t.Fatalf("second scrape: %v", err)
	}
	assertFloat(t, "generated_tokens_per_sec", second.GeneratedTokensPerSec, 20)
}

// The three failure modes must stay distinguishable. They all look like "no
// data" to a user but mean very different things operationally, and the UI
// reports them differently.
func TestScrapeDistinguishesFailureModes(t *testing.T) {
	// Answers HTTP but serves no /metrics, via an honest 404.
	noMetrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"some-model"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer noMetrics.Close()

	// LM Studio, captured from a live instance: GET /metrics returns 200 with
	// a JSON error rather than a 404, so HTTP status alone cannot distinguish
	// a metrics endpoint from a runtime that has none. Reporting this as
	// "unrecognized runtime" would be wrong — it is a runtime, it just
	// publishes no Prometheus metrics.
	lmStudio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"qwen2.5-7b-instruct"}]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"Unexpected endpoint or method. (GET /metrics)"}`)
	}))
	defer lmStudio.Close()

	// Serves metrics, but not from an inference runtime.
	otherExporter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "go_goroutines 12\nprocess_cpu_seconds_total 3.4\n")
	}))
	defer otherExporter.Close()

	tests := []struct {
		name string
		url  string
		want error
	}{
		{"404 on metrics", noMetrics.URL, ErrNoMetrics},
		{"lm studio: 200 with a json error body", lmStudio.URL, ErrNoMetrics},
		{"unrelated exporter", otherExporter.URL, ErrUnknownRuntime},
		{"nothing listening", "http://127.0.0.1:1", ErrUnreachable},
	}

	sc := NewScraper(2 * time.Second)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sc.Scrape(context.Background(), Endpoint{URL: tc.url}, time.Now())
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestProbeIdentifiesRuntimeWithoutRetainingState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, vllmModernMetrics)
	}))
	defer srv.Close()

	sc := NewScraper(2 * time.Second)
	runtime, err := sc.Probe(context.Background(), srv.URL, "")
	if err != nil || runtime != RuntimeVLLM {
		t.Errorf("Probe() = %q, %v; want %q, nil", runtime, err, RuntimeVLLM)
	}
	// Probing must not retain counters, or the first real scrape would diff
	// against a reading taken at probe time and report a bogus rate.
	if len(sc.prev) != 0 {
		t.Errorf("Probe retained %d counter states, want 0", len(sc.prev))
	}
}

// vLLM's auth middleware guards /v1 but not /metrics, so the model name must
// come from the metric label rather than a /v1/models request that would 401
// on a server started with --api-key.
func TestModelNameFromLabelWithoutExtraRequest(t *testing.T) {
	var v1Calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1") {
			v1Calls++
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, vllmModernMetrics)
	}))
	defer srv.Close()

	sc := NewScraper(2 * time.Second)
	got, err := sc.Scrape(context.Background(), Endpoint{URL: srv.URL}, time.Now())
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if got.Model != "qwen35B" {
		t.Errorf("model = %q, want qwen35B", got.Model)
	}
	if v1Calls != 0 {
		t.Errorf("made %d /v1 requests; vLLM's model name comes from the metric label", v1Calls)
	}
}

func TestForgetDropsState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, vllmModernMetrics)
	}))
	defer srv.Close()

	sc := NewScraper(2 * time.Second)
	ep := Endpoint{URL: srv.URL}
	if _, err := sc.Scrape(context.Background(), ep, time.Now()); err != nil {
		t.Fatalf("scrape: %v", err)
	}
	sc.Forget(srv.URL)

	got, err := sc.Scrape(context.Background(), ep, time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatalf("scrape after forget: %v", err)
	}
	if got.GeneratedTokensPerSec != nil {
		t.Error("rate reported after Forget; retained state should have been dropped")
	}
}

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
