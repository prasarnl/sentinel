import { useEffect, useRef } from "react";
import type { CPUPoint, DiskPoint, GPUPoint, MemPoint } from "./api";

export interface IngestEvent {
  host_id: string;
  payload: {
    cpu?: CPUPoint[];
    mem?: MemPoint[];
    disk?: DiskPoint[];
    gpu?: GPUPoint[];
  };
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
