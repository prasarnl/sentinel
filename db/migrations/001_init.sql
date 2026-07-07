-- Sentinel monitoring system: initial schema
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------------------
-- Auth
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('admin', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Hosts
-- ---------------------------------------------------------------------------
CREATE TABLE hosts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL UNIQUE,
    os                TEXT NOT NULL CHECK (os IN ('linux', 'windows')),
    tags              TEXT[] NOT NULL DEFAULT '{}',
    enrollment_token  TEXT UNIQUE,             -- one-time, cleared after agent enrolls
    enrollment_expires TIMESTAMPTZ,
    api_key_hash      TEXT,                    -- set once agent exchanges enrollment token
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'online', 'offline', 'removed')),
    last_seen         TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_hosts_api_key_hash ON hosts (api_key_hash) WHERE api_key_hash IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Settings (key/value store, e.g. retention_days)
-- ---------------------------------------------------------------------------
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO settings (key, value) VALUES ('retention_days', '90');

-- ---------------------------------------------------------------------------
-- Metrics hypertables
-- ---------------------------------------------------------------------------
CREATE TABLE metrics_cpu (
    time       TIMESTAMPTZ NOT NULL,
    host_id    UUID NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    usage_pct  DOUBLE PRECISION NOT NULL,
    load1      DOUBLE PRECISION,
    load5      DOUBLE PRECISION,
    load15     DOUBLE PRECISION
);
SELECT create_hypertable('metrics_cpu', 'time');
CREATE INDEX idx_metrics_cpu_host_time ON metrics_cpu (host_id, time DESC);

CREATE TABLE metrics_mem (
    time              TIMESTAMPTZ NOT NULL,
    host_id           UUID NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    total_bytes       BIGINT NOT NULL,
    used_bytes        BIGINT NOT NULL,
    available_bytes   BIGINT NOT NULL,
    swap_used_bytes   BIGINT NOT NULL DEFAULT 0,
    swap_total_bytes  BIGINT NOT NULL DEFAULT 0
);
SELECT create_hypertable('metrics_mem', 'time');
CREATE INDEX idx_metrics_mem_host_time ON metrics_mem (host_id, time DESC);

CREATE TABLE metrics_disk (
    time            TIMESTAMPTZ NOT NULL,
    host_id         UUID NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    mount           TEXT NOT NULL,
    total_bytes     BIGINT NOT NULL,
    used_bytes      BIGINT NOT NULL,
    read_bytes_sec  DOUBLE PRECISION NOT NULL DEFAULT 0,
    write_bytes_sec DOUBLE PRECISION NOT NULL DEFAULT 0
);
SELECT create_hypertable('metrics_disk', 'time');
CREATE INDEX idx_metrics_disk_host_time ON metrics_disk (host_id, time DESC);

CREATE TABLE metrics_gpu (
    time             TIMESTAMPTZ NOT NULL,
    host_id          UUID NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    gpu_index        INT NOT NULL,
    vendor           TEXT NOT NULL CHECK (vendor IN ('nvidia', 'amd')),
    name             TEXT,
    utilization_pct  DOUBLE PRECISION,
    mem_used_bytes   BIGINT,
    mem_total_bytes  BIGINT,
    temp_c           DOUBLE PRECISION
);
SELECT create_hypertable('metrics_gpu', 'time');
CREATE INDEX idx_metrics_gpu_host_time ON metrics_gpu (host_id, time DESC);

-- ---------------------------------------------------------------------------
-- Continuous aggregates (1-minute rollups) for fast long-range charts
-- ---------------------------------------------------------------------------
CREATE MATERIALIZED VIEW metrics_cpu_1m
WITH (timescaledb.continuous) AS
SELECT host_id,
       time_bucket('1 minute', time) AS bucket,
       avg(usage_pct) AS avg_usage_pct,
       max(usage_pct) AS max_usage_pct
FROM metrics_cpu
GROUP BY host_id, bucket
WITH NO DATA;

CREATE MATERIALIZED VIEW metrics_mem_1m
WITH (timescaledb.continuous) AS
SELECT host_id,
       time_bucket('1 minute', time) AS bucket,
       avg(used_bytes) AS avg_used_bytes,
       avg(total_bytes) AS avg_total_bytes
FROM metrics_mem
GROUP BY host_id, bucket
WITH NO DATA;

CREATE MATERIALIZED VIEW metrics_disk_1m
WITH (timescaledb.continuous) AS
SELECT host_id,
       mount,
       time_bucket('1 minute', time) AS bucket,
       avg(used_bytes) AS avg_used_bytes,
       avg(total_bytes) AS avg_total_bytes,
       avg(read_bytes_sec) AS avg_read_bytes_sec,
       avg(write_bytes_sec) AS avg_write_bytes_sec
FROM metrics_disk
GROUP BY host_id, mount, bucket
WITH NO DATA;

CREATE MATERIALIZED VIEW metrics_gpu_1m
WITH (timescaledb.continuous) AS
SELECT host_id,
       gpu_index,
       time_bucket('1 minute', time) AS bucket,
       avg(utilization_pct) AS avg_utilization_pct,
       avg(mem_used_bytes) AS avg_mem_used_bytes,
       avg(mem_total_bytes) AS avg_mem_total_bytes,
       avg(temp_c) AS avg_temp_c
FROM metrics_gpu
GROUP BY host_id, gpu_index, bucket
WITH NO DATA;

SELECT add_continuous_aggregate_policy('metrics_cpu_1m', start_offset => INTERVAL '3 hours', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '1 minute');
SELECT add_continuous_aggregate_policy('metrics_mem_1m', start_offset => INTERVAL '3 hours', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '1 minute');
SELECT add_continuous_aggregate_policy('metrics_disk_1m', start_offset => INTERVAL '3 hours', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '1 minute');
SELECT add_continuous_aggregate_policy('metrics_gpu_1m', start_offset => INTERVAL '3 hours', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '1 minute');

-- ---------------------------------------------------------------------------
-- Retention policies (default 90 days, adjusted at runtime by the server
-- whenever settings.retention_days changes)
-- ---------------------------------------------------------------------------
SELECT add_retention_policy('metrics_cpu', INTERVAL '90 days');
SELECT add_retention_policy('metrics_mem', INTERVAL '90 days');
SELECT add_retention_policy('metrics_disk', INTERVAL '90 days');
SELECT add_retention_policy('metrics_gpu', INTERVAL '90 days');
