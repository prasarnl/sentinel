-- Speculative decoding effectiveness, for runtimes configured with it (MTP,
-- EAGLE, n-gram, a draft model).
--
-- The two figures answer different questions. The acceptance rate is how much
-- of the speculated work survived verification. Accepted-per-draft, compared
-- against the configured num_speculative_tokens, says whether that setting is
-- earning its cost: speculating 3 tokens and keeping 1.2 of them is wasted
-- compute, while keeping 2.6 suggests there is headroom to speculate further.
--
-- NULL means the runtime isn't speculating at all, which is a different thing
-- from speculating and never being accepted.

ALTER TABLE metrics_llm ADD COLUMN spec_decode_acceptance_rate DOUBLE PRECISION;   -- 0..1
ALTER TABLE metrics_llm ADD COLUMN spec_decode_accepted_per_draft DOUBLE PRECISION; -- tokens

-- A continuous aggregate's definition cannot be altered, so adding columns
-- means recreating it. Only derived rows are discarded; the raw hypertable is
-- untouched and the aggregate refills from it.
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
       avg(preemptions_per_sec) AS avg_preemptions_per_sec,
       avg(spec_decode_acceptance_rate) AS avg_spec_decode_acceptance_rate,
       avg(spec_decode_accepted_per_draft) AS avg_spec_decode_accepted_per_draft
FROM metrics_llm
GROUP BY endpoint_id, bucket
WITH NO DATA;

SELECT add_continuous_aggregate_policy('metrics_llm_1m', start_offset => INTERVAL '3 hours', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '1 minute');
