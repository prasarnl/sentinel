import type { LLMHistoryPoint } from "../lib/api";
import { HistoryChart, type ChartSeries } from "./HistoryChart";

// Series colors come from the app's categorical palette. Each pairing below
// was checked for colorblind separation against both the light and dark
// surfaces; green/orange and green/red both fail for protanopia, so the queue
// chart uses blue/red and the cache chart orange/aqua.
export const KV_CACHE_COLOR = "var(--series-8)";
export const PREFIX_HIT_COLOR = "var(--series-2)";
export const PROMPT_COLOR = "var(--series-1)";
export const GENERATED_COLOR = "var(--series-4)";
export const RUNNING_COLOR = "var(--series-1)";
export const WAITING_COLOR = "var(--series-6)";
// Single-series chart, so this only had to clear the lightness, chroma and
// contrast checks rather than a CVD separation against a partner.
export const SPEC_DECODE_COLOR = "var(--series-5)";

type ChartRow = Record<string, unknown>;

/** Builds chart rows, keeping nulls as nulls so a gap in the data reads as a
 * gap rather than a drop to zero. */
function toRows(points: LLMHistoryPoint[], map: (p: LLMHistoryPoint) => ChartRow): ChartRow[] {
  return points.map((p) => ({ time: p.time, ...map(p) }));
}

/** Drops series with no data at all. A runtime that never reports a metric
 * shouldn't contribute an always-empty line and a legend entry for it. */
function seriesWithData(rows: ChartRow[], candidates: ChartSeries[]): ChartSeries[] {
  return candidates.filter((s) => rows.some((r) => r[s.key] !== null && r[s.key] !== undefined));
}

/** The four charts describing an inference endpoint over time. Shared by the
 * per-host panel and the remote-endpoint view so both stay identical. */
export function LLMCharts({ history }: { history: LLMHistoryPoint[] }) {
  const cacheRows = toRows(history, (p) => ({
    kv_cache_pct: p.kv_cache_usage_ratio === null ? null : p.kv_cache_usage_ratio * 100,
    prefix_hit_pct: p.prefix_cache_hit_ratio === null ? null : p.prefix_cache_hit_ratio * 100,
  }));
  const throughputRows = toRows(history, (p) => ({
    prompt: p.prompt_tokens_per_sec,
    generated: p.generated_tokens_per_sec,
  }));
  const queueRows = toRows(history, (p) => ({
    running: p.requests_running,
    waiting: p.requests_waiting,
  }));

  const cacheSeries = seriesWithData(cacheRows, [
    { key: "kv_cache_pct", label: "KV cache used", color: KV_CACHE_COLOR },
    { key: "prefix_hit_pct", label: "Prefix cache hit rate", color: PREFIX_HIT_COLOR },
  ]);
  const generatedSeries = seriesWithData(throughputRows, [
    { key: "generated", label: "Generated tok/s", color: GENERATED_COLOR },
  ]);
  const promptSeries = seriesWithData(throughputRows, [
    { key: "prompt", label: "Prompt tok/s", color: PROMPT_COLOR },
  ]);
  const queueSeries = seriesWithData(queueRows, [
    { key: "running", label: "Running", color: RUNNING_COLOR },
    { key: "waiting", label: "Waiting", color: WAITING_COLOR },
  ]);

  const specRows = toRows(history, (p) => ({
    acceptance_pct:
      p.spec_decode_acceptance_rate === null ? null : p.spec_decode_acceptance_rate * 100,
  }));
  const specSeries = seriesWithData(specRows, [
    { key: "acceptance_pct", label: "Draft acceptance", color: SPEC_DECODE_COLOR },
  ]);

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {/* Both series are percentages, so they share one axis. The story here
          is the cache filling while the hit rate falls. */}
      <div className="lg:col-span-2">
        <div className="mb-1 text-xs text-[var(--text-secondary)]">Cache</div>
        <HistoryChart data={cacheRows} series={cacheSeries} yFormatter={(v) => `${Math.round(v)}%`} />
      </div>

      {/* Generation and prompt-processing rates share a unit but not a
          magnitude — prefill routinely runs 10x decode, which flattens the
          generation line to nothing if the two share an axis. They get one
          chart each rather than a second y-axis. */}
      <div>
        <div className="mb-1 text-xs text-[var(--text-secondary)]">Generation rate</div>
        <HistoryChart data={throughputRows} series={generatedSeries} yFormatter={(v) => `${Math.round(v)}`} height={180} />
      </div>

      <div>
        <div className="mb-1 text-xs text-[var(--text-secondary)]">Prompt processing rate</div>
        <HistoryChart data={throughputRows} series={promptSeries} yFormatter={(v) => `${Math.round(v)}`} height={180} />
      </div>

      <div className={specSeries.length > 0 ? "" : "lg:col-span-2"}>
        <div className="mb-1 text-xs text-[var(--text-secondary)]">Request queue</div>
        <HistoryChart data={queueRows} series={queueSeries} yFormatter={(v) => `${Math.round(v)}`} height={180} />
      </div>

      {/* Omitted entirely for runtimes that aren't speculating, rather than
          drawn as an empty axis. */}
      {specSeries.length > 0 && (
        <div>
          <div className="mb-1 text-xs text-[var(--text-secondary)]">Draft acceptance</div>
          <HistoryChart
            data={specRows}
            series={specSeries}
            yFormatter={(v) => `${Math.round(v)}%`}
            height={180}
          />
        </div>
      )}
    </div>
  );
}
