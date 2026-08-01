export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "-";
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const exp = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** exp;
  return `${value >= 100 ? Math.round(value) : value.toFixed(1)} ${units[exp]}`;
}

export function formatBytesPerSec(bytes: number): string {
  return `${formatBytes(bytes)}/s`;
}

export function formatPct(pct: number | null | undefined): string {
  if (pct === null || pct === undefined || !Number.isFinite(pct)) return "-";
  return `${pct.toFixed(1)}%`;
}

export function formatRelativeTime(iso: string | null): string {
  if (!iso) return "never";
  const then = new Date(iso).getTime();
  const diffSec = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (diffSec < 5) return "just now";
  if (diffSec < 60) return `${diffSec}s ago`;
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  return `${Math.floor(diffSec / 86400)}d ago`;
}

// The LLM formatters below return null rather than a dash when a value is
// absent, so StatTile can distinguish "this runtime cannot report it" from a
// real zero and render "n/a".

/** Formats a 0..1 ratio as a percentage. */
export function formatRatioPct(ratio: number | null | undefined): string | null {
  if (ratio === null || ratio === undefined || !Number.isFinite(ratio)) return null;
  return `${(ratio * 100).toFixed(1)}%`;
}

export function formatTokensPerSec(value: number | null | undefined): string | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null;
  return `${value >= 100 ? Math.round(value) : value.toFixed(1)} tok/s`;
}

export function formatMillis(value: number | null | undefined): string | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null;
  if (value >= 1000) return `${(value / 1000).toFixed(2)} s`;
  return `${Math.round(value)} ms`;
}

export function formatCount(value: number | null | undefined): string | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null;
  return `${Math.round(value)}`;
}

export function formatPerSec(value: number | null | undefined, digits = 2): string | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null;
  return `${value.toFixed(digits)}/s`;
}

export function formatFixed(value: number | null | undefined, digits = 2): string | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null;
  return value.toFixed(digits);
}

export function formatClockTime(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
