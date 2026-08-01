package llmscrape

// Runtimes rename metrics between releases, so each logical metric lists
// every spelling we know of, newest first. vLLM v0.9 dropped the "gpu_"
// prefix from the cache metrics and replaced time_per_output_token with
// inter_token_latency.
//
// Matching only one spelling yields nil on every other version — visually
// identical to a runtime that genuinely cannot report the metric, with
// nothing logged and no error to trace back from. That failure has already
// happened once here; the fallback lists are the fix.
var (
	vllmKVCacheNames     = []string{"vllm:kv_cache_usage_perc", "vllm:gpu_cache_usage_perc"}
	vllmPrefixQueryNames = []string{"vllm:prefix_cache_queries_total", "vllm:gpu_prefix_cache_queries_total"}
	vllmPrefixHitNames   = []string{"vllm:prefix_cache_hits_total", "vllm:gpu_prefix_cache_hits_total"}
	vllmRunningNames     = []string{"vllm:num_requests_running"}
	vllmWaitingNames     = []string{"vllm:num_requests_waiting"}
	vllmPromptNames      = []string{"vllm:prompt_tokens_total"}
	vllmGeneratedNames   = []string{"vllm:generation_tokens_total"}
	vllmPreemptionNames  = []string{"vllm:num_preemptions_total"}

	// Present only when the server runs speculative decoding (MTP, EAGLE,
	// n-gram, a draft model). Absent otherwise, which is why these stay nil
	// rather than zero — no speculation is not the same as speculation that
	// never gets accepted.
	vllmSpecDraftsNames      = []string{"vllm:spec_decode_num_drafts_total"}
	vllmSpecDraftTokensNames = []string{"vllm:spec_decode_num_draft_tokens_total"}
	vllmSpecAcceptedNames    = []string{"vllm:spec_decode_num_accepted_tokens_total"}

	// Histogram base names; the _sum/_count suffixes are appended.
	vllmTTFTBases = []string{"vllm:time_to_first_token_seconds"}
	vllmTPOTBases = []string{
		"vllm:inter_token_latency_seconds",
		"vllm:time_per_output_token_seconds",
		"vllm:request_time_per_output_token_seconds",
	}
)

// mapVLLM maps vLLM's metric names onto the normalized sample. vLLM reports
// the richest set of the supported runtimes: it is the only one exposing
// prefix-cache hit counters and scheduler preemptions.
func mapVLLM(m promMetrics, s *Sample, ctr *counterState) {
	// Despite the "_perc" suffix vLLM reports this as a 0..1 fraction, which
	// matches llama.cpp's ratio, so no conversion is needed.
	s.KVCacheUsageRatio = m.firstPtr(vllmKVCacheNames...)

	s.PromptTokensTotal = intPtrFirst(m, vllmPromptNames...)
	s.GeneratedTokensTotal = intPtrFirst(m, vllmGeneratedNames...)
	s.PrefixCacheQueriesTotal = intPtrFirst(m, vllmPrefixQueryNames...)
	s.PrefixCacheHitsTotal = intPtrFirst(m, vllmPrefixHitNames...)
	s.RequestsRunning = intFromFirst(m, vllmRunningNames...)
	s.RequestsWaiting = intFromFirst(m, vllmWaitingNames...)

	ctr.promptTokens, _ = m.firstValue(vllmPromptNames...)
	ctr.genTokens, _ = m.firstValue(vllmGeneratedNames...)
	ctr.prefixQueries, _ = m.firstValue(vllmPrefixQueryNames...)
	ctr.prefixHits, _ = m.firstValue(vllmPrefixHitNames...)
	ctr.preemptions, _ = m.firstValue(vllmPreemptionNames...)
	ctr.ttftSum, ctr.ttftCount = m.histogram(vllmTTFTBases...)
	ctr.tpotSum, ctr.tpotCount = m.histogram(vllmTPOTBases...)

	s.SpecDecodeDraftTokensTotal = intPtrFirst(m, vllmSpecDraftTokensNames...)
	s.SpecDecodeAcceptedTokensTotal = intPtrFirst(m, vllmSpecAcceptedNames...)
	ctr.specDrafts, _ = m.firstValue(vllmSpecDraftsNames...)
	ctr.specDraftToks, _ = m.firstValue(vllmSpecDraftTokensNames...)
	ctr.specAccepted, _ = m.firstValue(vllmSpecAcceptedNames...)
}

// mapLlamaCpp maps llama.cpp's metric names. It exposes no prefix-cache or
// preemption counters, so those stay nil; its latency gauges are already
// rates, so TPOT is derived directly rather than from a histogram.
func mapLlamaCpp(m promMetrics, s *Sample, ctr *counterState) {
	s.KVCacheUsageRatio = m.ptr("llamacpp:kv_cache_usage_ratio")
	s.KVCacheTokens = intPtrFirst(m, "llamacpp:kv_cache_tokens")

	s.PromptTokensTotal = intPtrFirst(m, "llamacpp:prompt_tokens_total")
	s.GeneratedTokensTotal = intPtrFirst(m, "llamacpp:tokens_predicted_total")
	s.RequestsRunning = intFromFirst(m, "llamacpp:requests_processing")
	s.RequestsWaiting = intFromFirst(m, "llamacpp:requests_deferred")

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

// applyRates fills in derived per-second rates and windowed ratios from the
// delta between two scrapes.
//
// Counters that went backwards mean the runtime restarted (llama-swap does
// this on every model switch), so those rates are left nil for one tick
// rather than reported as a large negative or a bogus spike.
func applyRates(s *Sample, prev, cur counterState) {
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

	// A hit ratio over the scrape window, rather than the since-boot
	// cumulative figure, so the number reflects current behaviour.
	if queries := cur.prefixQueries - prev.prefixQueries; queries > 0 {
		if hits := cur.prefixHits - prev.prefixHits; hits >= 0 {
			ratio := hits / queries
			s.PrefixCacheHitRatio = &ratio
		}
	}

	// Speculative decoding, over the window only. Both denominators must have
	// advanced: a window with no drafts says nothing about acceptance, and
	// reporting a stale figure would misrepresent an idle server as a
	// performing one.
	if draftTokens := cur.specDraftToks - prev.specDraftToks; draftTokens > 0 {
		if accepted := cur.specAccepted - prev.specAccepted; accepted >= 0 {
			rate := accepted / draftTokens
			s.SpecDecodeAcceptanceRate = &rate
		}
	}
	if drafts := cur.specDrafts - prev.specDrafts; drafts > 0 {
		if accepted := cur.specAccepted - prev.specAccepted; accepted >= 0 {
			perDraft := accepted / drafts
			s.SpecDecodeAcceptedPerDraft = &perDraft
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

func intPtrFirst(m promMetrics, names ...string) *int64 {
	v, ok := m.firstValue(names...)
	if !ok {
		return nil
	}
	i := int64(v)
	return &i
}

func intFromFirst(m promMetrics, names ...string) *int {
	v, ok := m.firstValue(names...)
	if !ok {
		return nil
	}
	i := int(v)
	return &i
}
