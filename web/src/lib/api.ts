export type Role = "admin" | "viewer";
export type HostOS = "linux" | "windows";
export type HostStatus = "pending" | "online" | "offline" | "removed";

export interface User {
  id: string;
  username: string;
  role: Role;
  created_at: string;
}

export interface Host {
  id: string;
  name: string;
  os: HostOS;
  tags: string[];
  status: HostStatus;
  last_seen: string | null;
  created_at: string;
}

export interface CreateHostResponse {
  host: Host & { enrollment_token: string; enrollment_expires: string };
  install_command: string;
}

export interface CPUPoint {
  time: string;
  usage_pct: number;
  load1?: number;
  load5?: number;
  load15?: number;
}

export interface MemPoint {
  time: string;
  total_bytes: number;
  used_bytes: number;
  available_bytes: number;
  swap_used_bytes: number;
  swap_total_bytes: number;
}

export interface DiskPoint {
  time: string;
  mount: string;
  total_bytes: number;
  used_bytes: number;
  read_bytes_sec: number;
  write_bytes_sec: number;
}

export interface GPUPoint {
  time: string;
  gpu_index: number;
  vendor: string;
  name: string;
  utilization_pct: number | null;
  mem_used_bytes: number | null;
  mem_total_bytes: number | null;
  temp_c: number | null;
}

export type LLMRuntime = "llamacpp" | "vllm";

/** One scrape of an inference runtime's metrics endpoint on a host.
 *
 * Every metric is nullable, and null means "this runtime cannot report it"
 * rather than zero — llama.cpp exposes no prefix-cache or preemption
 * counters, so those read as n/a instead of 0. */
export interface LLMPoint {
  time: string;
  endpoint: string;
  runtime: LLMRuntime;
  model: string | null;
  kv_cache_usage_ratio: number | null; // 0..1
  kv_cache_tokens: number | null;
  prompt_tokens_total: number | null;
  generated_tokens_total: number | null;
  prompt_tokens_per_sec: number | null;
  generated_tokens_per_sec: number | null;
  prefix_cache_queries_total: number | null;
  prefix_cache_hits_total: number | null;
  prefix_cache_hit_ratio: number | null; // 0..1, over the last scrape window
  requests_running: number | null;
  requests_waiting: number | null;
  ttft_ms_avg: number | null;
  tpot_ms_avg: number | null;
  preemptions_per_sec: number | null;
}

/** A history sample. Counts come back as floats because long ranges are
 * served from the 1-minute rollups, where they are averages. */
export interface LLMHistoryPoint {
  time: string;
  kv_cache_usage_ratio: number | null;
  prompt_tokens_per_sec: number | null;
  generated_tokens_per_sec: number | null;
  prefix_cache_hit_ratio: number | null;
  requests_running: number | null;
  requests_waiting: number | null;
  ttft_ms_avg: number | null;
  tpot_ms_avg: number | null;
  preemptions_per_sec: number | null;
}

export interface LatestSnapshot {
  cpu?: CPUPoint;
  mem?: MemPoint;
  disk?: DiskPoint[];
  gpu?: GPUPoint[];
  llm?: LLMPoint[];
}

export interface LLMTarget {
  id: string;
  name: string;
  base_url: string;
  has_api_key: boolean;
  supports_model_swap: boolean;
  created_at: string;
}

export interface LLMBenchmarkConfig {
  target_id: string;
  concurrency: number;
  num_requests: number;
  warmup_requests: number;
  prompt_tokens: number;
  max_tokens: number;
  request_timeout_secs: number;
  model_load_timeout_secs: number;
  // Optional, best-effort: sent as n_ctx/n_batch on each request. Most
  // llama.cpp-family servers fix these at process launch, so whether they
  // have any effect depends on the target's backend. null/omitted means
  // don't send them.
  context_window?: number | null;
  batch_size?: number | null;
  updated_at: string;
}

export type BenchmarkRunStatus = "running" | "completed" | "failed" | "cancelled";

export interface LatencyStats {
  p50: number;
  p95: number;
  mean: number;
  min: number;
  max: number;
}

export interface BenchmarkSummary {
  requests: number;
  failed: number;
  errors?: string[];
  wall_time_ms: number;
  throughput_tokens_per_sec: number;
  ttft_ms: LatencyStats;
  tokens_per_sec: LatencyStats;
}

export interface LLMBenchmarkRun {
  id: string;
  target_id: string;
  model: string;
  batch_id?: string;
  config: LLMBenchmarkConfig;
  status: BenchmarkRunStatus;
  summary?: BenchmarkSummary;
  error?: string;
  started_at: string;
  completed_at?: string;
}

export type BenchmarkStage = "unloading" | "loading" | "benchmarking" | "model_done" | "done";

export interface BenchmarkProgressEvent {
  batch_id: string;
  model?: string;
  model_index?: number;
  models_total?: number;
  stage: BenchmarkStage;
  completed: number;
  total: number;
  failed: number;
  last_ttft_ms?: number;
  last_tokens_per_sec?: number;
  last_error?: string;
  done: boolean;
  run?: LLMBenchmarkRun; // present when stage === "model_done"
}

class APIError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // ignore non-JSON error bodies
    }
    throw new APIError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const api = {
  login: (username: string, password: string) =>
    request<{ id: string; username: string; role: Role }>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<void>("/auth/logout", { method: "POST" }),
  me: () => request<{ id: string; username: string; role: Role }>("/auth/me"),

  listHosts: () => request<Host[]>("/hosts"),
  createHost: (name: string, os: HostOS, tags: string[]) =>
    request<CreateHostResponse>("/hosts", {
      method: "POST",
      body: JSON.stringify({ name, os, tags }),
    }),
  deleteHost: (id: string, purge: boolean) =>
    request<void>(`/hosts/${id}${purge ? "?purge=true" : ""}`, { method: "DELETE" }),

  latestSnapshot: (hostId: string) => request<LatestSnapshot>(`/hosts/${hostId}/latest`),
  historyCPU: (hostId: string, range: string) =>
    request<CPUPoint[]>(`/hosts/${hostId}/history/cpu?range=${range}`),
  historyMem: (hostId: string, range: string) =>
    request<{ time: string; used_bytes: number; total_bytes: number }[]>(
      `/hosts/${hostId}/history/mem?range=${range}`,
    ),
  historyDisk: (hostId: string, mount: string, range: string) =>
    request<
      { time: string; used_bytes: number; total_bytes: number; read_bytes_sec: number; write_bytes_sec: number }[]
    >(`/hosts/${hostId}/history/disk?range=${range}&mount=${encodeURIComponent(mount)}`),
  historyGPU: (hostId: string, gpuIndex: number, range: string) =>
    request<
      { time: string; utilization_pct: number | null; mem_used_bytes: number | null; mem_total_bytes: number | null; temp_c: number | null }[]
    >(`/hosts/${hostId}/history/gpu?range=${range}&gpu_index=${gpuIndex}`),
  historyLLM: (hostId: string, endpoint: string, range: string) =>
    request<LLMHistoryPoint[]>(
      `/hosts/${hostId}/history/llm?range=${range}&endpoint=${encodeURIComponent(endpoint)}`,
    ),

  listUsers: () => request<User[]>("/users"),
  createUser: (username: string, password: string, role: Role) =>
    request<User>("/users", { method: "POST", body: JSON.stringify({ username, password, role }) }),
  updateUser: (id: string, patch: { password?: string; role?: Role }) =>
    request<void>(`/users/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteUser: (id: string) => request<void>(`/users/${id}`, { method: "DELETE" }),

  getSettings: () => request<{ retention_days: string }>("/settings"),
  updateSettings: (retentionDays: number) =>
    request<{ retention_days: number }>("/settings", {
      method: "PUT",
      body: JSON.stringify({ retention_days: retentionDays }),
    }),

  listLLMTargets: () => request<LLMTarget[]>("/llm-targets"),
  getLLMTarget: (id: string) =>
    request<{ target: LLMTarget; config: LLMBenchmarkConfig }>(`/llm-targets/${id}`),
  createLLMTarget: (name: string, baseUrl: string, apiKey: string, supportsModelSwap: boolean) =>
    request<{ target: LLMTarget; config: LLMBenchmarkConfig }>("/llm-targets", {
      method: "POST",
      body: JSON.stringify({
        name,
        base_url: baseUrl,
        api_key: apiKey,
        supports_model_swap: supportsModelSwap,
      }),
    }),
  updateLLMTarget: (
    id: string,
    patch: { name?: string; base_url?: string; api_key?: string; supports_model_swap?: boolean },
  ) => request<LLMTarget>(`/llm-targets/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteLLMTarget: (id: string) => request<void>(`/llm-targets/${id}`, { method: "DELETE" }),

  discoverModels: (baseUrl: string, apiKey: string) =>
    request<{ models: string[] }>("/llm-targets/discover-models", {
      method: "POST",
      body: JSON.stringify({ base_url: baseUrl, api_key: apiKey }),
    }),
  getLLMTargetModels: (id: string) => request<{ models: string[] }>(`/llm-targets/${id}/models`),

  updateLLMBenchmarkConfig: (id: string, config: Omit<LLMBenchmarkConfig, "target_id" | "updated_at">) =>
    request<LLMBenchmarkConfig>(`/llm-targets/${id}/config`, {
      method: "PUT",
      body: JSON.stringify(config),
    }),

  runBenchmark: (id: string, models: string[]) =>
    request<{ batch_id: string; target_id: string; models: string[] }>(`/llm-targets/${id}/benchmark`, {
      method: "POST",
      body: JSON.stringify({ models }),
    }),
  listBenchmarkRuns: (id: string) => request<LLMBenchmarkRun[]>(`/llm-targets/${id}/benchmarks`),
  getBenchmarkRun: (id: string, runId: string) =>
    request<LLMBenchmarkRun>(`/llm-targets/${id}/benchmarks/${runId}`),
  deleteBenchmarkRun: (id: string, runId: string) =>
    request<void>(`/llm-targets/${id}/benchmarks/${runId}`, { method: "DELETE" }),
};

export { APIError };
