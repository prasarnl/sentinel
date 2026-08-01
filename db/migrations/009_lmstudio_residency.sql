-- LM Studio reports which model is resident but publishes no Prometheus
-- metrics at all — no KV cache, throughput, or queue depth. Recording
-- residency turns such a host from a permanently failing endpoint into one
-- that honestly says "up, serving X at 32k context", while every performance
-- column stays NULL because those numbers genuinely do not exist for it.

ALTER TABLE metrics_llm DROP CONSTRAINT metrics_llm_runtime_check;
ALTER TABLE metrics_llm ADD CONSTRAINT metrics_llm_runtime_check
    CHECK (runtime IN ('llamacpp', 'vllm', 'lmstudio'));

ALTER TABLE llm_endpoints DROP CONSTRAINT llm_endpoints_runtime_check;
ALTER TABLE llm_endpoints ADD CONSTRAINT llm_endpoints_runtime_check
    CHECK (runtime IN ('auto', 'vllm', 'llamacpp', 'lmstudio'));

-- context_length is what the model was actually loaded with, which is
-- typically far below max_context_length and is the figure that determines
-- real KV cache footprint.
ALTER TABLE metrics_llm ADD COLUMN quantization TEXT;
ALTER TABLE metrics_llm ADD COLUMN context_length INT;
ALTER TABLE metrics_llm ADD COLUMN max_context_length INT;

-- No continuous aggregate change: these are descriptive attributes of what is
-- loaded, not quantities worth averaging over a minute.
