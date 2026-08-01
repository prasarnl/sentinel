package models

import (
	"time"

	"sentinel/llmscrape"
)

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

// DiscoveredLLMEndpoint is something socket discovery found on this host.
// Reporting these upward is what puts autodetected endpoints in the server's
// registry, so an operator can name or disable them from the UI.
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

// IngestResponse is what the server returns from /ingest. The agent only ever
// pushes, so the response to that push is its one channel for receiving
// configuration — no second endpoint, request, or auth path needed.
type IngestResponse struct {
	LLMEndpoints []AgentLLMEndpoint `json:"llm_endpoints"`
	// DisabledLLMEndpoints are URLs not to scrape even when discovery finds
	// them, so disabling one in the UI isn't undone by autodetection.
	DisabledLLMEndpoints []string `json:"disabled_llm_endpoints,omitempty"`
}

// AgentLLMEndpoint is the subset of a registry entry needed to scrape it.
type AgentLLMEndpoint struct {
	URL     string `json:"url"`
	Runtime string `json:"runtime,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}
