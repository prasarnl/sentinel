-- LLM runtime telemetry: KV cache, token throughput, prefix-cache
-- effectiveness, and queue depth scraped by the agent from an inference
-- server's Prometheus endpoint (llama.cpp / llama-swap, vLLM) on GPU hosts.
--
-- These metrics are not obtainable from the GPU driver: nvidia-smi knows how
-- many VRAM bytes are allocated but nothing about what they hold. They come
-- from the runtime itself and are normalized by the agent into one shape, so
-- columns a given runtime cannot report stay NULL (llama.cpp exposes no
-- prefix-cache or preemption counters, for instance). NULL means "not
-- measurable here", which the UI renders differently from a real zero.
--
-- Distinct from llm_targets/llm_benchmark_runs: those drive synthetic load
-- from the server on demand, this is passive telemetry from the host.

CREATE TABLE metrics_llm (
    time                       TIMESTAMPTZ NOT NULL,
    host_id                    UUID NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    endpoint                   TEXT NOT NULL,          -- e.g. http://127.0.0.1:8080
    runtime                    TEXT NOT NULL CHECK (runtime IN ('llamacpp', 'vllm')),
    model                      TEXT,                   -- best-effort; can change under llama-swap

    kv_cache_usage_ratio       DOUBLE PRECISION,       -- 0..1
    kv_cache_tokens            BIGINT,

    prompt_tokens_total        BIGINT,                 -- cumulative, as reported
    generated_tokens_total     BIGINT,
    prompt_tokens_per_sec      DOUBLE PRECISION,       -- derived by the agent across scrapes
    generated_tokens_per_sec   DOUBLE PRECISION,

    prefix_cache_queries_total BIGINT,
    prefix_cache_hits_total    BIGINT,
    prefix_cache_hit_ratio     DOUBLE PRECISION,       -- 0..1, windowed rather than since-boot

    requests_running           INT,
    requests_waiting           INT,

    ttft_ms_avg                DOUBLE PRECISION,
    tpot_ms_avg                DOUBLE PRECISION,

    preemptions_per_sec        DOUBLE PRECISION        -- KV cache oversubscribed when this rises
);
SELECT create_hypertable('metrics_llm', 'time');
CREATE INDEX idx_metrics_llm_host_time ON metrics_llm (host_id, endpoint, time DESC);

CREATE MATERIALIZED VIEW metrics_llm_1m
WITH (timescaledb.continuous) AS
SELECT host_id,
       endpoint,
       time_bucket('1 minute', time) AS bucket,
       avg(kv_cache_usage_ratio) AS avg_kv_cache_usage_ratio,
       avg(prompt_tokens_per_sec) AS avg_prompt_tokens_per_sec,
       avg(generated_tokens_per_sec) AS avg_generated_tokens_per_sec,
       avg(prefix_cache_hit_ratio) AS avg_prefix_cache_hit_ratio,
       avg(requests_running) AS avg_requests_running,
       avg(requests_waiting) AS avg_requests_waiting,
       avg(ttft_ms_avg) AS avg_ttft_ms,
       avg(tpot_ms_avg) AS avg_tpot_ms,
       avg(preemptions_per_sec) AS avg_preemptions_per_sec
FROM metrics_llm
GROUP BY host_id, endpoint, bucket
WITH NO DATA;

SELECT add_continuous_aggregate_policy('metrics_llm_1m', start_offset => INTERVAL '3 hours', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '1 minute');

-- Matches the default applied to the other metric tables in 001_init.sql; the
-- server rewrites this whenever settings.retention_days changes.
SELECT add_retention_policy('metrics_llm', INTERVAL '90 days');
