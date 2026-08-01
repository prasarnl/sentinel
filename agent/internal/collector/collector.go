package collector

import (
	"net/http"
	"time"

	"sentinel/agent/internal/config"
	"sentinel/agent/internal/models"
)

type diskIOState struct {
	readBytes  uint64
	writeBytes uint64
	at         time.Time
}

// Collector holds state needed across ticks (e.g. previous disk I/O and LLM
// runtime counters) so per-second rates can be derived from cumulative
// counters.
type Collector struct {
	prevDiskIO map[string]diskIOState

	llmConfig    config.LLM
	llmEndpoints map[string]*llmEndpointState
	prevLLM      map[string]llmCounterState
	lastLLMProbe time.Time
	llmClient    *http.Client
}

func New(llmCfg config.LLM) *Collector {
	return &Collector{
		prevDiskIO:   make(map[string]diskIOState),
		llmConfig:    llmCfg,
		llmEndpoints: make(map[string]*llmEndpointState),
		prevLLM:      make(map[string]llmCounterState),
		// Inference endpoints are local, so a short timeout is right: a slow
		// one must never delay the tick that reports CPU/mem/disk/GPU.
		llmClient: &http.Client{Timeout: llmScrapeTimeout},
	}
}

// CollectAll gathers one sample of every metric family. Each collector is
// best-effort: a failure in one (e.g. no GPU present) never blocks the
// others from being reported.
func (c *Collector) CollectAll() models.IngestPayload {
	now := time.Now().UTC()
	payload := models.IngestPayload{}

	if cpu, err := c.collectCPU(now); err == nil {
		payload.CPU = []models.CPUSample{cpu}
	}
	if mem, err := c.collectMem(now); err == nil {
		payload.Mem = []models.MemSample{mem}
	}
	payload.Disk = c.collectDisk(now)
	payload.GPU = c.collectGPU(now)
	payload.LLM = c.collectLLM(now)

	return payload
}
