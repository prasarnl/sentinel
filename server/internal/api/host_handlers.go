package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"sentinel/server/internal/apikey"
	"sentinel/server/internal/models"
)

type createHostRequest struct {
	Name string        `json:"name"`
	OS   models.HostOS `json:"os"`
	Tags []string      `json:"tags"`
}

func (s *Server) CreateHost(w http.ResponseWriter, r *http.Request) {
	var req createHostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || (req.OS != models.OSLinux && req.OS != models.OSWindows) {
		writeError(w, http.StatusBadRequest, "name and a valid os (linux|windows) are required")
		return
	}

	token, err := apikey.Generate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate enrollment token")
		return
	}
	expires := time.Now().Add(24 * time.Hour)

	var h models.Host
	err = s.Pool.QueryRow(r.Context(),
		`INSERT INTO hosts (name, os, tags, enrollment_token, enrollment_expires, status)
		 VALUES ($1, $2, $3, $4, $5, 'pending')
		 RETURNING id, name, os, tags, status, created_at, enrollment_token, enrollment_expires`,
		req.Name, req.OS, req.Tags, token, expires,
	).Scan(&h.ID, &h.Name, &h.OS, &h.Tags, &h.Status, &h.CreatedAt, &h.EnrollmentToken, &h.EnrollmentExpires)
	if err != nil {
		writeError(w, http.StatusConflict, "a host with that name already exists")
		return
	}

	baseURL := requestBaseURL(r)
	installCmd := installCommand(h.OS, baseURL, token)
	writeJSON(w, http.StatusCreated, map[string]any{
		"host":            h,
		"install_command": installCmd,
	})
}

// requestBaseURL reconstructs the scheme+host the request came in on, so
// generated install commands are directly copy-pasteable rather than
// containing a placeholder.
func requestBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func installCommand(os models.HostOS, baseURL, token string) string {
	switch os {
	case models.OSWindows:
		return fmt.Sprintf(`irm %s/install.ps1 | iex; Install-SentinelAgent -ServerUrl "%s" -EnrollmentToken "%s"`, baseURL, baseURL, token)
	default:
		return fmt.Sprintf(`curl -fsSL %s/install.sh | sudo bash -s -- --server %s --token %s`, baseURL, baseURL, token)
	}
}

func (s *Server) ListHosts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(),
		`SELECT id, name, os, tags, status, last_seen, created_at FROM hosts WHERE status != 'removed' ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list hosts")
		return
	}
	defer rows.Close()

	hosts := []models.Host{}
	for rows.Next() {
		var h models.Host
		if err := rows.Scan(&h.ID, &h.Name, &h.OS, &h.Tags, &h.Status, &h.LastSeen, &h.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan host")
			return
		}
		hosts = append(hosts, h)
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (s *Server) DeleteHost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	purge := r.URL.Query().Get("purge") == "true"

	tag, err := s.Pool.Exec(r.Context(),
		`UPDATE hosts SET status = 'removed', api_key_hash = NULL, enrollment_token = NULL WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}

	if purge {
		for _, table := range []string{"metrics_cpu", "metrics_mem", "metrics_disk", "metrics_gpu"} {
			if _, err := s.Pool.Exec(r.Context(), fmt.Sprintf(`DELETE FROM %s WHERE host_id = $1`, table), id); err != nil {
				writeError(w, http.StatusInternalServerError, "host removed but failed to purge historical data")
				return
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
