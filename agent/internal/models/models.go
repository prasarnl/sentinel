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

type IngestPayload struct {
	CPU  []CPUSample  `json:"cpu,omitempty"`
	Mem  []MemSample  `json:"mem,omitempty"`
	Disk []DiskSample `json:"disk,omitempty"`
	GPU  []GPUSample  `json:"gpu,omitempty"`
	LLM  []LLMSample  `json:"llm,omitempty"`
}
