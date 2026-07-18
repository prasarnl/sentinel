import { useEffect, useRef } from "react";
import type { BenchmarkProgressEvent } from "./api";

/** Subscribes to live progress events for a running (possibly multi-model)
 * benchmark batch over WebSocket, reconnecting with backoff if the
 * connection drops. */
export function useBenchmarkStream(
  targetId: string | null,
  batchId: string | null,
  onEvent: (evt: BenchmarkProgressEvent) => void,
) {
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  useEffect(() => {
    if (!targetId || !batchId) return;

    let socket: WebSocket | null = null;
    let closedByEffect = false;
    let retryDelay = 1000;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    function connect() {
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(
        `${proto}//${window.location.host}/api/v1/llm-targets/${targetId}/benchmarks/batch/${batchId}/stream`,
      );

      socket.onmessage = (msg) => {
        try {
          const evt = JSON.parse(msg.data) as BenchmarkProgressEvent;
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
  }, [targetId, batchId]);
}
