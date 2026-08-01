import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Boxes, Plus, Trash2, Radio } from "lucide-react";
import { api, type Host, type LLMEndpoint, type LLMTarget } from "../lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { Select } from "../components/ui/Select";
import { useAuth } from "../lib/auth";
import { formatRelativeTime } from "../lib/format";

const RUNTIMES = [
  { value: "auto", label: "Auto-detect" },
  { value: "vllm", label: "vLLM" },
  { value: "llamacpp", label: "llama.cpp" },
];

/** Describes what an endpoint is currently doing, in words. Reachability,
 * "answers HTTP but publishes no metrics", and "working" are three genuinely
 * different situations that all look like an empty chart otherwise. */
function scrapeStatus(e: LLMEndpoint): { label: string; tone: "ok" | "warn" | "bad" | "idle" } {
  if (!e.enabled) return { label: "Disabled", tone: "idle" };
  if (e.last_scrape_error) {
    const err = e.last_scrape_error.toLowerCase();
    if (err.includes("no prometheus metrics")) {
      return { label: "No metrics endpoint", tone: "warn" };
    }
    if (err.includes("not a recognized")) {
      return { label: "Not an LLM runtime", tone: "warn" };
    }
    return { label: "Unreachable", tone: "bad" };
  }
  if (!e.last_scrape_at) return { label: "Awaiting first scrape", tone: "idle" };
  return { label: `Reporting · ${formatRelativeTime(e.last_scrape_at)}`, tone: "ok" };
}

const TONE_CLASS: Record<string, string> = {
  ok: "text-[var(--series-4)]",
  warn: "text-[var(--series-3)]",
  bad: "text-[var(--series-6)]",
  idle: "text-[var(--text-muted)]",
};

function EndpointRow({
  endpoint,
  isAdmin,
  onChange,
}: {
  endpoint: LLMEndpoint;
  isAdmin: boolean;
  onChange: () => void;
}) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const status = scrapeStatus(endpoint);

  async function toggle() {
    setBusy(true);
    try {
      await api.updateLLMEndpoint(endpoint.id, { enabled: !endpoint.enabled });
      onChange();
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    // Deleting an autodetected endpoint that is still running only makes it
    // reappear on the next discovery sweep, so say so rather than letting it
    // look like the delete failed.
    const warning =
      endpoint.source === "autodetected"
        ? `\n\nThis endpoint was found automatically. If it is still running it will be rediscovered within a minute — disable it instead to stop monitoring it for good.`
        : "";
    if (!confirm(`Delete ${endpoint.name || endpoint.url} and all its collected metrics?${warning}`)) {
      return;
    }
    setBusy(true);
    try {
      await api.deleteLLMEndpoint(endpoint.id);
      onChange();
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex items-center gap-3 border-b border-[var(--border)] py-2.5 last:border-b-0">
      <button
        onClick={() => navigate(`/llm-endpoints/${endpoint.id}`)}
        className="min-w-0 flex-1 cursor-pointer text-left"
      >
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{endpoint.name || endpoint.model || endpoint.url}</span>
          {/* Not labelled "auto": the runtime badge beside this one uses
              "auto" to mean "detect the runtime", and the two would read as
              the same thing. */}
          {endpoint.source === "autodetected" && (
            <span className="shrink-0 rounded border border-[var(--border)] px-1 py-px text-[10px] text-[var(--text-muted)]">
              discovered
            </span>
          )}
          {endpoint.runtime !== "auto" && (
            <span className="shrink-0 text-xs text-[var(--text-muted)]">{endpoint.runtime}</span>
          )}
        </div>
        <div className="truncate font-mono text-xs text-[var(--text-muted)]">{endpoint.url}</div>
      </button>

      <div className={`shrink-0 text-xs ${TONE_CLASS[status.tone]}`}>{status.label}</div>

      {isAdmin && (
        <div className="flex shrink-0 items-center gap-1">
          <Button variant="secondary" onClick={toggle} disabled={busy}>
            {endpoint.enabled ? "Disable" : "Enable"}
          </Button>
          <button
            onClick={remove}
            disabled={busy}
            title="Delete endpoint and its metrics"
            className="rounded-md p-1.5 text-[var(--text-muted)] transition-colors hover:bg-[var(--gridline)]/40 hover:text-[var(--series-6)]"
          >
            <Trash2 size={14} />
          </button>
        </div>
      )}
    </div>
  );
}

function AddEndpointForm({ hosts, targets, onAdded }: { hosts: Host[]; targets: LLMTarget[]; onAdded: () => void }) {
  const [url, setUrl] = useState("");
  const [name, setName] = useState("");
  const [runtime, setRuntime] = useState("auto");
  const [apiKey, setApiKey] = useState("");
  const [hostId, setHostId] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // Registering an endpoint against a host duplicates what that host's agent
  // discovers by itself, and the same server would then be recorded under two
  // identities. Remote is the case that actually needs manual entry.
  const duplicatesAgent = hostId !== "";

  function prefillFromTarget(targetId: string) {
    const t = targets.find((x) => x.id === targetId);
    if (!t) return;
    setUrl(t.base_url);
    if (!name) setName(t.name);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      await api.createLLMEndpoint({
        host_id: hostId || null,
        name,
        url,
        runtime,
        api_key: apiKey,
      });
      setUrl("");
      setName("");
      setApiKey("");
      onAdded();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add endpoint");
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={submit} className="space-y-3">
      {targets.length > 0 && (
        <div>
          <label className="mb-1 block text-xs text-[var(--text-secondary)]">Prefill from a benchmark target</label>
          <Select defaultValue="" onChange={(e) => prefillFromTarget(e.target.value)}>
            <option value="">—</option>
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name} ({t.base_url})
              </option>
            ))}
          </Select>
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label className="mb-1 block text-xs text-[var(--text-secondary)]">Base URL</label>
          <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://192.168.1.50:8000" required />
        </div>
        <div>
          <label className="mb-1 block text-xs text-[var(--text-secondary)]">Name (optional)</label>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Mac Studio vLLM" />
        </div>
        <div>
          <label className="mb-1 block text-xs text-[var(--text-secondary)]">Scraped by</label>
          <Select value={hostId} onChange={(e) => setHostId(e.target.value)}>
            <option value="">Sentinel server (no agent on that machine)</option>
            {hosts
              .filter((h) => h.status !== "removed")
              .map((h) => (
                <option key={h.id} value={h.id}>
                  Agent on {h.name}
                </option>
              ))}
          </Select>
        </div>
        <div>
          <label className="mb-1 block text-xs text-[var(--text-secondary)]">Runtime</label>
          <Select value={runtime} onChange={(e) => setRuntime(e.target.value)}>
            {RUNTIMES.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </Select>
        </div>
        <div className="sm:col-span-2">
          <label className="mb-1 block text-xs text-[var(--text-secondary)]">API key (optional)</label>
          <Input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="only if /metrics itself is protected"
          />
          <p className="mt-1 text-xs text-[var(--text-muted)]">
            vLLM's <code>--api-key</code> guards only <code>/v1</code> paths, so a key is usually unnecessary here.
          </p>
        </div>
      </div>

      {duplicatesAgent && (
        <p className="text-xs text-[var(--series-3)]">
          Agents already discover endpoints on their own host automatically. Add one here only if it listens somewhere
          the agent can't reach over loopback — otherwise the same server gets recorded twice.
        </p>
      )}
      {error && <p className="text-xs text-[var(--series-6)]">{error}</p>}

      <Button type="submit" disabled={saving || !url}>
        <Plus size={14} /> Add endpoint
      </Button>
    </form>
  );
}

export function LLMEndpoints() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const [endpoints, setEndpoints] = useState<LLMEndpoint[]>([]);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [targets, setTargets] = useState<LLMTarget[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);

  const reload = useCallback(() => {
    api
      .listLLMEndpoints()
      .then(setEndpoints)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    reload();
    api.listHosts().then(setHosts).catch(() => {});
    api.listLLMTargets().then(setTargets).catch(() => {});
  }, [reload]);

  if (loading) return <div className="text-sm text-[var(--text-muted)]">Loading…</div>;

  const byHost = new Map<string, LLMEndpoint[]>();
  const remote: LLMEndpoint[] = [];
  for (const e of endpoints) {
    if (e.host_id === null) {
      remote.push(e);
      continue;
    }
    const key = e.host_name ?? e.host_id;
    byHost.set(key, [...(byHost.get(key) ?? []), e]);
  }

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold">LLM Endpoints</h1>
        {isAdmin && (
          <Button onClick={() => setShowAdd((v) => !v)}>
            <Plus size={14} /> Add endpoint
          </Button>
        )}
      </div>

      {showAdd && isAdmin && (
        <Card className="mb-4">
          <CardHeader>
            <CardTitle>Add an endpoint</CardTitle>
          </CardHeader>
          <CardContent>
            <AddEndpointForm
              hosts={hosts}
              targets={targets}
              onAdded={() => {
                setShowAdd(false);
                reload();
              }}
            />
          </CardContent>
        </Card>
      )}

      {endpoints.length === 0 && (
        <div className="flex flex-col items-center justify-center py-20 text-center">
          <Boxes size={32} className="mb-3 text-[var(--text-muted)]" />
          <p className="mb-1 font-medium">No endpoints yet</p>
          <p className="max-w-md text-sm text-[var(--text-muted)]">
            Agents discover inference servers on their own hosts automatically — start one and it appears here within a
            minute. Add an endpoint by hand only for machines with no agent.
          </p>
        </div>
      )}

      {[...byHost.entries()].map(([hostName, list]) => (
        <Card key={hostName} className="mb-4">
          <CardHeader>
            <CardTitle>{hostName}</CardTitle>
            {/* "scraped by", not "discovered by": a host group can also hold
                endpoints an operator added against that host by hand. */}
            <span className="text-xs text-[var(--text-muted)]">scraped by agent</span>
          </CardHeader>
          <CardContent>
            {list.map((e) => (
              <EndpointRow key={e.id} endpoint={e} isAdmin={isAdmin} onChange={reload} />
            ))}
          </CardContent>
        </Card>
      ))}

      {remote.length > 0 && (
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <Radio size={14} className="text-[var(--text-muted)]" />
              <CardTitle>Remote (no agent)</CardTitle>
            </div>
            <span className="text-xs text-[var(--text-muted)]">polled by the Sentinel server</span>
          </CardHeader>
          <CardContent>
            {remote.map((e) => (
              <EndpointRow key={e.id} endpoint={e} isAdmin={isAdmin} onChange={reload} />
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
