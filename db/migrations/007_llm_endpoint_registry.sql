-- A registry of inference endpoints, so they can be managed from the web UI
-- instead of by editing /etc/sentinel-agent/config.yaml on each host, and so
-- endpoints on machines with no agent can be scraped by the server itself.
--
-- Endpoints reach the registry two ways: agents report what their socket
-- discovery found (source='autodetected'), and operators add them by hand
-- (source='manual'). Either can be renamed or disabled; disabling is how the
-- embedding server gets hidden without stopping it.

CREATE TABLE llm_endpoints (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NULL host_id means no agent can reach it, so the server polls it
    -- directly over the network.
    host_id    UUID REFERENCES hosts (id) ON DELETE CASCADE,
    name       TEXT,                       -- optional friendly label; falls back to the URL
    url        TEXT NOT NULL,              -- base URL, e.g. http://127.0.0.1:8035
    runtime    TEXT NOT NULL DEFAULT 'auto' CHECK (runtime IN ('auto', 'vllm', 'llamacpp')),
    api_key    TEXT,                       -- optional bearer token, sent outbound so stored reversibly
    enabled    BOOLEAN NOT NULL DEFAULT true,
    source     TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'autodetected')),
    -- Together these distinguish "unreachable", "reachable but publishes no
    -- metrics" (LM Studio's shape), and "working" — three states that
    -- otherwise all render as an endpoint with no data.
    last_scrape_at    TIMESTAMPTZ,
    last_scrape_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A plain UNIQUE (host_id, url) would not constrain remote endpoints, since
-- NULLs compare as distinct and every remote row has host_id NULL. Two
-- partial indexes cover both cases.
CREATE UNIQUE INDEX idx_llm_endpoints_host_url ON llm_endpoints (host_id, url) WHERE host_id IS NOT NULL;
CREATE UNIQUE INDEX idx_llm_endpoints_remote_url ON llm_endpoints (url) WHERE host_id IS NULL;

-- Register everything already reporting so existing samples keep an owner.
INSERT INTO llm_endpoints (host_id, url, runtime, source)
SELECT DISTINCT ON (host_id, endpoint) host_id, endpoint, runtime, 'autodetected'
FROM metrics_llm
ORDER BY host_id, endpoint, time DESC
ON CONFLICT DO NOTHING;

ALTER TABLE metrics_llm ADD COLUMN endpoint_id UUID REFERENCES llm_endpoints (id) ON DELETE CASCADE;

UPDATE metrics_llm m
SET endpoint_id = e.id
FROM llm_endpoints e
WHERE e.host_id = m.host_id AND e.url = m.endpoint;

-- Server-polled samples belong to an endpoint, not a host.
ALTER TABLE metrics_llm ALTER COLUMN host_id DROP NOT NULL;

CREATE INDEX idx_metrics_llm_endpoint_time ON metrics_llm (endpoint_id, time DESC);

-- The continuous aggregate has to be rekeyed onto endpoint_id, and a
-- continuous aggregate's definition cannot be altered in place. Dropping it
-- discards only derived rows; the raw hypertable is untouched and the
-- aggregate refills from it.
DROP MATERIALIZED VIEW metrics_llm_1m;

CREATE MATERIALIZED VIEW metrics_llm_1m
WITH (timescaledb.continuous) AS
SELECT endpoint_id,
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
GROUP BY endpoint_id, bucket
WITH NO DATA;

SELECT add_continuous_aggregate_policy('metrics_llm_1m', start_offset => INTERVAL '3 hours', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '1 minute');
