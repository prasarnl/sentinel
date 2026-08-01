import { useEffect, useRef } from "react";
import type { CPUPoint, DiskPoint, GPUPoint, LLMPoint, MemPoint } from "./api";

export interface IngestEvent {
  host_id: string;
  payload: {
    cpu?: CPUPoint[];
    mem?: MemPoint[];
    disk?: DiskPoint[];
    gpu?: GPUPoint[];
    llm?: LLMPoint[];
  };
}

/** Metric fields that are absent rather than null on the websocket.
 *
 * The REST snapshot builds its rows explicitly, so an unset metric arrives as
 * null. Websocket frames are the Go LLMSample struct, whose fields are tagged
 * omitempty — a nil metric is omitted from the JSON entirely and arrives as
 * undefined. Consumers declared these as `number | null`, so a strict
 * `=== null` check passed straight through an absent field and the next
 * property access threw, blanking the page a couple of seconds after load.
 *
 * Filling them in here keeps that difference at the boundary and makes the
 * declared types true, rather than asking every consumer to remember it. */
const NULLABLE_LLM_FIELDS = [
  "model",
  "kv_cache_usage_ratio",
  "kv_cache_tokens",
  "prompt_tokens_total",
  "generated_tokens_total",
  "prompt_tokens_per_sec",
  "generated_tokens_per_sec",
  "prefix_cache_queries_total",
  "prefix_cache_hits_total",
  "prefix_cache_hit_ratio",
  "requests_running",
  "requests_waiting",
  "ttft_ms_avg",
  "tpot_ms_avg",
  "preemptions_per_sec",
  "spec_decode_acceptance_rate",
  "spec_decode_accepted_per_draft",
  "quantization",
  "context_length",
  "max_context_length",
] as const;

function normalizeLLMPoint(point: LLMPoint): LLMPoint {
  const out: Record<string, unknown> = { ...point };
  for (const field of NULLABLE_LLM_FIELDS) {
    if (out[field] === undefined) out[field] = null;
  }
  return out as unknown as LLMPoint;
}

/** Subscribes to live ingest events for a host over WebSocket, reconnecting
 * with backoff if the connection drops. */
export function useHostStream(hostId: string | null, onEvent: (evt: IngestEvent) => void) {
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  useEffect(() => {
    if (!hostId) return;

    let socket: WebSocket | null = null;
    let closedByEffect = false;
    let retryDelay = 1000;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    function connect() {
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(`${proto}//${window.location.host}/api/v1/hosts/${hostId}/stream`);

      socket.onmessage = (msg) => {
        try {
          const evt = JSON.parse(msg.data) as IngestEvent;
          if (evt.payload?.llm) evt.payload.llm = evt.payload.llm.map(normalizeLLMPoint);
          onEventRef.current(evt);
        } catch {
          // ignore malformed frames
        }
      };

      socket.onopen = () => {
        retryDelay = 1000;
      };

      socket.onclose = () => {
        if (closedByEffect) return;
        retryTimer = setTimeout(connect, retryDelay);
        retryDelay = Math.min(retryDelay * 2, 30000);
      };
    }

    connect();

    return () => {
      closedByEffect = true;
      clearTimeout(retryTimer);
      socket?.close();
    };
  }, [hostId]);
}
