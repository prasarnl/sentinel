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

	writeJSON(w, http.StatusOK, result)
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
