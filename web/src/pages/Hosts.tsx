import { useEffect, useState, type FormEvent } from "react";
import { Plus, Trash2, Copy, Check } from "lucide-react";
import { api, type Host, type HostOS, type CreateHostResponse } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { Select } from "../components/ui/Select";
import { StatusBadge } from "../components/StatusBadge";
import { formatRelativeTime } from "../lib/format";

function AddHostDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (h: Host) => void }) {
  const [name, setName] = useState("");
  const [os, setOs] = useState<HostOS>("linux");
  const [result, setResult] = useState<CreateHostResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const res = await api.createHost(name, os, []);
      setResult(res);
      onCreated(res.host);
    } catch {
      setError("Failed to create host — name may already be in use");
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>{result ? "Install the agent" : "Add host"}</CardTitle>
        </CardHeader>
        <CardContent>
          {!result ? (
            <form onSubmit={onSubmit} className="flex flex-col gap-3">
              <div>
                <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">Host name</label>
                <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="db-prod-01" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">Operating system</label>
                <Select value={os} onChange={(e) => setOs(e.target.value as HostOS)} className="w-full">
                  <option value="linux">Linux</option>
                  <option value="windows">Windows</option>
                </Select>
              </div>
              {error && <div className="text-xs text-[var(--status-critical)]">{error}</div>}
              <div className="mt-2 flex justify-end gap-2">
                <Button type="button" variant="secondary" onClick={onClose}>
                  Cancel
                </Button>
                <Button type="submit" disabled={!name}>
                  Create host
                </Button>
              </div>
            </form>
          ) : (
            <div className="flex flex-col gap-3">
              <p className="text-sm text-[var(--text-secondary)]">
                Run this command on <span className="font-medium text-[var(--text-primary)]">{result.host.name}</span>{" "}
                to install and enroll the agent. The enrollment token expires in 24 hours.
              </p>
              <div className="relative">
                <pre className="overflow-x-auto rounded-md border border-[var(--border)] bg-[var(--page-plane)] p-3 pr-10 text-xs">
                  {result.install_command}
                </pre>
                <button
                  onClick={() => {
                    navigator.clipboard.writeText(result.install_command);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                  }}
                  className="absolute right-2 top-2 rounded p-1 text-[var(--text-muted)] hover:text-[var(--text-primary)]"
                >
                  {copied ? <Check size={14} /> : <Copy size={14} />}
                </button>
              </div>
              <div className="flex justify-end">
                <Button onClick={onClose}>Done</Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export function Hosts() {
  const { user } = useAuth();
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Host | null>(null);
  const [purge, setPurge] = useState(false);

  function refresh() {
    api
      .listHosts()
      .then(setHosts)
      .finally(() => setLoading(false));
  }

  useEffect(refresh, []);

  async function onDelete() {
    if (!pendingDelete) return;
    await api.deleteHost(pendingDelete.id, purge);
    setPendingDelete(null);
    setPurge(false);
    refresh();
  }

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold">Hosts</h1>
        {user?.role === "admin" && (
          <Button onClick={() => setShowAdd(true)}>
            <Plus size={16} /> Add host
          </Button>
        )}
      </div>

      <Card>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[var(--border)] text-left text-xs text-[var(--text-muted)]">
              <th className="px-4 py-2.5 font-medium">Name</th>
              <th className="px-4 py-2.5 font-medium">OS</th>
              <th className="px-4 py-2.5 font-medium">Status</th>
              <th className="px-4 py-2.5 font-medium">Last seen</th>
              <th className="px-4 py-2.5 font-medium">Tags</th>
              {user?.role === "admin" && <th className="px-4 py-2.5" />}
            </tr>
          </thead>
          <tbody>
            {!loading &&
              hosts.map((h) => (
                <tr key={h.id} className="border-b border-[var(--border)] last:border-0">
                  <td className="px-4 py-2.5 font-medium">{h.name}</td>
                  <td className="px-4 py-2.5 text-[var(--text-secondary)]">{h.os}</td>
                  <td className="px-4 py-2.5">
                    <StatusBadge status={h.status} />
                  </td>
                  <td className="px-4 py-2.5 text-[var(--text-secondary)]">{formatRelativeTime(h.last_seen)}</td>
                  <td className="px-4 py-2.5 text-[var(--text-secondary)]">{h.tags.join(", ") || "-"}</td>
                  {user?.role === "admin" && (
                    <td className="px-4 py-2.5 text-right">
                      <button
                        onClick={() => setPendingDelete(h)}
                        className="text-[var(--text-muted)] hover:text-[var(--status-critical)]"
                      >
                        <Trash2 size={14} />
                      </button>
                    </td>
                  )}
                </tr>
              ))}
          </tbody>
        </table>
        {!loading && hosts.length === 0 && (
          <div className="p-8 text-center text-sm text-[var(--text-muted)]">No hosts yet.</div>
        )}
      </Card>

      {showAdd && <AddHostDialog onClose={() => { setShowAdd(false); refresh(); }} onCreated={refresh} />}

      {pendingDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Remove {pendingDelete.name}?</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="mb-3 text-sm text-[var(--text-secondary)]">
                The agent's API key will be revoked immediately. It will no longer be able to report metrics.
              </p>
              <label className="mb-4 flex items-center gap-2 text-sm text-[var(--text-secondary)]">
                <input type="checkbox" checked={purge} onChange={(e) => setPurge(e.target.checked)} />
                Also delete its historical metrics
              </label>
              <div className="flex justify-end gap-2">
                <Button variant="secondary" onClick={() => setPendingDelete(null)}>
                  Cancel
                </Button>
                <Button variant="danger" onClick={onDelete}>
                  Remove host
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
