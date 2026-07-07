package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	var retentionDays string
	err := s.Pool.QueryRow(r.Context(), `SELECT value FROM settings WHERE key = 'retention_days'`).Scan(&retentionDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"retention_days": retentionDays})
}

type updateSettingsRequest struct {
	RetentionDays int `json:"retention_days"`
}

var retentionTables = []string{"metrics_cpu", "metrics_mem", "metrics_disk", "metrics_gpu"}

func (s *Server) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RetentionDays < 30 || req.RetentionDays > 365 {
		writeError(w, http.StatusBadRequest, "retention_days must be between 30 and 365")
		return
	}

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE settings SET value = $1 WHERE key = 'retention_days'`, strconv.Itoa(req.RetentionDays)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}

	for _, table := range retentionTables {
		// remove_retention_policy is a no-op (with if_exists) when none is set yet.
		if _, err := tx.Exec(ctx, `SELECT remove_retention_policy($1, if_exists => true)`, table); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear existing retention policy for "+table)
			return
		}
		interval := strconv.Itoa(req.RetentionDays) + " days"
		if _, err := tx.Exec(ctx, `SELECT add_retention_policy($1, $2::interval)`, table, interval); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to apply retention policy for "+table)
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit settings update")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"retention_days": req.RetentionDays})
}
