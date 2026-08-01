package api

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"

	"sentinel/server/internal/models"
)

// Ingest accepts a batch of metric samples from an agent, authenticated via
// the X-API-Key header. Samples are arrays (rather than single points) so an
// agent can catch up after a network outage using its local retry buffer.
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
	for _, l := range payload.LLM {
		batch.Queue(
			`INSERT INTO metrics_llm (
				time, host_id, endpoint, runtime, model,
				kv_cache_usage_ratio, kv_cache_tokens,
				prompt_tokens_total, generated_tokens_total,
				prompt_tokens_per_sec, generated_tokens_per_sec,
				prefix_cache_queries_total, prefix_cache_hits_total, prefix_cache_hit_ratio,
				requests_running, requests_waiting,
				ttft_ms_avg, tpot_ms_avg, preemptions_per_sec
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
			l.Time, hostID, l.Endpoint, l.Runtime, nullIfEmpty(l.Model),
			l.KVCacheUsageRatio, l.KVCacheTokens,
			l.PromptTokensTotal, l.GeneratedTokensTotal,
			l.PromptTokensPerSec, l.GeneratedTokensPerSec,
			l.PrefixCacheQueriesTotal, l.PrefixCacheHitsTotal, l.PrefixCacheHitRatio,
			l.RequestsRunning, l.RequestsWaiting,
			l.TTFTMsAvg, l.TPOTMsAvg, l.PreemptionsPerSec,
		)
	}
	batch.Queue(`UPDATE hosts SET last_seen = now(), status = 'online' WHERE id = $1`, hostID)

	br := s.Pool.SendBatch(r.Context(), batch)
	if err := br.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store metrics")
		return
	}

	if msg, err := json.Marshal(map[string]any{"host_id": hostID, "payload": payload}); err == nil {
		s.Hub.Publish(hostID, msg)
	}

	w.WriteHeader(http.StatusAccepted)
}

// nullIfEmpty stores an unknown model name as NULL rather than "", keeping
// "the agent couldn't determine it" distinct from a genuinely empty name.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
