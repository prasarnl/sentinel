package models

import "time"

type CPUSample struct {
	Time     time.Time `json:"time"`
	UsagePct float64   `json:"usage_pct"`
	Load1    *float64  `json:"load1,omitempty"`
	Load5    *float64  `json:"load5,omitempty"`
	Load15   *float64  `json:"load15,omitempty"`
}

type MemSample struct {
	Time           time.Time `json:"time"`
	TotalBytes     int64     `json:"total_bytes"`
	UsedBytes      int64     `json:"used_bytes"`
	AvailableBytes int64     `json:"available_bytes"`
	SwapUsedBytes  int64     `json:"swap_used_bytes"`
	SwapTotalBytes int64     `json:"swap_total_bytes"`
}

type DiskSample struct {
	Time          time.Time `json:"time"`
	Mount         string    `json:"mount"`
	TotalBytes    int64     `json:"total_bytes"`
	UsedBytes     int64     `json:"used_bytes"`
	ReadBytesSec  float64   `json:"read_bytes_sec"`
	WriteBytesSec float64   `json:"write_bytes_sec"`
}

type GPUSample struct {
	Time           time.Time `json:"time"`
	GPUIndex       int       `json:"gpu_index"`
	Vendor         string    `json:"vendor"`
	Name           string    `json:"name"`
	UtilizationPct *float64  `json:"utilization_pct,omitempty"`
	MemUsedBytes   *int64    `json:"mem_used_bytes,omitempty"`
	MemTotalBytes  *int64    `json:"mem_total_bytes,omitempty"`
	TempC          *float64  `json:"temp_c,omitempty"`
}

// LLMSample is one scrape of an inference runtime's metrics endpoint,
// normalized across backends so the dashboard doesn't need to know whether
// llama.cpp or vLLM is behind a given endpoint. Every metric is optional: a
// runtime that doesn't expose one leaves it nil rather than reporting zero,
// which would read as "0% cache hits" instead of "not measurable here".
type LLMSample struct {
	Time     time.Time `json:"time"`
	Endpoint string    `json:"endpoint"`         // e.g. http://127.0.0.1:8080
	Runtime  string    `json:"runtime"`          // llamacpp | vllm
	Model    string    `json:"model,omitempty"`  // best-effort; blank when unknown

	KVCacheUsageRatio *float64 `json:"kv_cache_usage_ratio,omitempty"` // 0..1
	KVCacheTokens     *int64   `json:"kv_cache_tokens,omitempty"`

	// Cumulative counters are reported as-is; the per-second rates beside
	// them are derived across ticks (see collectLLM) and are nil on the
	// first scrape of an endpoint, exactly as disk I/O rates are.
	PromptTokensTotal     *int64   `json:"prompt_tokens_total,omitempty"`
	GeneratedTokensTotal  *int64   `json:"generated_tokens_total,omitempty"`
	PromptTokensPerSec    *float64 `json:"prompt_tokens_per_sec,omitempty"`
	GeneratedTokensPerSec *float64 `json:"generated_tokens_per_sec,omitempty"`

	// PrefixCacheHitRatio is windowed (hits/queries since the previous
	// scrape), not cumulative-since-boot, so it reflects current behaviour
	// rather than being dominated by hours-old history.
	PrefixCacheQueriesTotal *int64   `json:"prefix_cache_queries_total,omitempty"`
	PrefixCacheHitsTotal    *int64   `json:"prefix_cache_hits_total,omitempty"`
	PrefixCacheHitRatio     *float64 `json:"prefix_cache_hit_ratio,omitempty"` // 0..1

	RequestsRunning *int `json:"requests_running,omitempty"`
	RequestsWaiting *int `json:"requests_waiting,omitempty"`

	TTFTMsAvg *float64 `json:"ttft_ms_avg,omitempty"`
	TPOTMsAvg *float64 `json:"tpot_ms_avg,omitempty"`

	// PreemptionsPerSec rising means the KV cache is oversubscribed and the
	// scheduler is evicting in-flight requests. vLLM only.
	PreemptionsPerSec *float64 `json:"preemptions_per_sec,omitempty"`
}

type IngestPayload struct {
	CPU  []CPUSample  `json:"cpu,omitempty"`
	Mem  []MemSample  `json:"mem,omitempty"`
	Disk []DiskSample `json:"disk,omitempty"`
	GPU  []GPUSample  `json:"gpu,omitempty"`
	LLM  []LLMSample  `json:"llm,omitempty"`
}
