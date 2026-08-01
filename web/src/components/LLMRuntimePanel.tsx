import { useEffect, useState } from "react";
import { api, type LLMHistoryPoint, type LLMPoint } from "../lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/Card";
import { Select } from "./ui/Select";
import {
  LLMCharts,
  KV_CACHE_COLOR,
  PREFIX_HIT_COLOR,
  PROMPT_COLOR,
  GENERATED_COLOR,
  RUNNING_COLOR,
  SPEC_DECODE_COLOR,
} from "./LLMCharts";
import { StatTile } from "./StatTile";
import { formatCount, formatMillis, formatRatioPct, formatTokensPerSec } from "../lib/format";

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
          spec_decode_acceptance_rate: current.spec_decode_acceptance_rate,
          spec_decode_accepted_per_draft: current.spec_decode_accepted_per_draft,
        },
      ].slice(-500);
    });
    // current is a fresh object each render; latestTime is what actually changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latestTime, endpoint]);

  if (!current) return null;

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

          {/* Only rendered when the runtime is actually speculating, rather
              than sitting there as two permanent n/a tiles on the servers
              that aren't. */}
          {(current.spec_decode_acceptance_rate !== null ||
            current.spec_decode_accepted_per_draft !== null) && (
            <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
              <StatTile
                label="Draft acceptance"
                value={formatRatioPct(current.spec_decode_acceptance_rate)}
                hint="of speculated tokens kept"
                color={SPEC_DECODE_COLOR}
              />
              <StatTile
                label="Accepted per draft"
                value={
                  current.spec_decode_accepted_per_draft === null
                    ? null
                    : current.spec_decode_accepted_per_draft.toFixed(2)
                }
                hint="compare to num_speculative_tokens"
              />
            </div>
          )}

          <LLMCharts history={history} />

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
