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

export interface LatestSnapshot {
  cpu?: CPUPoint;
  mem?: MemPoint;
  disk?: DiskPoint[];
  gpu?: GPUPoint[];
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
};

export { APIError };
