package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// LatestSnapshot returns the most recent sample from each metric family for
// a host, used to populate dashboard cards without waiting for a websocket
// push.
func (s *Server) LatestSnapshot(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "id")
	ctx := r.Context()

	result := map[string]any{}

	var cpu map[string]any
	row := s.Pool.QueryRow(ctx, `SELECT time, usage_pct, load1, load5, load15 FROM metrics_cpu WHERE host_id = $1 ORDER BY time DESC LIMIT 1`, hostID)
	cpu = scanCPU(row)
	if cpu != nil {
		result["cpu"] = cpu
	}

	row = s.Pool.QueryRow(ctx, `SELECT time, total_bytes, used_bytes, available_bytes, swap_used_bytes, swap_total_bytes FROM metrics_mem WHERE host_id = $1 ORDER BY time DESC LIMIT 1`, hostID)
	if mem := scanMem(row); mem != nil {
		result["mem"] = mem
	}

	diskRows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT ON (mount) time, mount, total_bytes, used_bytes, read_bytes_sec, write_bytes_sec
		FROM metrics_disk WHERE host_id = $1 ORDER BY mount, time DESC`, hostID)
	if err == nil {
		defer diskRows.Close()
		disks := []map[string]any{}
		for diskRows.Next() {
			var t time.Time
			var mount string
			var total, used int64
			var read, write float64
			if diskRows.Scan(&t, &mount, &total, &used, &read, &write) == nil {
				disks = append(disks, map[string]any{
					"time": t, "mount": mount, "total_bytes": total, "used_bytes": used,
					"read_bytes_sec": read, "write_bytes_sec": write,
				})
			}
		}
		result["disk"] = disks
	}

	gpuRows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT ON (gpu_index) time, gpu_index, vendor, name, utilization_pct, mem_used_bytes, mem_total_bytes, temp_c
		FROM metrics_gpu WHERE host_id = $1 ORDER BY gpu_index, time DESC`, hostID)
	if err == nil {
		defer gpuRows.Close()
		gpus := []map[string]any{}
		for gpuRows.Next() {
			var t time.Time
			var idx int
			var vendor, name string
			var util, temp *float64
			var memUsed, memTotal *int64
			if gpuRows.Scan(&t, &idx, &vendor, &name, &util, &memUsed, &memTotal, &temp) == nil {
				gpus = append(gpus, map[string]any{
					"time": t, "gpu_index": idx, "vendor": vendor, "name": name,
					"utilization_pct": util, "mem_used_bytes": memUsed, "mem_total_bytes": memTotal, "temp_c": temp,
				})
			}
		}
		result["gpu"] = gpus
	}

	llmRows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT ON (endpoint) `+llmColumns+`
		FROM metrics_llm WHERE host_id = $1 ORDER BY endpoint, time DESC`, hostID)
	if err == nil {
		defer llmRows.Close()
		llms := []map[string]any{}
		for llmRows.Next() {
			if llm := scanLLM(llmRows); llm != nil {
				llms = append(llms, llm)
			}
		}
		result["llm"] = llms
	}

	writeJSON(w, http.StatusOK, result)
}

// llmColumns is shared by the snapshot and raw-history queries so the two
// stay in step with scanLLM's field order.
const llmColumns = `time, endpoint, runtime, model,
	kv_cache_usage_ratio, kv_cache_tokens,
	prompt_tokens_total, generated_tokens_total,
	prompt_tokens_per_sec, generated_tokens_per_sec,
	prefix_cache_queries_total, prefix_cache_hits_total, prefix_cache_hit_ratio,
	requests_running, requests_waiting,
	ttft_ms_avg, tpot_ms_avg, preemptions_per_sec`

// scanLLM reads one metrics_llm row. Nullable metrics stay nil in the JSON so
// the dashboard can hide a tile the runtime cannot report, rather than
// showing a misleading zero.
func scanLLM(rows pgx.Rows) map[string]any {
	var (
		t                             time.Time
		endpoint, runtime             string
		model                         *string
		kvRatio                       *float64
		kvTokens                      *int64
		promptTotal, genTotal         *int64
		promptPerSec, genPerSec       *float64
		prefixQueries, prefixHits     *int64
		prefixRatio                   *float64
		running, waiting              *int
		ttft, tpot, preemptionsPerSec *float64
	)
	if rows.Scan(&t, &endpoint, &runtime, &model,
		&kvRatio, &kvTokens,
		&promptTotal, &genTotal,
		&promptPerSec, &genPerSec,
		&prefixQueries, &prefixHits, &prefixRatio,
		&running, &waiting,
		&ttft, &tpot, &preemptionsPerSec) != nil {
		return nil
	}
	return map[string]any{
		"time": t, "endpoint": endpoint, "runtime": runtime, "model": model,
		"kv_cache_usage_ratio": kvRatio, "kv_cache_tokens": kvTokens,
		"prompt_tokens_total": promptTotal, "generated_tokens_total": genTotal,
		"prompt_tokens_per_sec": promptPerSec, "generated_tokens_per_sec": genPerSec,
		"prefix_cache_queries_total": prefixQueries, "prefix_cache_hits_total": prefixHits,
		"prefix_cache_hit_ratio": prefixRatio,
		"requests_running":       running, "requests_waiting": waiting,
		"ttft_ms_avg": ttft, "tpot_ms_avg": tpot, "preemptions_per_sec": preemptionsPerSec,
	}
}

func scanCPU(row pgx.Row) map[string]any {
	var t time.Time
	var usage float64
	var l1, l5, l15 *float64
	if row.Scan(&t, &usage, &l1, &l5, &l15) != nil {
		return nil
	}
	return map[string]any{"time": t, "usage_pct": usage, "load1": l1, "load5": l5, "load15": l15}
}

func scanMem(row pgx.Row) map[string]any {
	var t time.Time
	var total, used, avail, swapUsed, swapTotal int64
	if row.Scan(&t, &total, &used, &avail, &swapUsed, &swapTotal) != nil {
		return nil
	}
	return map[string]any{
		"time": t, "total_bytes": total, "used_bytes": used, "available_bytes": avail,
		"swap_used_bytes": swapUsed, "swap_total_bytes": swapTotal,
	}
}

// rawCutoff is how far back a history query still hits raw tables; anything
// further back reads from the 1-minute continuous aggregates instead so
// multi-week ranges stay fast.
const rawCutoff = 6 * time.Hour

func parseRange(r *http.Request) time.Duration {
	switch r.URL.Query().Get("range") {
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	case "90d":
		return 90 * 24 * time.Hour
	default:
		return time.Hour
	}
}

func (s *Server) HistoryCPU(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "id")
	rng := parseRange(r)
	since := time.Now().Add(-rng)
	ctx := r.Context()

	type point struct {
		Time     time.Time `json:"time"`
		UsagePct float64   `json:"usage_pct"`
	}
	points := []point{}

	if rng <= rawCutoff {
		rows, err := s.Pool.Query(ctx, `SELECT time, usage_pct FROM metrics_cpu WHERE host_id = $1 AND time >= $2 ORDER BY time`, hostID, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UsagePct) == nil {
				points = append(points, p)
			}
		}
	} else {
		rows, err := s.Pool.Query(ctx, `SELECT bucket, avg_usage_pct FROM metrics_cpu_1m WHERE host_id = $1 AND bucket >= $2 ORDER BY bucket`, hostID, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UsagePct) == nil {
				points = append(points, p)
			}
		}
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) HistoryMem(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "id")
	rng := parseRange(r)
	since := time.Now().Add(-rng)
	ctx := r.Context()

	type point struct {
		Time      time.Time `json:"time"`
		UsedBytes float64   `json:"used_bytes"`
		TotalBytes float64  `json:"total_bytes"`
	}
	points := []point{}

	if rng <= rawCutoff {
		rows, err := s.Pool.Query(ctx, `SELECT time, used_bytes, total_bytes FROM metrics_mem WHERE host_id = $1 AND time >= $2 ORDER BY time`, hostID, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UsedBytes, &p.TotalBytes) == nil {
				points = append(points, p)
			}
		}
	} else {
		rows, err := s.Pool.Query(ctx, `SELECT bucket, avg_used_bytes, avg_total_bytes FROM metrics_mem_1m WHERE host_id = $1 AND bucket >= $2 ORDER BY bucket`, hostID, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UsedBytes, &p.TotalBytes) == nil {
				points = append(points, p)
			}
		}
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) HistoryDisk(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "id")
	mount := r.URL.Query().Get("mount")
	if mount == "" {
		writeError(w, http.StatusBadRequest, "mount query parameter is required")
		return
	}
	rng := parseRange(r)
	since := time.Now().Add(-rng)
	ctx := r.Context()

	type point struct {
		Time          time.Time `json:"time"`
		UsedBytes     float64   `json:"used_bytes"`
		TotalBytes    float64   `json:"total_bytes"`
		ReadBytesSec  float64   `json:"read_bytes_sec"`
		WriteBytesSec float64   `json:"write_bytes_sec"`
	}
	points := []point{}

	if rng <= rawCutoff {
		rows, err := s.Pool.Query(ctx, `SELECT time, used_bytes, total_bytes, read_bytes_sec, write_bytes_sec FROM metrics_disk WHERE host_id = $1 AND mount = $2 AND time >= $3 ORDER BY time`, hostID, mount, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UsedBytes, &p.TotalBytes, &p.ReadBytesSec, &p.WriteBytesSec) == nil {
				points = append(points, p)
			}
		}
	} else {
		rows, err := s.Pool.Query(ctx, `SELECT bucket, avg_used_bytes, avg_total_bytes, avg_read_bytes_sec, avg_write_bytes_sec FROM metrics_disk_1m WHERE host_id = $1 AND mount = $2 AND bucket >= $3 ORDER BY bucket`, hostID, mount, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UsedBytes, &p.TotalBytes, &p.ReadBytesSec, &p.WriteBytesSec) == nil {
				points = append(points, p)
			}
		}
	}
	writeJSON(w, http.StatusOK, points)
}

// HistoryLLM returns inference-runtime history for one endpoint on a host.
// Counts are cast to float8 so raw rows and the averaged 1-minute rollups
// share a single point shape, as HistoryGPU does for byte counters.
func (s *Server) HistoryLLM(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "id")
	endpoint := r.URL.Query().Get("endpoint")
	if endpoint == "" {
		writeError(w, http.StatusBadRequest, "endpoint query parameter is required")
		return
	}
	rng := parseRange(r)
	since := time.Now().Add(-rng)
	ctx := r.Context()

	type point struct {
		Time                  time.Time `json:"time"`
		KVCacheUsageRatio     *float64  `json:"kv_cache_usage_ratio"`
		PromptTokensPerSec    *float64  `json:"prompt_tokens_per_sec"`
		GeneratedTokensPerSec *float64  `json:"generated_tokens_per_sec"`
		PrefixCacheHitRatio   *float64  `json:"prefix_cache_hit_ratio"`
		RequestsRunning       *float64  `json:"requests_running"`
		RequestsWaiting       *float64  `json:"requests_waiting"`
		TTFTMsAvg             *float64  `json:"ttft_ms_avg"`
		TPOTMsAvg             *float64  `json:"tpot_ms_avg"`
		PreemptionsPerSec     *float64  `json:"preemptions_per_sec"`
	}
	points := []point{}

	query := `SELECT time, kv_cache_usage_ratio, prompt_tokens_per_sec, generated_tokens_per_sec,
			prefix_cache_hit_ratio, requests_running::float8, requests_waiting::float8,
			ttft_ms_avg, tpot_ms_avg, preemptions_per_sec
		FROM metrics_llm WHERE host_id = $1 AND endpoint = $2 AND time >= $3 ORDER BY time`
	if rng > rawCutoff {
		// avg() over the INT request counts yields numeric, so it is cast
		// back to float8 to match the point shape the raw branch produces.
		query = `SELECT bucket, avg_kv_cache_usage_ratio, avg_prompt_tokens_per_sec, avg_generated_tokens_per_sec,
				avg_prefix_cache_hit_ratio, avg_requests_running::float8, avg_requests_waiting::float8,
				avg_ttft_ms, avg_tpot_ms, avg_preemptions_per_sec
			FROM metrics_llm_1m WHERE host_id = $1 AND endpoint = $2 AND bucket >= $3 ORDER BY bucket`
	}

	rows, err := s.Pool.Query(ctx, query, hostID, endpoint, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p point
		if rows.Scan(&p.Time, &p.KVCacheUsageRatio, &p.PromptTokensPerSec, &p.GeneratedTokensPerSec,
			&p.PrefixCacheHitRatio, &p.RequestsRunning, &p.RequestsWaiting,
			&p.TTFTMsAvg, &p.TPOTMsAvg, &p.PreemptionsPerSec) == nil {
			points = append(points, p)
		}
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) HistoryGPU(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "id")
	gpuIndex := r.URL.Query().Get("gpu_index")
	if gpuIndex == "" {
		gpuIndex = "0"
	}
	rng := parseRange(r)
	since := time.Now().Add(-rng)
	ctx := r.Context()

	type point struct {
		Time           time.Time `json:"time"`
		UtilizationPct *float64  `json:"utilization_pct"`
		MemUsedBytes   *float64  `json:"mem_used_bytes"`
		MemTotalBytes  *float64  `json:"mem_total_bytes"`
		TempC          *float64  `json:"temp_c"`
	}
	points := []point{}

	if rng <= rawCutoff {
		rows, err := s.Pool.Query(ctx, `SELECT time, utilization_pct, mem_used_bytes::float8, mem_total_bytes::float8, temp_c FROM metrics_gpu WHERE host_id = $1 AND gpu_index = $2 AND time >= $3 ORDER BY time`, hostID, gpuIndex, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UtilizationPct, &p.MemUsedBytes, &p.MemTotalBytes, &p.TempC) == nil {
				points = append(points, p)
			}
		}
	} else {
		rows, err := s.Pool.Query(ctx, `SELECT bucket, avg_utilization_pct, avg_mem_used_bytes, avg_mem_total_bytes, avg_temp_c FROM metrics_gpu_1m WHERE host_id = $1 AND gpu_index = $2 AND bucket >= $3 ORDER BY bucket`, hostID, gpuIndex, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p point
			if rows.Scan(&p.Time, &p.UtilizationPct, &p.MemUsedBytes, &p.MemTotalBytes, &p.TempC) == nil {
				points = append(points, p)
			}
		}
	}
	writeJSON(w, http.StatusOK, points)
}
