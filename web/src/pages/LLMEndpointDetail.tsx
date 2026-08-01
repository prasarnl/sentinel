import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ArrowLeft, AlertTriangle } from "lucide-react";
import { api, type LLMEndpoint, type LLMHistoryPoint } from "../lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Select } from "../components/ui/Select";
import { LLMCharts } from "../components/LLMCharts";
import { formatRelativeTime } from "../lib/format";

const RANGES = [
  { value: "1h", label: "Last hour" },
  { value: "6h", label: "Last 6 hours" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
];

/** Explains an endpoint that is producing nothing, in terms of what to do
 * about it. "No metrics endpoint" in particular is a permanent property of
 * some runtimes rather than an outage, and shouldn't read like one. */
function Diagnosis({ endpoint }: { endpoint: LLMEndpoint }) {
  const err = endpoint.last_scrape_error?.toLowerCase() ?? "";

  let title: string;
  let detail: string;
  if (err.includes("no prometheus metrics")) {
    title = "This runtime publishes no Prometheus metrics";
    detail =
      "The server is reachable and answering, but has no /metrics endpoint — LM Studio behaves this way, for example. KV cache, throughput and queue depth simply aren't exposed by it. vLLM and llama.cpp (started with --metrics) do publish them.";
  } else if (err.includes("not a recognized")) {
    title = "Not a recognized inference runtime";
    detail =
      "Something is serving Prometheus metrics at this address, but none of them look like vLLM or llama.cpp. This is usually a different exporter sharing the port.";
  } else {
    title = "Endpoint unreachable";
    detail = `Nothing answered at ${endpoint.url}. The machine may be off, the server stopped, or a firewall may be blocking the Sentinel server from reaching it.`;
  }

  return (
    <Card>
      <CardContent className="pt-4">
        <div className="flex gap-3">
          <AlertTriangle size={16} className="mt-0.5 shrink-0 text-[var(--series-3)]" />
          <div>
            <p className="text-sm font-medium">{title}</p>
            <p className="mt-1 text-sm text-[var(--text-secondary)]">{detail}</p>
            {endpoint.last_scrape_error && (
              <p className="mt-2 font-mono text-xs text-[var(--text-muted)]">{endpoint.last_scrape_error}</p>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

export function LLMEndpointDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [endpoint, setEndpoint] = useState<LLMEndpoint | null>(null);
  const [history, setHistory] = useState<LLMHistoryPoint[]>([]);
  const [range, setRange] = useState("1h");

  useEffect(() => {
    api
      .listLLMEndpoints()
      .then((all) => setEndpoint(all.find((e) => e.id === id) ?? null))
      .catch(() => {});
  }, [id]);

  useEffect(() => {
    if (!id) return;
    api.historyLLMEndpoint(id, range).then(setHistory).catch(() => {});
    // The server polls remote endpoints rather than pushing them over the
    // host websocket, so this refreshes on a timer instead of a stream.
    const timer = setInterval(() => {
      api.historyLLMEndpoint(id, range).then(setHistory).catch(() => {});
    }, 15000);
    return () => clearInterval(timer);
  }, [id, range]);

  if (!endpoint) return <div className="text-sm text-[var(--text-muted)]">Loading…</div>;

  const healthy = !endpoint.last_scrape_error;

  return (
    <div>
      <button
        onClick={() => navigate("/llm-endpoints")}
        className="mb-4 flex items-center gap-1.5 text-sm text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
      >
        <ArrowLeft size={14} /> Back to endpoints
      </button>

      <div className="mb-6 flex items-center justify-between">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">
            {endpoint.name || endpoint.model || endpoint.url}
          </h1>
          <p className="truncate font-mono text-xs text-[var(--text-muted)]">
            {endpoint.url}
            {endpoint.host_name ? ` · agent on ${endpoint.host_name}` : " · polled by the Sentinel server"}
            {endpoint.last_scrape_at ? ` · last scrape ${formatRelativeTime(endpoint.last_scrape_at)}` : ""}
          </p>
        </div>
        <Select value={range} onChange={(e) => setRange(e.target.value)}>
          {RANGES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </Select>
      </div>

      {!healthy && <Diagnosis endpoint={endpoint} />}

      {healthy && (
        <Card>
          <CardHeader>
            <CardTitle>{endpoint.model || "Runtime metrics"}</CardTitle>
            <span className="text-xs text-[var(--text-muted)]">{endpoint.runtime}</span>
          </CardHeader>
          <CardContent>
            <LLMCharts history={history} />
          </CardContent>
        </Card>
      )}
    </div>
  );
}
