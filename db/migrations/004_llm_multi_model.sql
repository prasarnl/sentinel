-- Model selection moves from a fixed target-level attribute to a per-run
-- choice, so a single target can benchmark multiple models. Targets that
-- sit behind a swap-capable proxy (e.g. llama-swap) can opt into the
-- unload/load-one-at-a-time orchestration so VRAM never holds more than
-- one model at once.

ALTER TABLE llm_targets DROP COLUMN model;
ALTER TABLE llm_targets ADD COLUMN supports_model_swap BOOLEAN NOT NULL DEFAULT false;

-- Distinct, longer timeout for each model's warmup request: spinning up a
-- fresh backend process and loading weights can take far longer than
-- steady-state generation, which request_timeout_secs governs instead.
ALTER TABLE llm_benchmark_configs ADD COLUMN model_load_timeout_secs INT NOT NULL DEFAULT 180;

ALTER TABLE llm_benchmark_runs ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_benchmark_runs ADD COLUMN batch_id TEXT;
CREATE INDEX idx_llm_benchmark_runs_batch ON llm_benchmark_runs (batch_id) WHERE batch_id IS NOT NULL;
