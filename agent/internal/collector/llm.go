package collector

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"sentinel/agent/internal/config"
	"sentinel/agent/internal/models"
)

const (
	runtimeLlamaCpp = "llamacpp"
	runtimeVLLM     = "vllm"

	// probeInterval is how often endpoints that aren't currently reporting
	// are retried, and how often a runtime's model name is refreshed (it can
	// change under llama-swap without the endpoint changing).
	probeInterval = 60 * time.Second

	llmScrapeTimeout = 2 * time.Second
)

// autodetectTargets are the default listen addresses of the supported
// runtimes. Probing is cheap (a 2s-timeout request to loopback, once a
// minute) so a host with no inference server pays essentially nothing.
var autodetectTargets = []string{
	"http://127.0.0.1:8080", // llama.cpp / llama-swap
	"http://127.0.0.1:8000", // vLLM
}

// llmCounterState holds the cumulative counters from an endpoint's previous
// scrape, so per-second rates and windowed ratios can be derived. Same
// approach as diskIOState.
type llmCounterState struct {
	promptTokens  float64
	genTokens     float64
	prefixQueries float64
	prefixHits    float64
	preemptions   float64
	ttftSum       float64
	ttftCount     float64
	tpotSum       float64
	tpotCount     float64
	at            time.Time
}

// llmEndpointState tracks one endpoint the agent scrapes.
type llmEndpointState struct {
	cfg          config.LLMEndpoint
	autodetected bool   // autodetected endpoints are dropped when they stop responding, so they get re-probed
	model        string // cached; refreshed every probeInterval
	modelAt      time.Time
}

// collectLLM scrapes every known inference endpoint and normalizes the result.
// Like GPU collection, it is entirely best-effort: an endpoint that is down,
// unreachable, or serving something unrecognized simply contributes no sample.
func (c *Collector) collectLLM(now time.Time) []models.LLMSample {
	c.discoverLLMEndpoints(now)

	var samples []models.LLMSample
	for url, state := range c.llmEndpoints {
		sample, ok := c.scrapeLLMEndpoint(url, state, now)
		if !ok {
			// An autodetected endpoint that stopped answering may have moved
			// or shut down; forget it so the next probe can re-evaluate.
			if state.autodetected {
				delete(c.llmEndpoints, url)
			}
			continue
		}
		samples = append(samples, sample)
	}
	return samples
}

// discoverLLMEndpoints seeds explicitly configured endpoints (once) and, when
// autodetection is on, periodically probes well-known local ports.
func (c *Collector) discoverLLMEndpoints(now time.Time) {
	for _, ep := range c.llmConfig.Endpoints {
		url := strings.TrimSuffix(ep.URL, "/")
		if url == "" {
			continue
		}
		if _, exists := c.llmEndpoints[url]; !exists {
			c.llmEndpoints[url] = &llmEndpointState{cfg: ep}
		}
	}

	if !c.llmConfig.AutodetectEnabled() {
		return
	}
	if !c.lastLLMProbe.IsZero() && now.Sub(c.lastLLMProbe) < probeInterval {
		return
	}
	c.lastLLMProbe = now

	for _, url := range autodetectTargets {
		if _, exists := c.llmEndpoints[url]; exists {
			continue
		}
		if _, _, ok := c.fetchMetrics(url, ""); ok {
			c.llmEndpoints[url] = &llmEndpointState{
				cfg:          config.LLMEndpoint{URL: url},
				autodetected: true,
			}
		}
	}
}

// scrapeLLMEndpoint fetches and maps one endpoint's metrics.
func (c *Collector) scrapeLLMEndpoint(url string, state *llmEndpointState, now time.Time) (models.LLMSample, bool) {
	metrics, runtime, ok := c.fetchMetrics(url, state.cfg.APIKey)
	if !ok {
		return models.LLMSample{}, false
	}
	if configured := state.cfg.Runtime; configured != "" && configured != "auto" {
		runtime = configured
	}

	sample := models.LLMSample{
		Time:     now,
		Endpoint: url,
		Runtime:  runtime,
		Model:    c.llmModelName(url, state, metrics, runtime, now),
	}

	var counters llmCounterState
	counters.at = now

	switch runtime {
	case runtimeVLLM:
		mapVLLM(metrics, &sample, &counters)
	case runtimeLlamaCpp:
		mapLlamaCpp(metrics, &sample, &counters)
	default:
		return models.LLMSample{}, false
	}

	if prev, hadPrev := c.prevLLM[url]; hadPrev {
		applyLLMRates(&sample, prev, counters)
	}
	c.prevLLM[url] = counters

	return sample, true
}

// mapVLLM maps vLLM's metric names onto the normalized sample. vLLM reports
// the richest set of the supported runtimes: it is the only one exposing
// prefix-cache hit counters and scheduler preemptions.
func mapVLLM(m promMetrics, s *models.LLMSample, ctr *llmCounterState) {
	// Despite the "_perc" suffix vLLM reports this as a 0..1 fraction, which
	// matches llama.cpp's ratio, so no conversion is needed.
	s.KVCacheUsageRatio = m.ptr("vllm:gpu_cache_usage_perc")

	s.PromptTokensTotal = intPtr(m, "vllm:prompt_tokens_total")
	s.GeneratedTokensTotal = intPtr(m, "vllm:generation_tokens_total")
	s.PrefixCacheQueriesTotal = intPtr(m, "vllm:gpu_prefix_cache_queries_total")
	s.PrefixCacheHitsTotal = intPtr(m, "vllm:gpu_prefix_cache_hits_total")
	s.RequestsRunning = intFromMetric(m, "vllm:num_requests_running")
	s.RequestsWaiting = intFromMetric(m, "vllm:num_requests_waiting")

	ctr.promptTokens, _ = m.value("vllm:prompt_tokens_total")
	ctr.genTokens, _ = m.value("vllm:generation_tokens_total")
	ctr.prefixQueries, _ = m.value("vllm:gpu_prefix_cache_queries_total")
	ctr.prefixHits, _ = m.value("vllm:gpu_prefix_cache_hits_total")
	ctr.preemptions, _ = m.value("vllm:num_preemptions_total")
	ctr.ttftSum, _ = m.value("vllm:time_to_first_token_seconds_sum")
	ctr.ttftCount, _ = m.value("vllm:time_to_first_token_seconds_count")
	ctr.tpotSum, _ = m.value("vllm:time_per_output_token_seconds_sum")
	ctr.tpotCount, _ = m.value("vllm:time_per_output_token_seconds_count")
}

// mapLlamaCpp maps llama.cpp's metric names. It exposes no prefix-cache or
// preemption counters, so those stay nil; its latency gauges are already
// rates, so TPOT is derived directly rather than from a histogram.
func mapLlamaCpp(m promMetrics, s *models.LLMSample, ctr *llmCounterState) {
	s.KVCacheUsageRatio = m.ptr("llamacpp:kv_cache_usage_ratio")
	s.KVCacheTokens = intPtr(m, "llamacpp:kv_cache_tokens")

	s.PromptTokensTotal = intPtr(m, "llamacpp:prompt_tokens_total")
	s.GeneratedTokensTotal = intPtr(m, "llamacpp:tokens_predicted_total")
	s.RequestsRunning = intFromMetric(m, "llamacpp:requests_processing")
	s.RequestsWaiting = intFromMetric(m, "llamacpp:requests_deferred")

	// predicted_tokens_seconds is the generation rate of the last request, so
	// its reciprocal is milliseconds per output token. There is no sound way
	// to recover TTFT from what llama.cpp exposes, so TTFT stays nil rather
	// than being fabricated from prompt throughput.
	if rate, ok := m.value("llamacpp:predicted_tokens_seconds"); ok && rate > 0 {
		ms := 1000 / rate
		s.TPOTMsAvg = &ms
	}

	ctr.promptTokens, _ = m.value("llamacpp:prompt_tokens_total")
	ctr.genTokens, _ = m.value("llamacpp:tokens_predicted_total")
}

// applyLLMRates fills in the derived per-second rates and windowed ratios from
// the delta between two scrapes.
//
// Counters that went backwards mean the runtime restarted (llama-swap does
// this on every model switch), so those rates are left nil for one tick rather
// than reported as a large negative or bogus spike.
func applyLLMRates(s *models.LLMSample, prev, cur llmCounterState) {
	elapsed := cur.at.Sub(prev.at).Seconds()
	if elapsed <= 0 {
		return
	}

	if rate, ok := perSec(prev.promptTokens, cur.promptTokens, elapsed); ok {
		s.PromptTokensPerSec = &rate
	}
	if rate, ok := perSec(prev.genTokens, cur.genTokens, elapsed); ok {
		s.GeneratedTokensPerSec = &rate
	}
	if rate, ok := perSec(prev.preemptions, cur.preemptions, elapsed); ok {
		s.PreemptionsPerSec = &rate
	}

	// A hit ratio over the scrape window, rather than vLLM's since-boot
	// cumulative figure, so the number reflects what the cache is doing now.
	if queries := cur.prefixQueries - prev.prefixQueries; queries > 0 {
		if hits := cur.prefixHits - prev.prefixHits; hits >= 0 {
			ratio := hits / queries
			s.PrefixCacheHitRatio = &ratio
		}
	}

	if ms, ok := avgMillis(prev.ttftSum, cur.ttftSum, prev.ttftCount, cur.ttftCount); ok {
		s.TTFTMsAvg = &ms
	}
	if ms, ok := avgMillis(prev.tpotSum, cur.tpotSum, prev.tpotCount, cur.tpotCount); ok {
		s.TPOTMsAvg = &ms
	}
}

// perSec derives a rate from two readings of a cumulative counter, rejecting
// counter resets.
func perSec(prev, cur, elapsed float64) (float64, bool) {
	if cur < prev {
		return 0, false
	}
	return (cur - prev) / elapsed, true
}

// avgMillis derives a mean latency in milliseconds from the _sum and _count
// pair of a Prometheus histogram, over the scrape window only.
func avgMillis(prevSum, curSum, prevCount, curCount float64) (float64, bool) {
	count := curCount - prevCount
	sum := curSum - prevSum
	if count <= 0 || sum < 0 {
		return 0, false
	}
	return (sum / count) * 1000, true
}

// llmModelName resolves which model an endpoint is serving. vLLM labels its
// metrics with it; llama.cpp does not, so its name comes from /v1/models,
// re-fetched periodically because llama-swap changes models underneath a
// stable endpoint.
func (c *Collector) llmModelName(url string, state *llmEndpointState, m promMetrics, runtime string, now time.Time) string {
	if runtime == runtimeVLLM {
		for _, name := range []string{"vllm:num_requests_running", "vllm:gpu_cache_usage_perc"} {
			if model := m.label(name, "model_name"); model != "" {
				return model
			}
		}
	}

	if state.model != "" && now.Sub(state.modelAt) < probeInterval {
		return state.model
	}
	state.model = c.fetchModelName(url, state.cfg.APIKey)
	state.modelAt = now
	return state.model
}

func (c *Collector) fetchModelName(url, apiKey string) string {
	body, ok := c.httpGet(url+"/v1/models", apiKey)
	if !ok {
		return ""
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || len(out.Data) == 0 {
		return ""
	}
	return out.Data[0].ID
}

// fetchMetrics scrapes /metrics and identifies which runtime produced it from
// the metric-name prefix.
func (c *Collector) fetchMetrics(url, apiKey string) (promMetrics, string, bool) {
	body, ok := c.httpGet(url+"/metrics", apiKey)
	if !ok {
		return nil, "", false
	}

	metrics := parseProm(body)
	switch {
	case metrics.hasPrefix("vllm:"):
		return metrics, runtimeVLLM, true
	case metrics.hasPrefix("llamacpp:"):
		return metrics, runtimeLlamaCpp, true
	default:
		// Something else is listening on this port (a plain Prometheus
		// exporter, an unrelated service); not ours to report on.
		return nil, "", false
	}
}

func (c *Collector) httpGet(url, apiKey string) (string, bool) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.llmClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", false
	}
	return string(body), true
}

func intPtr(m promMetrics, name string) *int64 {
	v, ok := m.value(name)
	if !ok {
		return nil
	}
	i := int64(v)
	return &i
}

func intFromMetric(m promMetrics, name string) *int {
	v, ok := m.value(name)
	if !ok {
		return nil
	}
	i := int(v)
	return &i
}
