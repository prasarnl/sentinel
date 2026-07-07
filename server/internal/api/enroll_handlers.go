package api

import (
	"encoding/json"
	"net/http"

	"sentinel/server/internal/apikey"
)

type enrollRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
}

// Enroll exchanges a one-time enrollment token for a permanent API key.
// It is unauthenticated (the token itself is the credential) and is only
// ever expected to be called once per host, right after an admin creates it.
func (s *Server) Enroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EnrollmentToken == "" {
		writeError(w, http.StatusBadRequest, "enrollment_token is required")
		return
	}

	key, err := apikey.Generate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate api key")
		return
	}
	keyHash := apikey.Hash(key)

	var hostID string
	err = s.Pool.QueryRow(r.Context(),
		`UPDATE hosts
		 SET api_key_hash = $1, enrollment_token = NULL, enrollment_expires = NULL, status = 'online', last_seen = now()
		 WHERE enrollment_token = $2 AND enrollment_expires > now()
		 RETURNING id`,
		keyHash, req.EnrollmentToken,
	).Scan(&hostID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired enrollment token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"host_id": hostID,
		"api_key": key,
	})
}

// hostFromAPIKey resolves the X-API-Key header to a host ID, used by the
// ingest endpoint. Returns ("", false) if the key is missing or invalid.
func (s *Server) hostFromAPIKey(r *http.Request) (string, bool) {
	key := r.Header.Get("X-API-Key")
	if key == "" {
		return "", false
	}
	var hostID string
	err := s.Pool.QueryRow(r.Context(),
		`SELECT id FROM hosts WHERE api_key_hash = $1 AND status != 'removed'`,
		apikey.Hash(key),
	).Scan(&hostID)
	if err != nil {
		return "", false
	}
	return hostID, true
}
