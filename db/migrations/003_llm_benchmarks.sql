-- LLM inference benchmarking: configurable targets (OpenAI-compatible HTTP
-- endpoints) hit directly by the server, with saved benchmark parameters
-- and historical run results.

CREATE TABLE llm_targets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    base_url   TEXT NOT NULL,        -- e.g. http://10.0.0.5:8080
    model      TEXT NOT NULL,        -- model name sent in request body
    api_key    TEXT,                 -- optional bearer token, sent outbound so stored reversibly (plaintext)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE llm_benchmark_configs (
    target_id            UUID PRIMARY KEY REFERENCES llm_targets (id) ON DELETE CASCADE,
    concurrency          INT NOT NULL DEFAULT 1,
    num_requests         INT NOT NULL DEFAULT 10,
    warmup_requests      INT NOT NULL DEFAULT 1,
    prompt_tokens        INT NOT NULL DEFAULT 512,
    max_tokens           INT NOT NULL DEFAULT 128,
    request_timeout_secs INT NOT NULL DEFAULT 120,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE llm_benchmark_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id    UUID NOT NULL REFERENCES llm_targets (id) ON DELETE CASCADE,
    config       JSONB NOT NULL,     -- snapshot of params used for this run
    status       TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    summary      JSONB,              -- aggregate stats once completed
    error        TEXT,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX idx_llm_benchmark_runs_target_time ON llm_benchmark_runs (target_id, started_at DESC);
