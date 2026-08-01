package collector

import (
	"time"

	"sentinel/agent/internal/config"
	"sentinel/agent/internal/models"
	"sentinel/llmscrape"
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

	llmConfig config.LLM
	llm       *llmState
	// llmScraper retains per-endpoint counters for rate derivation;
	// llmProbe is separate so background discovery never touches that state.
	llmScraper *llmscrape.Scraper
	llmProbe   *llmscrape.Scraper
}

func New(llmCfg config.LLM) *Collector {
	return &Collector{
		prevDiskIO: make(map[string]diskIOState),
		llmConfig:  llmCfg,
		llm: &llmState{
			endpoints:  make(map[string]llmscrape.Endpoint),
			negative:   make(map[string]time.Time),
			fromServer: make(map[string]llmscrape.Endpoint),
			disabled:   make(map[string]bool),
		},
		// Inference endpoints are local, so short timeouts are right: a slow
		// one must never delay the tick reporting CPU/mem/disk/GPU.
		llmScraper: llmscrape.NewScraper(scrapeTimeout),
		llmProbe:   llmscrape.NewScraper(probeTimeout),
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
