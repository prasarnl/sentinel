import { useEffect, useState } from "react";
import { api, type LLMHistoryPoint, type LLMPoint } from "../lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/Card";
import { Select } from "./ui/Select";
import { HistoryChart, type ChartSeries } from "./HistoryChart";
import { StatTile } from "./StatTile";
import { formatCount, formatMillis, formatRatioPct, formatTokensPerSec } from "../lib/format";

// Series colors are drawn from the app's categorical palette. Each pairing
// below was checked for colorblind separation against both the light and dark
// surfaces; notably green/red and green/orange both fail for protanopia, so
// the queue chart uses blue/red and the cache chart uses orange/aqua.
const KV_CACHE_COLOR = "var(--series-8)";
const PREFIX_HIT_COLOR = "var(--series-2)";
const PROMPT_COLOR = "var(--series-1)";
const GENERATED_COLOR = "var(--series-4)";
const RUNNING_COLOR = "var(--series-1)";
const WAITING_COLOR = "var(--series-6)";

type ChartRow = Record<string, unknown>;

/** Builds chart rows, keeping nulls as nulls so a gap in the data reads as a
 * gap rather than a drop to zero. */
function toRows(points: LLMHistoryPoint[], map: (p: LLMHistoryPoint) => ChartRow): ChartRow[] {
  return points.map((p) => ({ time: p.time, ...map(p) }));
}

/** Whether any row carries a value for a key. A runtime that never reports a
 * metric shouldn't contribute an always-empty line and a legend entry for it. */
function hasData(rows: ChartRow[], key: string): boolean {
  return rows.some((r) => r[key] !== null && r[key] !== undefined);
}

function seriesWithData(rows: ChartRow[], candidates: ChartSeries[]): ChartSeries[] {
  return candidates.filter((s) => hasData(rows, s.key));
}

export function LLMRuntimePanel({
  hostId,
  range,
  samples,
}: {
  hostId: string;
  range: string;
  samples: LLMPoint[];
}) {
  const [endpoint, setEndpoint] = useState<string>(samples[0]?.endpoint ?? "");
  const [history, setHistory] = useState<LLMHistoryPoint[]>([]);

  // If the selected endpoint disappears (runtime shut down, agent stopped
  // detecting it), fall back to whichever is still reporting.
  const current = samples.find((s) => s.endpoint === endpoint) ?? samples[0];

  useEffect(() => {
    if (current && current.endpoint !== endpoint) setEndpoint(current.endpoint);
  }, [current, endpoint]);

  useEffect(() => {
    if (!hostId || !endpoint) return;
    api
      .historyLLM(hostId, endpoint, range)
      .then(setHistory)
      .catch(() => {});
  }, [hostId, endpoint, range]);

  // Extend the charts from the live stream so they track alongside the tiles
  // instead of sitting frozen until the range changes, matching how the CPU
  // and memory charts behave. Keyed on the sample timestamp so re-renders
  // that don't carry a new scrape don't duplicate the last point.
  const latestTime = current?.time;
  useEffect(() => {
    if (!current || current.endpoint !== endpoint) return;
    setHistory((prev) => {
      if (prev.length > 0 && prev[prev.length - 1].time === current.time) return prev;
      return [
        ...prev,
        {
          time: current.time,
          kv_cache_usage_ratio: current.kv_cache_usage_ratio,
          prompt_tokens_per_sec: current.prompt_tokens_per_sec,
          generated_tokens_per_sec: current.generated_tokens_per_sec,
          prefix_cache_hit_ratio: current.prefix_cache_hit_ratio,
          requests_running: current.requests_running,
          requests_waiting: current.requests_waiting,
          ttft_ms_avg: current.ttft_ms_avg,
          tpot_ms_avg: current.tpot_ms_avg,
          preemptions_per_sec: current.preemptions_per_sec,
        },
      ].slice(-500);
    });
    // current is a fresh object each render; latestTime is what actually changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latestTime, endpoint]);

  if (!current) return null;

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

  const runtimeLabel = current.runtime === "vllm" ? "vLLM" : "llama.cpp";

  return (
    <div className="mt-4">
      <Card>
        <CardHeader>
          <div className="flex min-w-0 items-center gap-2">
            <CardTitle className="truncate">
              LLM runtime — {current.model || "unknown model"}
            </CardTitle>
            <span className="shrink-0 rounded border border-[var(--border)] px-1.5 py-0.5 text-xs text-[var(--text-muted)]">
              {runtimeLabel}
            </span>
          </div>
          {samples.length > 1 ? (
            <Select value={endpoint} onChange={(e) => setEndpoint(e.target.value)}>
              {samples.map((s) => (
                <option key={s.endpoint} value={s.endpoint}>
                  {s.endpoint}
                </option>
              ))}
            </Select>
          ) : (
            <span className="shrink-0 text-xs text-[var(--text-muted)]">{current.endpoint}</span>
          )}
        </CardHeader>

        <CardContent>
          <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
            <StatTile
              label="KV cache used"
              value={formatRatioPct(current.kv_cache_usage_ratio)}
              hint={current.kv_cache_tokens ? `${current.kv_cache_tokens.toLocaleString()} tokens` : undefined}
              color={KV_CACHE_COLOR}
            />
            <StatTile
              label="Prefix cache hits"
              value={formatRatioPct(current.prefix_cache_hit_ratio)}
              color={PREFIX_HIT_COLOR}
            />
            <StatTile
              label="Generating"
              value={formatTokensPerSec(current.generated_tokens_per_sec)}
              color={GENERATED_COLOR}
            />
            <StatTile
              label="Prompt processing"
              value={formatTokensPerSec(current.prompt_tokens_per_sec)}
              color={PROMPT_COLOR}
            />
            <StatTile
              label="Requests running"
              value={formatCount(current.requests_running)}
              hint={
                current.requests_waiting === null ? undefined : `${current.requests_waiting} waiting`
              }
              color={RUNNING_COLOR}
            />
            <StatTile label="Time to first token" value={formatMillis(current.ttft_ms_avg)} />
            <StatTile label="Per output token" value={formatMillis(current.tpot_ms_avg)} />
            <StatTile
              label="Preemptions"
              value={
                current.preemptions_per_sec === null
                  ? null
                  : `${current.preemptions_per_sec.toFixed(2)}/s`
              }
              hint={current.preemptions_per_sec ? "cache oversubscribed" : undefined}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {/* Both series are percentages, so they share one axis. The story
                here is cache filling while the hit rate falls. */}
            <div className="lg:col-span-2">
              <div className="mb-1 text-xs text-[var(--text-secondary)]">Cache</div>
              <HistoryChart
                data={cacheRows}
                series={cacheSeries}
                yFormatter={(v) => `${Math.round(v)}%`}
              />
            </div>

            {/* Generation and prompt-processing rates share a unit but not a
                magnitude — prefill routinely runs 10x decode, which flattens
                the generation line to nothing if the two share an axis. They
                get one chart each rather than a second y-axis. */}
            <div>
              <div className="mb-1 text-xs text-[var(--text-secondary)]">Generation rate</div>
              <HistoryChart
                data={throughputRows}
                series={generatedSeries}
                yFormatter={(v) => `${Math.round(v)}`}
                height={180}
              />
            </div>

            <div>
              <div className="mb-1 text-xs text-[var(--text-secondary)]">Prompt processing rate</div>
              <HistoryChart
                data={throughputRows}
                series={promptSeries}
                yFormatter={(v) => `${Math.round(v)}`}
                height={180}
              />
            </div>

            <div className="lg:col-span-2">
              <div className="mb-1 text-xs text-[var(--text-secondary)]">Request queue</div>
              <HistoryChart
                data={queueRows}
                series={queueSeries}
                yFormatter={(v) => `${Math.round(v)}`}
                height={180}
              />
            </div>
          </div>

          {current.runtime === "llamacpp" && (
            <p className="mt-3 text-xs text-[var(--text-muted)]">
              llama.cpp does not expose prefix-cache, time-to-first-token, or preemption metrics, so
              those read as n/a rather than zero.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
