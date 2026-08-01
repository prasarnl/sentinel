package llmscrape

import (
	"context"
	"encoding/json"
	"strings"
)

// lmStudioModels is the subset of LM Studio's native /api/v0/models response
// that describes residency. Its OpenAI-compatible /v1/models lists everything
// installed with no indication of what is actually resident, so the native
// endpoint is the only one that answers "what is loaded right now".
type lmStudioModels struct {
	Data []struct {
		ID                  string `json:"id"`
		State               string `json:"state"` // loaded | not-loaded
		Type                string `json:"type"`  // llm | vlm | embeddings
		Arch                string `json:"arch"`
		Quantization        string `json:"quantization"`
		LoadedContextLength int    `json:"loaded_context_length"`
		MaxContextLength    int    `json:"max_context_length"`
	} `json:"data"`
}

// scrapeLMStudio reports what an LM Studio server currently has loaded.
//
// LM Studio publishes no Prometheus metrics at all — no KV cache, no
// throughput, no queue depth — so this deliberately returns a sample with
// only residency populated and every performance metric nil. That is the
// honest representation: those numbers do not exist for this runtime, and
// inventing plausible-looking ones would be worse than showing nothing.
//
// Returns ErrNoMetrics if this isn't an LM Studio server, leaving the caller's
// original diagnosis intact.
func (s *Scraper) scrapeLMStudio(ctx context.Context, url, apiKey string) (Sample, error) {
	body, err := s.get(ctx, url+"/api/v0/models", apiKey)
	if err != nil {
		return Sample{}, ErrNoMetrics
	}

	var out lmStudioModels
	if err := json.Unmarshal([]byte(body), &out); err != nil || len(out.Data) == 0 {
		return Sample{}, ErrNoMetrics
	}

	sample := Sample{Endpoint: url, Runtime: RuntimeLMStudio}
	for _, m := range out.Data {
		if !strings.EqualFold(m.State, "loaded") {
			continue
		}
		sample.Model = m.ID
		sample.Quantization = m.Quantization
		if m.LoadedContextLength > 0 {
			n := m.LoadedContextLength
			sample.ContextLength = &n
		}
		if m.MaxContextLength > 0 {
			n := m.MaxContextLength
			sample.MaxContextLength = &n
		}
		break
	}

	// A server with models installed but none loaded is still a healthy,
	// reachable LM Studio; Model stays empty and the UI says so.
	sample.ModelsInstalled = len(out.Data)
	return sample, nil
}
