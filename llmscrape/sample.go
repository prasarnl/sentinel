package llmscrape

import "time"

// Runtime identifiers, as detected from a metrics endpoint's metric-name
// prefix.
const (
	RuntimeLlamaCpp = "llamacpp"
	RuntimeVLLM     = "vllm"

	// RuntimeAuto asks for detection rather than asserting a runtime.
	RuntimeAuto = "auto"
)

// Sample is one scrape of an inference runtime's metrics endpoint, normalized
// across backends so consumers don't need to know whether llama.cpp or vLLM
// is behind a given endpoint.
//
// Every metric is a nullable pointer, and nil means "this runtime cannot
// report it" rather than zero. llama.cpp exposes no prefix-cache counters at
// all, and rendering that as "0% hits" would describe a cache that never hits
// instead of one that isn't measurable.
type Sample struct {
	Time     time.Time `json:"time"`
	Endpoint string    `json:"endpoint"`
	Runtime  string    `json:"runtime"` // llamacpp | vllm
	Model    string    `json:"model,omitempty"`

	KVCacheUsageRatio *float64 `json:"kv_cache_usage_ratio,omitempty"` // 0..1
	KVCacheTokens     *int64   `json:"kv_cache_tokens,omitempty"`

	// Cumulative counters are reported as-is; the per-second rates beside
	// them are derived across scrapes and are nil on the first scrape of an
	// endpoint, since there is nothing to diff against.
	PromptTokensTotal     *int64   `json:"prompt_tokens_total,omitempty"`
	GeneratedTokensTotal  *int64   `json:"generated_tokens_total,omitempty"`
	PromptTokensPerSec    *float64 `json:"prompt_tokens_per_sec,omitempty"`
	GeneratedTokensPerSec *float64 `json:"generated_tokens_per_sec,omitempty"`

	// PrefixCacheHitRatio is windowed over the scrape interval rather than
	// cumulative since boot, so it reflects what the cache is doing now
	// instead of being dominated by hours-old history.
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

	// Speculative decoding, when the runtime is configured for it. The two
	// derived figures answer different questions: the acceptance rate is how
	// much of the speculated work survived, while accepted-per-draft compared
	// against the configured number of speculative tokens says whether that
	// setting is earning its cost. Both are windowed, like the cache hit
	// ratio, so they describe current behaviour rather than since-boot.
	SpecDecodeDraftTokensTotal    *int64   `json:"spec_decode_draft_tokens_total,omitempty"`
	SpecDecodeAcceptedTokensTotal *int64   `json:"spec_decode_accepted_tokens_total,omitempty"`
	SpecDecodeAcceptanceRate      *float64 `json:"spec_decode_acceptance_rate,omitempty"`   // 0..1
	SpecDecodeAcceptedPerDraft    *float64 `json:"spec_decode_accepted_per_draft,omitempty"` // tokens
}

// Endpoint describes something to scrape.
type Endpoint struct {
	URL     string // base URL, e.g. http://127.0.0.1:8035
	Runtime string // RuntimeAuto (default) | RuntimeVLLM | RuntimeLlamaCpp
	APIKey  string // optional bearer token, for a protected /metrics
}

// counterState holds the cumulative counters from an endpoint's previous
// scrape, so per-second rates and windowed ratios can be derived.
type counterState struct {
	promptTokens  float64
	genTokens     float64
	prefixQueries float64
	prefixHits    float64
	preemptions   float64
	ttftSum       float64
	ttftCount     float64
	tpotSum       float64
	tpotCount     float64
	specDrafts    float64
	specDraftToks float64
	specAccepted  float64
	at            time.Time
}
