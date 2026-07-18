-- Optional, best-effort model-load-related parameters (context window,
-- batch size) sent as extra fields (n_ctx, n_batch) on each chat completion
-- request. Most llama.cpp-family servers treat context size and batch size
-- as fixed at process launch, so whether these have any effect depends on
-- the target's backend; unrecognized fields are simply ignored by servers
-- that don't support per-request overrides. NULL means "don't send it".
ALTER TABLE llm_benchmark_configs ADD COLUMN context_window INT;
ALTER TABLE llm_benchmark_configs ADD COLUMN batch_size INT;
