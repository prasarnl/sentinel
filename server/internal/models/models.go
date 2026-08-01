package models

import (
	"time"

	"sentinel/llmscrape"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type HostOS string

const (
	OSLinux   HostOS = "linux"
	OSWindows HostOS = "windows"
)

type HostStatus string

const (
	StatusPending HostStatus = "pending"
	StatusOnline  HostStatus = "online"
	StatusOffline HostStatus = "offline"
	StatusRemoved HostStatus = "removed"
)

type Host struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	OS                HostOS     `json:"os"`
	Tags              []string   `json:"tags"`
	Status            HostStatus `json:"status"`
	LastSeen          *time.Time `json:"last_seen"`
	CreatedAt         time.Time  `json:"created_at"`
	EnrollmentToken   *string    `json:"enrollment_token,omitempty"`
	EnrollmentExpires *time.Time `json:"enrollment_expires,omitempty"`
}

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
// normalized across backends (llama.cpp, vLLM). It is an alias rather than
// a copy so the agent, the server, and the shared scraper cannot drift out
// of sync on field names or JSON tags.
type LLMSample = llmscrape.Sample

// DiscoveredLLMEndpoint is something an agent's socket discovery found on its
// host. Reporting these upward is what puts autodetected endpoints in the
// registry, so they can be named or disabled from the UI.
type DiscoveredLLMEndpoint struct {
	URL     string `json:"url"`
	Runtime string `json:"runtime"`
}

type IngestPayload struct {
	CPU  []CPUSample  `json:"cpu,omitempty"`
	Mem  []MemSample  `json:"mem,omitempty"`
	Disk []DiskSample `json:"disk,omitempty"`
	GPU  []GPUSample  `json:"gpu,omitempty"`
	LLM  []LLMSample  `json:"llm,omitempty"`

	DiscoveredLLM []DiscoveredLLMEndpoint `json:"discovered_llm,omitempty"`
}

// IngestResponse is returned from /ingest. The agent has no other channel for
// receiving configuration — it only ever pushes — so the response to the push
// it already makes is how server-side settings reach it, with no extra
// endpoint, request, or auth path.
type IngestResponse struct {
	LLMEndpoints []AgentLLMEndpoint `json:"llm_endpoints"`
	// DisabledLLMEndpoints are URLs the agent must not scrape even if its own
	// discovery finds them. Without this an operator could disable an
	// endpoint and have autodetect immediately rediscover and resume it.
	DisabledLLMEndpoints []string `json:"disabled_llm_endpoints,omitempty"`
}

// AgentLLMEndpoint is the subset of a registry entry an agent needs to scrape
// it. Disabled endpoints are simply omitted rather than sent with a flag, so
// an older agent that ignores this field cannot keep scraping something an
// operator switched off.
type AgentLLMEndpoint struct {
	URL     string `json:"url"`
	Runtime string `json:"runtime,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

// LLMEndpoint is a registered inference endpoint to scrape. A nil HostID
// means no agent can reach it and the server polls it directly.
type LLMEndpoint struct {
	ID        string  `json:"id"`
	HostID    *string `json:"host_id"`
	HostName  *string `json:"host_name,omitempty"` // joined for display
	Name      *string `json:"name"`
	URL       string  `json:"url"`
	Runtime   string  `json:"runtime"`
	APIKey    *string `json:"-"` // sent outbound, so stored reversibly; never serialized
	HasAPIKey bool    `json:"has_api_key"`
	Enabled   bool    `json:"enabled"`
	Source    string  `json:"source"` // manual | autodetected

	// LastScrapeError distinguishes "unreachable", "reachable but publishes
	// no metrics", and "working" — without it all three look identical in
	// the UI, as an endpoint with no data.
	LastScrapeAt    *time.Time `json:"last_scrape_at"`
	LastScrapeError *string    `json:"last_scrape_error"`

	// Model is the most recently observed model name, for display.
	Model     *string   `json:"model,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	// BenchmarkURL is how the server would reach this endpoint, which differs
	// from URL whenever an agent scrapes it over loopback. A suggestion only,
	// meant to be confirmed before use.
	BenchmarkURL string `json:"benchmark_url"`
	// BenchmarkTargetID is set when this address is already registered as a
	// benchmark target, so the UI can link to it instead of offering to
	// create a duplicate.
	BenchmarkTargetID   *string `json:"benchmark_target_id"`
	BenchmarkTargetName *string `json:"benchmark_target_name"`
}

// LLMTarget is an OpenAI-compatible inference endpoint the server can run
// benchmarks against directly (no agent involved). Which model(s) to test
// are chosen per benchmark run, not fixed on the target.
type LLMTarget struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	BaseURL           string    `json:"base_url"`
	APIKey            *string   `json:"-"` // never serialized; HasAPIKey conveys presence to clients
	HasAPIKey         bool      `json:"has_api_key"`
	SupportsModelSwap bool      `json:"supports_model_swap"` // sits behind a swap-capable proxy (e.g. llama-swap): enables multi-model runs + auto unload/load
	CreatedAt         time.Time `json:"created_at"`
}

// LLMBenchmarkConfig holds the saved, editable parameters for benchmark
// runs against a target. One row per target.
type LLMBenchmarkConfig struct {
	TargetID             string `json:"target_id"`
	Concurrency          int    `json:"concurrency"`
	NumRequests          int    `json:"num_requests"`
	WarmupRequests       int    `json:"warmup_requests"`
	PromptTokens         int    `json:"prompt_tokens"`
	MaxTokens            int    `json:"max_tokens"`
	RequestTimeoutSecs   int    `json:"request_timeout_secs"`
	ModelLoadTimeoutSecs int    `json:"model_load_timeout_secs"`
	// ContextWindow/BatchSize are optional, best-effort: sent as extra
	// n_ctx/n_batch fields on each request. Most llama.cpp-family servers
	// fix these at process launch, so whether they have any effect
	// depends on the target's backend; nil means don't send them.
	ContextWindow *int      `json:"context_window,omitempty"`
	BatchSize     *int      `json:"batch_size,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DefaultLLMBenchmarkConfig returns sensible out-of-the-box benchmark
// parameters: a quick single-stream baseline with a chat-sized prompt.
func DefaultLLMBenchmarkConfig(targetID string) LLMBenchmarkConfig {
	return LLMBenchmarkConfig{
		TargetID:             targetID,
		Concurrency:          1,
		NumRequests:          10,
		WarmupRequests:       1,
		PromptTokens:         512,
		MaxTokens:            128,
		RequestTimeoutSecs:   120,
		ModelLoadTimeoutSecs: 180,
	}
}

type LLMBenchmarkRunStatus string

const (
	BenchmarkRunning   LLMBenchmarkRunStatus = "running"
	BenchmarkCompleted LLMBenchmarkRunStatus = "completed"
	BenchmarkFailed    LLMBenchmarkRunStatus = "failed"
	BenchmarkCancelled LLMBenchmarkRunStatus = "cancelled"
)

// LatencyStats summarizes a distribution of per-request measurements.
type LatencyStats struct {
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	Mean float64 `json:"mean"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

// BenchmarkSummary is the aggregate result of a completed benchmark run.
type BenchmarkSummary struct {
	Requests               int          `json:"requests"`
	Failed                 int          `json:"failed"`
	Errors                 []string     `json:"errors,omitempty"` // sample of distinct failed-request error messages
	WallTimeMs             float64      `json:"wall_time_ms"`
	ThroughputTokensPerSec float64      `json:"throughput_tokens_per_sec"`
	TTFTMs                 LatencyStats `json:"ttft_ms"`
	TokensPerSec           LatencyStats `json:"tokens_per_sec"`
}

type LLMBenchmarkRun struct {
	ID          string                `json:"id"`
	TargetID    string                `json:"target_id"`
	Model       string                `json:"model"`
	BatchID     *string               `json:"batch_id,omitempty"`
	Config      LLMBenchmarkConfig    `json:"config"`
	Status      LLMBenchmarkRunStatus `json:"status"`
	Summary     *BenchmarkSummary     `json:"summary,omitempty"`
	Error       *string               `json:"error,omitempty"`
	StartedAt   time.Time             `json:"started_at"`
	CompletedAt *time.Time            `json:"completed_at,omitempty"`
}

// BenchmarkStage describes what an in-flight batch is currently doing, for
// a plain-language progress UI.
type BenchmarkStage string

const (
	StageUnloading    BenchmarkStage = "unloading"
	StageLoading      BenchmarkStage = "loading"
	StageBenchmarking BenchmarkStage = "benchmarking"
	StageModelDone    BenchmarkStage = "model_done"
	StageDone         BenchmarkStage = "done"
)

// BenchmarkProgressEvent is published over the websocket hub (keyed by the
// batch token) as a multi-model benchmark run progresses, so the UI can
// show live, per-model status.
type BenchmarkProgressEvent struct {
	BatchID          string           `json:"batch_id"`
	Model            string           `json:"model,omitempty"`
	ModelIndex       int              `json:"model_index,omitempty"`
	ModelsTotal      int              `json:"models_total,omitempty"`
	Stage            BenchmarkStage   `json:"stage"`
	Completed        int              `json:"completed"`
	Total            int              `json:"total"`
	Failed           int              `json:"failed"`
	LastTTFTMs       float64          `json:"last_ttft_ms,omitempty"`
	LastTokensPerSec float64          `json:"last_tokens_per_sec,omitempty"`
	LastError        string           `json:"last_error,omitempty"` // set when the most recently completed request failed
	Done             bool             `json:"done"`
	Run              *LLMBenchmarkRun `json:"run,omitempty"` // present when stage=="model_done"
}
