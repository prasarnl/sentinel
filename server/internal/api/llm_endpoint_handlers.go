package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sentinel/llmscrape"
	"sentinel/server/internal/models"
)

// endpointColumns is shared by every read path so the SELECT list and
// scanLLMEndpoint cannot drift apart. The model name is the most recent one
// observed for the endpoint, which is display-only.
const endpointColumns = `
	e.id, e.host_id, h.name, e.name, e.url, e.runtime, e.api_key, e.enabled, e.source,
	e.last_scrape_at, e.last_scrape_error, e.created_at,
	(SELECT m.model FROM metrics_llm m WHERE m.endpoint_id = e.id AND m.model IS NOT NULL ORDER BY m.time DESC LIMIT 1)`

func scanLLMEndpoint(row pgx.Row) (models.LLMEndpoint, error) {
	var e models.LLMEndpoint
	err := row.Scan(&e.ID, &e.HostID, &e.HostName, &e.Name, &e.URL, &e.Runtime, &e.APIKey,
		&e.Enabled, &e.Source, &e.LastScrapeAt, &e.LastScrapeError, &e.CreatedAt, &e.Model)
	e.HasAPIKey = e.APIKey != nil && *e.APIKey != ""
	e.BenchmarkURL = benchmarkURLFor(e.URL, e.HostName)
	return e, err
}

// annotateBenchmarkTargets marks endpoints whose server-reachable address is
// already a benchmark target, so the UI links to it rather than offering to
// create a second one for the same server.
func (s *Server) annotateBenchmarkTargets(ctx context.Context, endpoints []models.LLMEndpoint) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, base_url FROM llm_targets`)
	if err != nil {
		return
	}
	defer rows.Close()

	byURL := map[string][2]string{}
	for rows.Next() {
		var id, name, baseURL string
		if rows.Scan(&id, &name, &baseURL) == nil {
			byURL[strings.TrimSuffix(baseURL, "/")] = [2]string{id, name}
		}
	}
	for i := range endpoints {
		if hit, ok := byURL[strings.TrimSuffix(endpoints[i].BenchmarkURL, "/")]; ok {
			id, name := hit[0], hit[1]
			endpoints[i].BenchmarkTargetID = &id
			endpoints[i].BenchmarkTargetName = &name
		}
	}
}

func (s *Server) ListLLMEndpoints(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT `+endpointColumns+`
		FROM llm_endpoints e LEFT JOIN hosts h ON h.id = e.host_id
		-- Remote endpoints last: they are the exception and read better
		-- grouped at the end of the list.
		ORDER BY (e.host_id IS NULL), h.name NULLS FIRST, e.url`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list endpoints")
		return
	}
	defer rows.Close()

	endpoints := []models.LLMEndpoint{}
	for rows.Next() {
		e, err := scanLLMEndpoint(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan endpoint")
			return
		}
		endpoints = append(endpoints, e)
	}
	s.annotateBenchmarkTargets(r.Context(), endpoints)
	writeJSON(w, http.StatusOK, endpoints)
}

type createLLMEndpointRequest struct {
	HostID  *string `json:"host_id"`
	Name    string  `json:"name"`
	URL     string  `json:"url"`
	Runtime string  `json:"runtime"`
	APIKey  string  `json:"api_key"`
}

func (s *Server) CreateLLMEndpoint(w http.ResponseWriter, r *http.Request) {
	var req createLLMEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.URL = strings.TrimSuffix(strings.TrimSpace(req.URL), "/")
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		writeError(w, http.StatusBadRequest, "url must start with http:// or https://")
		return
	}
	if req.Runtime == "" {
		req.Runtime = llmscrape.RuntimeAuto
	}
	if req.Runtime != llmscrape.RuntimeAuto && req.Runtime != llmscrape.RuntimeVLLM && req.Runtime != llmscrape.RuntimeLlamaCpp {
		writeError(w, http.StatusBadRequest, "runtime must be auto, vllm, or llamacpp")
		return
	}

	row := s.Pool.QueryRow(r.Context(), `
		WITH inserted AS (
			INSERT INTO llm_endpoints (host_id, name, url, runtime, api_key, source)
			VALUES ($1, NULLIF($2, ''), $3, $4, NULLIF($5, ''), 'manual')
			RETURNING *
		)
		SELECT `+endpointColumns+`
		FROM inserted e LEFT JOIN hosts h ON h.id = e.host_id`,
		req.HostID, req.Name, req.URL, req.Runtime, req.APIKey)

	e, err := scanLLMEndpoint(row)
	if err != nil {
		writeError(w, http.StatusConflict, "that endpoint is already registered for this host")
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

type updateLLMEndpointRequest struct {
	Name    *string `json:"name"`
	Runtime *string `json:"runtime"`
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
}

func (s *Server) UpdateLLMEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateLLMEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// COALESCE leaves omitted fields untouched, so a PATCH that only toggles
	// `enabled` cannot blank out a stored API key.
	row := s.Pool.QueryRow(r.Context(), `
		WITH updated AS (
			UPDATE llm_endpoints SET
				name    = COALESCE(NULLIF($2, ''), name),
				runtime = COALESCE($3, runtime),
				api_key = COALESCE(NULLIF($4, ''), api_key),
				enabled = COALESCE($5, enabled)
			WHERE id = $1
			RETURNING *
		)
		SELECT `+endpointColumns+`
		FROM updated e LEFT JOIN hosts h ON h.id = e.host_id`,
		id, req.Name, req.Runtime, req.APIKey, req.Enabled)

	e, err := scanLLMEndpoint(row)
	if err != nil {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// DeleteLLMEndpoint removes a registration. Its metrics cascade away with it.
//
// An autodetected endpoint that is still running will be rediscovered and
// reappear within a discovery interval — deleting it is not how you stop
// monitoring something. Disabling is, which is why the UI offers that first.
func (s *Server) DeleteLLMEndpoint(w http.ResponseWriter, r *http.Request) {
	tag, err := s.Pool.Exec(r.Context(), `DELETE FROM llm_endpoints WHERE id = $1`, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete endpoint")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// registerDiscoveredEndpoints records what an agent's socket discovery found.
// Already-known endpoints are left alone so a rediscovery never resurrects
// one an operator disabled, nor overwrites a name they set.
func registerDiscoveredEndpoints(ctx context.Context, pool *pgxpool.Pool, hostID string, found []models.DiscoveredLLMEndpoint) error {
	if len(found) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, ep := range found {
		url := strings.TrimSuffix(strings.TrimSpace(ep.URL), "/")
		if url == "" {
			continue
		}
		batch.Queue(`
			INSERT INTO llm_endpoints (host_id, url, runtime, source)
			VALUES ($1, $2, $3, 'autodetected')
			ON CONFLICT DO NOTHING`,
			hostID, url, ep.Runtime)
	}
	if batch.Len() == 0 {
		return nil
	}
	return pool.SendBatch(ctx, batch).Close()
}

// agentEndpointsFor returns the endpoints a host's agent should scrape:
// everything enabled and registered against that host. Disabled entries are
// omitted entirely rather than sent with a flag, so an agent that doesn't
// understand the field can't keep scraping something switched off.
func agentEndpointsFor(ctx context.Context, pool *pgxpool.Pool, hostID string) ([]models.AgentLLMEndpoint, error) {
	rows, err := pool.Query(ctx, `
		SELECT url, runtime, COALESCE(api_key, '')
		FROM llm_endpoints
		WHERE host_id = $1 AND enabled = true
		ORDER BY url`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	endpoints := []models.AgentLLMEndpoint{}
	for rows.Next() {
		var e models.AgentLLMEndpoint
		if err := rows.Scan(&e.URL, &e.Runtime, &e.APIKey); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, e)
	}
	return endpoints, rows.Err()
}

// disabledEndpointsFor lists URLs an agent must not scrape, so a disabled
// endpoint stops being reported without waiting for the agent to restart.
func disabledEndpointsFor(ctx context.Context, pool *pgxpool.Pool, hostID string) (map[string]bool, error) {
	rows, err := pool.Query(ctx,
		`SELECT url FROM llm_endpoints WHERE host_id = $1 AND enabled = false`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	disabled := map[string]bool{}
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		disabled[url] = true
	}
	return disabled, rows.Err()
}
