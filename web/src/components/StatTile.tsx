import { cn } from "../lib/cn";

interface StatTileProps {
  label: string;
  /** Pre-formatted value, or null when the runtime cannot report this metric. */
  value: string | null;
  /** Optional context under the value, e.g. what the number is out of. */
  hint?: string;
  /** Accent dot color, used to tie a tile to its series in the chart below. */
  color?: string;
  className?: string;
}

/** A single current-value readout. Used for metrics where the number itself is
 * the answer (cache hit rate, queue depth) and a chart would add nothing.
 *
 * A null value renders as "n/a" rather than 0: llama.cpp exposes no
 * prefix-cache counters at all, and showing "0%" there would read as a cache
 * that never hits instead of one that isn't measurable. */
export function StatTile({ label, value, hint, color, className }: StatTileProps) {
  return (
    <div className={cn("rounded-md border border-[var(--border)] px-3 py-2", className)}>
      <div className="flex items-center gap-1.5 text-xs text-[var(--text-secondary)]">
        {color && <span className="inline-block h-2 w-2 shrink-0 rounded-full" style={{ background: color }} />}
        <span className="truncate">{label}</span>
      </div>
      <div
        className={cn(
          "mt-0.5 tabular-nums text-lg font-medium",
          value === null ? "text-[var(--text-muted)]" : "text-[var(--text-primary)]",
        )}
      >
        {value ?? "n/a"}
      </div>
      {hint && <div className="text-xs text-[var(--text-muted)]">{hint}</div>}
    </div>
  );
}
