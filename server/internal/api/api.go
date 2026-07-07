package api

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"sentinel/server/internal/auth"
	"sentinel/server/internal/ws"
)

type Server struct {
	Pool         *pgxpool.Pool
	Auth         *auth.Service
	Hub          *ws.Hub
	DownloadsDir string
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
