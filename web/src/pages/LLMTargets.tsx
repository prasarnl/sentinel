import { useEffect, useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { Plus, Trash2, KeyRound, Layers } from "lucide-react";
import { api, type LLMTarget } from "../lib/api";
import { useAuth } from "../lib/auth";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

function AddTargetDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("http://");
  const [apiKey, setApiKey] = useState("");
  const [supportsModelSwap, setSupportsModelSwap] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.createLLMTarget(name, baseUrl, apiKey, supportsModelSwap);
      onCreated();
      onClose();
    } catch {
      setError("Failed to create target — name may already be in use");
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>Add LLM target</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col gap-3">
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">Name</label>
              <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="local-llama-cpp" />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">
                Base URL (host:port)
              </label>
              <Input
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
                placeholder="http://10.0.0.5:8080"
              />
              <p className="mt-1 text-xs text-[var(--text-muted)]">
                Must expose an OpenAI-compatible <code>/v1/chat/completions</code> endpoint. Which model(s) to test
                are chosen on the target's page, not here.
              </p>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">
                API key (optional)
              </label>
              <Input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="sent as a Bearer token, leave blank if unauthenticated"
              />
            </div>
            <label className="flex items-start gap-2 text-sm text-[var(--text-secondary)]">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={supportsModelSwap}
                onChange={(e) => setSupportsModelSwap(e.target.checked)}
              />
              <span>
                Supports model swap (e.g. <code>llama-swap</code>)
                <span className="block text-xs text-[var(--text-muted)]">
                  Enables testing multiple models in one run: the server unloads whatever's loaded, benchmarks one
                  model at a time, and unloads it before moving to the next — so VRAM never holds more than one
                  model. Leave unchecked for plain OpenAI-compatible servers (no unload API), which can only test one
                  model per run.
                </span>
              </span>
            </label>
            {error && <div className="text-xs text-[var(--status-critical)]">{error}</div>}
            <div className="mt-2 flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={!name || !baseUrl}>
                Create target
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export function LLMTargets() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const [targets, setTargets] = useState<LLMTarget[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<LLMTarget | null>(null);

  function refresh() {
    api
      .listLLMTargets()
      .then(setTargets)
      .finally(() => setLoading(false));
  }

  useEffect(refresh, []);

  async function onDelete() {
    if (!pendingDelete) return;
    await api.deleteLLMTarget(pendingDelete.id);
    setPendingDelete(null);
    refresh();
  }

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold">LLM Benchmarks</h1>
        {user?.role === "admin" && (
          <Button onClick={() => setShowAdd(true)}>
            <Plus size={16} /> Add target
          </Button>
        )}
      </div>

      <Card>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[var(--border)] text-left text-xs text-[var(--text-muted)]">
              <th className="px-4 py-2.5 font-medium">Name</th>
              <th className="px-4 py-2.5 font-medium">Base URL</th>
              <th className="px-4 py-2.5 font-medium">Multi-model</th>
              <th className="px-4 py-2.5 font-medium">Auth</th>
              {user?.role === "admin" && <th className="px-4 py-2.5" />}
            </tr>
          </thead>
          <tbody>
            {!loading &&
              targets.map((t) => (
                <tr
                  key={t.id}
                  className="cursor-pointer border-b border-[var(--border)] last:border-0 hover:bg-[var(--gridline)]/20"
                  onClick={() => navigate(`/llm-targets/${t.id}`)}
                >
                  <td className="px-4 py-2.5 font-medium">{t.name}</td>
                  <td className="px-4 py-2.5 text-[var(--text-secondary)]">{t.base_url}</td>
                  <td className="px-4 py-2.5 text-[var(--text-secondary)]">
                    {t.supports_model_swap && (
                      <span className="inline-flex items-center gap-1 text-xs">
                        <Layers size={14} className="text-[var(--series-1)]" /> supported
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-[var(--text-secondary)]">
                    {t.has_api_key && <KeyRound size={14} className="inline text-[var(--text-muted)]" />}
                  </td>
                  {user?.role === "admin" && (
                    <td className="px-4 py-2.5 text-right">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          setPendingDelete(t);
                        }}
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
        {!loading && targets.length === 0 && (
          <div className="p-8 text-center text-sm text-[var(--text-muted)]">No LLM targets yet.</div>
        )}
      </Card>

      {showAdd && <AddTargetDialog onClose={() => setShowAdd(false)} onCreated={refresh} />}

      {pendingDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Remove {pendingDelete.name}?</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="mb-4 text-sm text-[var(--text-secondary)]">
                This deletes the target, its saved benchmark config, and all past benchmark run history.
              </p>
              <div className="flex justify-end gap-2">
                <Button variant="secondary" onClick={() => setPendingDelete(null)}>
                  Cancel
                </Button>
                <Button variant="danger" onClick={onDelete}>
                  Remove target
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
