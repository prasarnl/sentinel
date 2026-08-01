package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sentinel/server/internal/models"
)

// Ingest accepts a batch of metric samples from an agent, authenticated via
// the X-API-Key header. Samples are arrays (rather than single points) so an
// agent can catch up after a network outage using its local retry buffer.
//
// The response carries the host's LLM endpoint configuration back down. The
// agent has no other channel for receiving settings — it only pushes — so
// answering the push it already makes avoids inventing a second endpoint,
// request, and auth path just to distribute config.
func (s *Server) Ingest(w http.ResponseWriter, r *http.Request) {
	hostID, ok := s.hostFromAPIKey(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid or missing api key")
		return
	}

	var payload models.IngestPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()

	// Register anything new before resolving ids, so a freshly discovered
	// endpoint's very first samples are attributed rather than dropped.
	if err := registerDiscoveredEndpoints(ctx, s.Pool, hostID, discoveriesFrom(payload)); err != nil {
		log.Printf("register discovered endpoints for host %s: %v", hostID, err)
	}
	endpoints, err := endpointIDsForHost(ctx, s.Pool, hostID)
	if err != nil {
		log.Printf("resolve endpoint ids for host %s: %v", hostID, err)
	}

	batch := &pgx.Batch{}
	for _, c := range payload.CPU {
		batch.Queue(
			`INSERT INTO metrics_cpu (time, host_id, usage_pct, load1, load5, load15) VALUES ($1,$2,$3,$4,$5,$6)`,
			c.Time, hostID, c.UsagePct, c.Load1, c.Load5, c.Load15,
		)
	}
	for _, m := range payload.Mem {
		batch.Queue(
			`INSERT INTO metrics_mem (time, host_id, total_bytes, used_bytes, available_bytes, swap_used_bytes, swap_total_bytes) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			m.Time, hostID, m.TotalBytes, m.UsedBytes, m.AvailableBytes, m.SwapUsedBytes, m.SwapTotalBytes,
		)
	}
	for _, d := range payload.Disk {
		batch.Queue(
			`INSERT INTO metrics_disk (time, host_id, mount, total_bytes, used_bytes, read_bytes_sec, write_bytes_sec) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			d.Time, hostID, d.Mount, d.TotalBytes, d.UsedBytes, d.ReadBytesSec, d.WriteBytesSec,
		)
	}
	for _, g := range payload.GPU {
		batch.Queue(
			`INSERT INTO metrics_gpu (time, host_id, gpu_index, vendor, name, utilization_pct, mem_used_bytes, mem_total_bytes, temp_c) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			g.Time, hostID, g.GPUIndex, g.Vendor, g.Name, g.UtilizationPct, g.MemUsedBytes, g.MemTotalBytes, g.TempC,
		)
	}
	kept := payload.LLM[:0:0]
	for _, l := range payload.LLM {
		ep, known := endpoints[strings.TrimSuffix(l.Endpoint, "/")]
		// An agent running ahead of the server's view, or one still scraping
		// something just disabled, must not write rows: an unattributed
		// sample can never be shown or managed.
		if !known || !ep.enabled {
			continue
		}
		kept = append(kept, l)
		batch.Queue(
			`INSERT INTO metrics_llm (
				time, host_id, endpoint_id, endpoint, runtime, model,
				kv_cache_usage_ratio, kv_cache_tokens,
				prompt_tokens_total, generated_tokens_total,
				prompt_tokens_per_sec, generated_tokens_per_sec,
				prefix_cache_queries_total, prefix_cache_hits_total, prefix_cache_hit_ratio,
				requests_running, requests_waiting,
				ttft_ms_avg, tpot_ms_avg, preemptions_per_sec,
				spec_decode_acceptance_rate, spec_decode_accepted_per_draft
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
			l.Time, hostID, ep.id, l.Endpoint, l.Runtime, nullIfEmpty(l.Model),
			l.KVCacheUsageRatio, l.KVCacheTokens,
			l.PromptTokensTotal, l.GeneratedTokensTotal,
			l.PromptTokensPerSec, l.GeneratedTokensPerSec,
			l.PrefixCacheQueriesTotal, l.PrefixCacheHitsTotal, l.PrefixCacheHitRatio,
			l.RequestsRunning, l.RequestsWaiting,
			l.TTFTMsAvg, l.TPOTMsAvg, l.PreemptionsPerSec,
			l.SpecDecodeAcceptanceRate, l.SpecDecodeAcceptedPerDraft,
		)
		batch.Queue(
			`UPDATE llm_endpoints SET last_scrape_at = $2, last_scrape_error = NULL WHERE id = $1`,
			ep.id, l.Time,
		)
	}
	payload.LLM = kept
	batch.Queue(`UPDATE hosts SET last_seen = now(), status = 'online' WHERE id = $1`, hostID)

	br := s.Pool.SendBatch(ctx, batch)
	if err := br.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store metrics")
		return
	}

	if msg, err := json.Marshal(map[string]any{"host_id": hostID, "payload": payload}); err == nil {
		s.Hub.Publish(hostID, msg)
	}

	resp := models.IngestResponse{}
	if resp.LLMEndpoints, err = agentEndpointsFor(ctx, s.Pool, hostID); err != nil {
		log.Printf("load agent endpoints for host %s: %v", hostID, err)
	}
	if disabled, err := disabledEndpointsFor(ctx, s.Pool, hostID); err == nil {
		for url := range disabled {
			resp.DisabledLLMEndpoints = append(resp.DisabledLLMEndpoints, url)
		}
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// discoveriesFrom merges what the agent explicitly reported as discovered
// with the endpoints its samples came from. Samples are the more reliable
// signal — an endpoint that produced one is definitely live — while the
// explicit list also covers endpoints found but not scraped successfully.
func discoveriesFrom(p models.IngestPayload) []models.DiscoveredLLMEndpoint {
	seen := map[string]bool{}
	out := make([]models.DiscoveredLLMEndpoint, 0, len(p.DiscoveredLLM)+len(p.LLM))

	add := func(url, runtime string) {
		url = strings.TrimSuffix(strings.TrimSpace(url), "/")
		if url == "" || seen[url] {
			return
		}
		seen[url] = true
		out = append(out, models.DiscoveredLLMEndpoint{URL: url, Runtime: runtime})
	}
	for _, s := range p.LLM {
		add(s.Endpoint, s.Runtime)
	}
	for _, d := range p.DiscoveredLLM {
		add(d.URL, d.Runtime)
	}
	return out
}

type endpointRef struct {
	id      string
	enabled bool
}

func endpointIDsForHost(ctx context.Context, pool *pgxpool.Pool, hostID string) (map[string]endpointRef, error) {
	rows, err := pool.Query(ctx, `SELECT id, url, enabled FROM llm_endpoints WHERE host_id = $1`, hostID)
	if err != nil {
		return map[string]endpointRef{}, err
	}
	defer rows.Close()

	out := map[string]endpointRef{}
	for rows.Next() {
		var id, url string
		var enabled bool
		if err := rows.Scan(&id, &url, &enabled); err != nil {
			return out, err
		}
		out[url] = endpointRef{id: id, enabled: enabled}
	}
	return out, rows.Err()
}

// nullIfEmpty stores an unknown model name as NULL rather than "", keeping
// "the agent couldn't determine it" distinct from a genuinely empty name.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
