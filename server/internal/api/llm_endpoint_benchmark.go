package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"sentinel/server/internal/models"
)

// benchmarkURLFor suggests how the *server* can reach an endpoint, which is
// often not the URL the endpoint is monitored at.
//
// An agent scrapes its own host over loopback, so a monitored endpoint is
// typically http://127.0.0.1:8035 — an address that means the server itself
// if used from here. The same runtime is usually reachable at the host's own
// name on the same port, so that is offered as a starting point. It is a
// suggestion, not a fact: the port may be firewalled or the name may not
// resolve from the server, which is why the caller can edit it and probe
// before committing.
func benchmarkURLFor(endpointURL string, hostName *string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(endpointURL), "/")

	// Remote endpoints are already addressed from the server's perspective.
	if hostName == nil || strings.TrimSpace(*hostName) == "" {
		return trimmed
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	if !isLoopbackHost(u.Hostname()) {
		return trimmed
	}

	host := strings.TrimSpace(*hostName)
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	u.Host = host
	return u.String()
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1", "0.0.0.0", "":
		return true
	default:
		return false
	}
}

type benchmarkProbeRequest struct {
	BaseURL string `json:"base_url"`
}

// ProbeEndpointBenchmarkURL lists the models reachable at a candidate URL,
// using the endpoint's stored API key.
//
// The key never leaves the server — the endpoint API exposes only
// has_api_key — so the browser cannot run this probe itself.
func (s *Server) ProbeEndpointBenchmarkURL(w http.ResponseWriter, r *http.Request) {
	var req benchmarkProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.BaseURL = strings.TrimSuffix(strings.TrimSpace(req.BaseURL), "/")
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url is required")
		return
	}

	apiKey, err := endpointAPIKey(r, s, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	}

	modelList, err := fetchModelList(r.Context(), req.BaseURL, apiKey)
	if err != nil {
		// A failed probe is information, not an error condition: the caller
		// is deciding whether this URL is right, and the reason it failed is
		// the useful part.
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reachable": true, "models": modelList})
}

type createTargetFromEndpointRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

// CreateBenchmarkTargetFromEndpoint registers a monitored endpoint as a
// benchmark target, so an endpoint never has to be typed in twice.
//
// This runs server-side specifically so the endpoint's API key can be copied
// across without ever being sent to the browser.
func (s *Server) CreateBenchmarkTargetFromEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req createTargetFromEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSuffix(strings.TrimSpace(req.BaseURL), "/")
	if req.Name == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "name and base_url are required")
		return
	}

	apiKey, err := endpointAPIKey(r, s, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	}

	ctx := r.Context()

	// The same server may already be registered under this address — their
	// DGX target predates the endpoint registry, for instance. Point at the
	// existing one rather than failing on a unique-name collision.
	var existingID, existingName string
	if err := s.Pool.QueryRow(ctx,
		`SELECT id, name FROM llm_targets WHERE base_url = $1`, req.BaseURL,
	).Scan(&existingID, &existingName); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"target_id": existingID, "name": existingName, "already_existed": true,
		})
		return
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	var t models.LLMTarget
	err = tx.QueryRow(ctx,
		`INSERT INTO llm_targets (name, base_url, api_key, supports_model_swap)
		 VALUES ($1, $2, $3, false)
		 RETURNING id, name, base_url, api_key, supports_model_swap, created_at`,
		req.Name, req.BaseURL, nullIfEmpty(apiKey),
	).Scan(&t.ID, &t.Name, &t.BaseURL, &t.APIKey, &t.SupportsModelSwap, &t.CreatedAt)
	if err != nil {
		writeError(w, http.StatusConflict, "a benchmark target with that name already exists")
		return
	}
	t.HasAPIKey = t.APIKey != nil && *t.APIKey != ""

	cfg := models.DefaultLLMBenchmarkConfig(t.ID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO llm_benchmark_configs (target_id, concurrency, num_requests, warmup_requests, prompt_tokens, max_tokens, request_timeout_secs, model_load_timeout_secs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		cfg.TargetID, cfg.Concurrency, cfg.NumRequests, cfg.WarmupRequests, cfg.PromptTokens,
		cfg.MaxTokens, cfg.RequestTimeoutSecs, cfg.ModelLoadTimeoutSecs,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save default benchmark config")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"target_id": t.ID, "name": t.Name, "already_existed": false,
	})
}

// endpointAPIKey reads an endpoint's stored key for server-side outbound use.
func endpointAPIKey(r *http.Request, s *Server, endpointID string) (string, error) {
	var apiKey *string
	if err := s.Pool.QueryRow(r.Context(),
		`SELECT api_key FROM llm_endpoints WHERE id = $1`, endpointID).Scan(&apiKey); err != nil {
		return "", err
	}
	if apiKey == nil {
		return "", nil
	}
	return *apiKey, nil
}
