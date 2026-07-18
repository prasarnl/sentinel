package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"

	"sentinel/server/internal/apikey"
	"sentinel/server/internal/llmbench"
	"sentinel/server/internal/models"
)

// pgxQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so helpers like
// loadConfig can run inside or outside a transaction.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ---------------------------------------------------------------------------
// Targets
// ---------------------------------------------------------------------------

type createLLMTargetRequest struct {
	Name              string `json:"name"`
	BaseURL           string `json:"base_url"`
	APIKey            string `json:"api_key"`
	SupportsModelSwap bool   `json:"supports_model_swap"`
}

func (s *Server) CreateLLMTarget(w http.ResponseWriter, r *http.Request) {
	var req createLLMTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.Name == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "name and base_url are required")
		return
	}

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	var apiKey *string
	if req.APIKey != "" {
		apiKey = &req.APIKey
	}

	var t models.LLMTarget
	err = tx.QueryRow(ctx,
		`INSERT INTO llm_targets (name, base_url, api_key, supports_model_swap)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, base_url, api_key, supports_model_swap, created_at`,
		req.Name, req.BaseURL, apiKey, req.SupportsModelSwap,
	).Scan(&t.ID, &t.Name, &t.BaseURL, &t.APIKey, &t.SupportsModelSwap, &t.CreatedAt)
	if err != nil {
		writeError(w, http.StatusConflict, "a target with that name already exists")
		return
	}
	t.HasAPIKey = t.APIKey != nil && *t.APIKey != ""

	cfg := models.DefaultLLMBenchmarkConfig(t.ID)
	_, err = tx.Exec(ctx, `
		INSERT INTO llm_benchmark_configs (target_id, concurrency, num_requests, warmup_requests, prompt_tokens, max_tokens, request_timeout_secs, model_load_timeout_secs)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		cfg.TargetID, cfg.Concurrency, cfg.NumRequests, cfg.WarmupRequests, cfg.PromptTokens, cfg.MaxTokens, cfg.RequestTimeoutSecs, cfg.ModelLoadTimeoutSecs,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save default benchmark config")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"target": t, "config": cfg})
}

func (s *Server) ListLLMTargets(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(),
		`SELECT id, name, base_url, api_key, supports_model_swap, created_at FROM llm_targets ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list targets")
		return
	}
	defer rows.Close()

	targets := []models.LLMTarget{}
	for rows.Next() {
		var t models.LLMTarget
		if err := rows.Scan(&t.ID, &t.Name, &t.BaseURL, &t.APIKey, &t.SupportsModelSwap, &t.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan target")
			return
		}
		t.HasAPIKey = t.APIKey != nil && *t.APIKey != ""
		targets = append(targets, t)
	}
	writeJSON(w, http.StatusOK, targets)
}

func (s *Server) GetLLMTarget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	t, err := loadTarget(ctx, s.Pool, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}

	cfg, err := loadConfig(ctx, s.Pool, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load benchmark config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"target": t, "config": cfg})
}

// loadTarget fetches a target by ID, used both by handlers that return it
// directly and ones (like RunBenchmark) that need its connection info.
func loadTarget(ctx context.Context, pool pgxQuerier, id string) (models.LLMTarget, error) {
	var t models.LLMTarget
	err := pool.QueryRow(ctx,
		`SELECT id, name, base_url, api_key, supports_model_swap, created_at FROM llm_targets WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.BaseURL, &t.APIKey, &t.SupportsModelSwap, &t.CreatedAt)
	t.HasAPIKey = t.APIKey != nil && *t.APIKey != ""
	return t, err
}

type updateLLMTargetRequest struct {
	Name              *string `json:"name"`
	BaseURL           *string `json:"base_url"`
	APIKey            *string `json:"api_key"` // empty string clears the key; omitted leaves it unchanged
	SupportsModelSwap *bool   `json:"supports_model_swap"`
}

func (s *Server) UpdateLLMTarget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateLLMTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	var t models.LLMTarget
	err := s.Pool.QueryRow(ctx, `
		UPDATE llm_targets SET
			name                = COALESCE($2, name),
			base_url            = COALESCE($3, base_url),
			api_key             = CASE WHEN $4::boolean THEN $5 ELSE api_key END,
			supports_model_swap = COALESCE($6, supports_model_swap)
		WHERE id = $1
		RETURNING id, name, base_url, api_key, supports_model_swap, created_at`,
		id, req.Name, req.BaseURL, req.APIKey != nil, req.APIKey, req.SupportsModelSwap,
	).Scan(&t.ID, &t.Name, &t.BaseURL, &t.APIKey, &t.SupportsModelSwap, &t.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	t.HasAPIKey = t.APIKey != nil && *t.APIKey != ""

	writeJSON(w, http.StatusOK, t)
}

func (s *Server) DeleteLLMTarget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tag, err := s.Pool.Exec(r.Context(), `DELETE FROM llm_targets WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type discoverModelsRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

// discoverModelsTimeout bounds the outbound call to the target so an
// unreachable/slow host doesn't hang the "Fetch models" UI action.
const discoverModelsTimeout = 10 * time.Second

// DiscoverModels queries base_url/v1/models on behalf of the browser, since
// the target may only be reachable from the server's network (not the
// user's browser) and this keeps the flow consistent with how benchmarks
// themselves are run — the server talks to targets directly. Used from the
// "Add target" dialog before a target row exists yet, so it takes
// base_url/api_key directly rather than a target ID.
func (s *Server) DiscoverModels(w http.ResponseWriter, r *http.Request) {
	var req discoverModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url is required")
		return
	}

	modelList, err := fetchModelList(r.Context(), req.BaseURL, req.APIKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"models": modelList})
}

// GetTargetModels discovers available models using an already-saved
// target's own connection info, for populating the run-time model picker.
func (s *Server) GetTargetModels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := loadTarget(r.Context(), s.Pool, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}

	apiKey := ""
	if t.APIKey != nil {
		apiKey = *t.APIKey
	}
	modelList, err := fetchModelList(r.Context(), t.BaseURL, apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"models": modelList})
}

// fetchModelList bounds and performs the outbound call to a target's model
// listing endpoint, shared by the ad-hoc (pre-save) and target-scoped
// discovery handlers.
func fetchModelList(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, discoverModelsTimeout)
	defer cancel()
	httpClient := &http.Client{Timeout: discoverModelsTimeout}
	return llmbench.ListModels(ctx, httpClient, baseURL, apiKey)
}

// ---------------------------------------------------------------------------
// Benchmark config
// ---------------------------------------------------------------------------

func loadConfig(ctx context.Context, pool pgxQuerier, targetID string) (models.LLMBenchmarkConfig, error) {
	var cfg models.LLMBenchmarkConfig
	err := pool.QueryRow(ctx, `
		SELECT target_id, concurrency, num_requests, warmup_requests, prompt_tokens, max_tokens, request_timeout_secs, model_load_timeout_secs, context_window, batch_size, updated_at
		FROM llm_benchmark_configs WHERE target_id = $1`, targetID,
	).Scan(&cfg.TargetID, &cfg.Concurrency, &cfg.NumRequests, &cfg.WarmupRequests, &cfg.PromptTokens, &cfg.MaxTokens, &cfg.RequestTimeoutSecs, &cfg.ModelLoadTimeoutSecs, &cfg.ContextWindow, &cfg.BatchSize, &cfg.UpdatedAt)
	return cfg, err
}

type updateLLMBenchmarkConfigRequest struct {
	Concurrency          int  `json:"concurrency"`
	NumRequests          int  `json:"num_requests"`
	WarmupRequests       int  `json:"warmup_requests"`
	PromptTokens         int  `json:"prompt_tokens"`
	MaxTokens            int  `json:"max_tokens"`
	RequestTimeoutSecs   int  `json:"request_timeout_secs"`
	ModelLoadTimeoutSecs int  `json:"model_load_timeout_secs"`
	ContextWindow        *int `json:"context_window"`
	BatchSize            *int `json:"batch_size"`
}

func (s *Server) UpdateLLMBenchmarkConfig(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	var req updateLLMBenchmarkConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Concurrency < 1 || req.NumRequests < 1 || req.WarmupRequests < 0 ||
		req.PromptTokens < 1 || req.MaxTokens < 1 || req.RequestTimeoutSecs < 1 || req.ModelLoadTimeoutSecs < 1 {
		writeError(w, http.StatusBadRequest, "all benchmark parameters must be positive (warmup_requests may be 0)")
		return
	}
	if (req.ContextWindow != nil && *req.ContextWindow < 1) || (req.BatchSize != nil && *req.BatchSize < 1) {
		writeError(w, http.StatusBadRequest, "context_window and batch_size must be positive when set")
		return
	}

	ctx := r.Context()
	var cfg models.LLMBenchmarkConfig
	err := s.Pool.QueryRow(ctx, `
		UPDATE llm_benchmark_configs SET
			concurrency = $2, num_requests = $3, warmup_requests = $4,
			prompt_tokens = $5, max_tokens = $6, request_timeout_secs = $7,
			model_load_timeout_secs = $8, context_window = $9, batch_size = $10, updated_at = now()
		WHERE target_id = $1
		RETURNING target_id, concurrency, num_requests, warmup_requests, prompt_tokens, max_tokens, request_timeout_secs, model_load_timeout_secs, context_window, batch_size, updated_at`,
		targetID, req.Concurrency, req.NumRequests, req.WarmupRequests, req.PromptTokens, req.MaxTokens, req.RequestTimeoutSecs, req.ModelLoadTimeoutSecs, req.ContextWindow, req.BatchSize,
	).Scan(&cfg.TargetID, &cfg.Concurrency, &cfg.NumRequests, &cfg.WarmupRequests, &cfg.PromptTokens, &cfg.MaxTokens, &cfg.RequestTimeoutSecs, &cfg.ModelLoadTimeoutSecs, &cfg.ContextWindow, &cfg.BatchSize, &cfg.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

// ---------------------------------------------------------------------------
// Benchmark runs
// ---------------------------------------------------------------------------

type runBenchmarkRequest struct {
	Models []string `json:"models"`
}

// unloadCallTimeout bounds each unload API call — these are simple control
// requests to the proxy itself, not inference, so they should be quick.
const unloadCallTimeout = 30 * time.Second

func (s *Server) RunBenchmark(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	ctx := r.Context()

	var req runBenchmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	selectedModels := make([]string, 0, len(req.Models))
	for _, m := range req.Models {
		if m = strings.TrimSpace(m); m != "" {
			selectedModels = append(selectedModels, m)
		}
	}
	if len(selectedModels) == 0 {
		writeError(w, http.StatusBadRequest, "at least one model is required")
		return
	}

	t, err := loadTarget(ctx, s.Pool, targetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	if !t.SupportsModelSwap && len(selectedModels) > 1 {
		writeError(w, http.StatusBadRequest, "this target does not support testing multiple models in one run")
		return
	}

	cfg, err := loadConfig(ctx, s.Pool, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load benchmark config")
		return
	}

	batchID, err := apikey.Generate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start benchmark batch")
		return
	}

	apiKey := ""
	if t.APIKey != nil {
		apiKey = *t.APIKey
	}
	target := llmbench.Target{BaseURL: t.BaseURL, APIKey: apiKey}

	go s.executeBenchmarkBatch(target, t.SupportsModelSwap, targetID, selectedModels, cfg, batchID)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"batch_id":  batchID,
		"target_id": targetID,
		"models":    selectedModels,
	})
}

// executeBenchmarkBatch runs in the background (detached from the
// triggering HTTP request), sequencing one model at a time so VRAM never
// holds more than one loaded model when the target supports it: unload
// everything, then for each model load-and-benchmark-and-unload it before
// moving to the next, then a final unload-all as a safety net. Progress is
// published to the websocket hub keyed by the batch token, and each
// model's result is persisted regardless of whether anyone is listening.
func (s *Server) executeBenchmarkBatch(target llmbench.Target, supportsSwap bool, targetID string, modelList []string, cfg models.LLMBenchmarkConfig, batchID string) {
	ctx := context.Background()
	unloadClient := &http.Client{Timeout: unloadCallTimeout}

	publish := func(evt models.BenchmarkProgressEvent) {
		evt.BatchID = batchID
		evt.ModelsTotal = len(modelList)
		payload, _ := json.Marshal(evt)
		s.Hub.Publish(batchID, payload)
	}

	if supportsSwap {
		publish(models.BenchmarkProgressEvent{Stage: models.StageUnloading})
		if err := llmbench.UnloadAllModels(ctx, unloadClient, target.BaseURL, target.APIKey); err != nil {
			log.Printf("batch %s: initial unload-all failed: %v", batchID, err)
		}
	}

	for i, model := range modelList {
		publish(models.BenchmarkProgressEvent{Stage: models.StageLoading, Model: model, ModelIndex: i + 1})

		cfgJSON, _ := json.Marshal(cfg)
		run := models.LLMBenchmarkRun{TargetID: targetID, Model: model, BatchID: &batchID, Config: cfg, Status: models.BenchmarkRunning}
		if err := s.Pool.QueryRow(ctx,
			`INSERT INTO llm_benchmark_runs (target_id, model, batch_id, config, status) VALUES ($1, $2, $3, $4, $5) RETURNING id, started_at`,
			run.TargetID, run.Model, run.BatchID, cfgJSON, run.Status,
		).Scan(&run.ID, &run.StartedAt); err != nil {
			log.Printf("batch %s: failed to create run row for model %s: %v", batchID, model, err)
			continue
		}

		summary, runErr := llmbench.Run(ctx, target, model, cfg, func(evt llmbench.ProgressEvent) {
			publish(models.BenchmarkProgressEvent{
				Stage: models.StageBenchmarking, Model: model, ModelIndex: i + 1,
				Completed: evt.Completed, Total: evt.Total, Failed: evt.Failed,
				LastTTFTMs: evt.LastTTFTMs, LastTokensPerSec: evt.LastTokensPerSec, LastError: evt.LastError,
			})
		})

		completedAt := time.Now()
		if runErr != nil {
			errMsg := runErr.Error()
			run.Status = models.BenchmarkFailed
			run.Error = &errMsg
			run.CompletedAt = &completedAt
			if _, dbErr := s.Pool.Exec(ctx,
				`UPDATE llm_benchmark_runs SET status = $2, error = $3, completed_at = $4 WHERE id = $1`,
				run.ID, run.Status, run.Error, completedAt,
			); dbErr != nil {
				log.Printf("batch %s: failed to persist failure for %s: %v", batchID, model, dbErr)
			}
		} else {
			run.Status = models.BenchmarkCompleted
			run.Summary = &summary
			run.CompletedAt = &completedAt
			summaryJSON, _ := json.Marshal(summary)
			if _, dbErr := s.Pool.Exec(ctx,
				`UPDATE llm_benchmark_runs SET status = $2, summary = $3, completed_at = $4 WHERE id = $1`,
				run.ID, run.Status, summaryJSON, completedAt,
			); dbErr != nil {
				log.Printf("batch %s: failed to persist summary for %s: %v", batchID, model, dbErr)
			}
		}

		publish(models.BenchmarkProgressEvent{Stage: models.StageModelDone, Model: model, ModelIndex: i + 1, Run: &run})

		if supportsSwap {
			if err := llmbench.UnloadModel(ctx, unloadClient, target.BaseURL, target.APIKey, model); err != nil && !errors.Is(err, llmbench.ErrModelNotRunning) {
				log.Printf("batch %s: failed to unload %s: %v", batchID, model, err)
			}
		}
	}

	if supportsSwap {
		if err := llmbench.UnloadAllModels(ctx, unloadClient, target.BaseURL, target.APIKey); err != nil {
			log.Printf("batch %s: final unload-all failed: %v", batchID, err)
		}
	}

	publish(models.BenchmarkProgressEvent{Stage: models.StageDone, Done: true})
}

func (s *Server) ListBenchmarkRuns(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, target_id, model, batch_id, config, status, summary, error, started_at, completed_at
		FROM llm_benchmark_runs WHERE target_id = $1 ORDER BY started_at DESC LIMIT 50`, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list benchmark runs")
		return
	}
	defer rows.Close()

	runs := []models.LLMBenchmarkRun{}
	for rows.Next() {
		run, err := scanBenchmarkRun(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan benchmark run")
			return
		}
		runs = append(runs, run)
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) GetBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	row := s.Pool.QueryRow(r.Context(), `
		SELECT id, target_id, model, batch_id, config, status, summary, error, started_at, completed_at
		FROM llm_benchmark_runs WHERE id = $1`, runID)
	run, err := scanBenchmarkRun(row)
	if err != nil {
		writeError(w, http.StatusNotFound, "benchmark run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) DeleteBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	runID := chi.URLParam(r, "runId")
	tag, err := s.Pool.Exec(r.Context(),
		`DELETE FROM llm_benchmark_runs WHERE id = $1 AND target_id = $2`, runID, targetID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "benchmark run not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pgxRowScanner covers both pgx.Row (QueryRow) and pgx.Rows (Query, via its
// embedded Scan) so scanBenchmarkRun can be shared by both handlers.
type pgxRowScanner interface {
	Scan(dest ...any) error
}

func scanBenchmarkRun(row pgxRowScanner) (models.LLMBenchmarkRun, error) {
	var (
		run         models.LLMBenchmarkRun
		configJSON  []byte
		summaryJSON []byte
	)
	if err := row.Scan(&run.ID, &run.TargetID, &run.Model, &run.BatchID, &configJSON, &run.Status, &summaryJSON, &run.Error, &run.StartedAt, &run.CompletedAt); err != nil {
		return run, err
	}
	if len(configJSON) > 0 {
		_ = json.Unmarshal(configJSON, &run.Config)
	}
	if len(summaryJSON) > 0 {
		var summary models.BenchmarkSummary
		if err := json.Unmarshal(summaryJSON, &summary); err == nil {
			run.Summary = &summary
		}
	}
	return run, nil
}

// StreamBenchmarkBatch streams live progress for a running (possibly
// multi-model) benchmark batch, keyed by the opaque batch token returned
// from RunBenchmark rather than a single DB run ID.
func (s *Server) StreamBenchmarkBatch(w http.ResponseWriter, r *http.Request) {
	batchID := chi.URLParam(r, "batchId")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ch := s.Hub.Subscribe(batchID)
	defer s.Hub.Unsubscribe(batchID, ch)

	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				conn.Close()
				return
			}
		}
	}()

	for msg := range ch {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
