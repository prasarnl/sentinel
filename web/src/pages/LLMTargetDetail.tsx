import { useEffect, useState, useCallback, Fragment, type FormEvent } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ArrowLeft, Play, RotateCcw, Plus, ChevronDown, ChevronRight, Trash2 } from "lucide-react";
import {
  api,
  type LLMTarget,
  type LLMBenchmarkConfig,
  type LLMBenchmarkRun,
  type BenchmarkProgressEvent,
  type BenchmarkRunStatus,
} from "../lib/api";
import { useAuth } from "../lib/auth";
import { useBenchmarkStream } from "../lib/useBenchmarkStream";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { formatRelativeTime } from "../lib/format";

type ConfigForm = Omit<LLMBenchmarkConfig, "target_id" | "updated_at">;

const DEFAULTS: ConfigForm = {
  concurrency: 1,
  num_requests: 10,
  warmup_requests: 1,
  prompt_tokens: 512,
  max_tokens: 128,
  request_timeout_secs: 120,
  model_load_timeout_secs: 180,
  context_window: null,
  batch_size: null,
};

const runStatusColor: Record<BenchmarkRunStatus, string> = {
  running: "var(--status-warning)",
  completed: "var(--status-good)",
  failed: "var(--status-critical)",
  cancelled: "var(--text-muted)",
};

function RunStatusBadge({ status }: { status: BenchmarkRunStatus }) {
  return (
    <span className="text-xs font-medium capitalize" style={{ color: runStatusColor[status] }}>
      {status}
    </span>
  );
}

function Field({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
  disabled?: boolean;
}) {
  return (
    <div>
      <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">{label}</label>
      <Input
        type="number"
        min={0}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(Number(e.target.value))}
      />
    </div>
  );
}

function OptionalField({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string;
  value: number | null | undefined;
  onChange: (v: number | null) => void;
  disabled?: boolean;
}) {
  return (
    <div>
      <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">{label}</label>
      <Input
        type="number"
        min={1}
        placeholder="target default"
        value={value ?? ""}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value === "" ? null : Number(e.target.value))}
      />
    </div>
  );
}

const stageLabel: Record<BenchmarkProgressEvent["stage"], string> = {
  unloading: "Unloading previous model(s)…",
  loading: "Loading",
  benchmarking: "Benchmarking",
  model_done: "Finished",
  done: "Done",
};

export function LLMTargetDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const [target, setTarget] = useState<LLMTarget | null>(null);
  const [form, setForm] = useState<ConfigForm>(DEFAULTS);
  const [savingConfig, setSavingConfig] = useState(false);
  const [runs, setRuns] = useState<LLMBenchmarkRun[]>([]);

  const [discoveredModels, setDiscoveredModels] = useState<string[]>([]);
  const [manualModels, setManualModels] = useState<string[]>([]);
  const [modelsError, setModelsError] = useState<string | null>(null);
  const [manualModelInput, setManualModelInput] = useState("");
  const [selectedModels, setSelectedModels] = useState<string[]>([]);

  const [batchId, setBatchId] = useState<string | null>(null);
  const [progress, setProgress] = useState<BenchmarkProgressEvent | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null);
  const [pendingDeleteRun, setPendingDeleteRun] = useState<LLMBenchmarkRun | null>(null);

  const refreshRuns = useCallback(() => {
    if (!id) return;
    api.listBenchmarkRuns(id).then(setRuns);
  }, [id]);

  useEffect(() => {
    if (!id) return;
    api.getLLMTarget(id).then(({ target: t, config: c }) => {
      setTarget(t);
      setForm({
        concurrency: c.concurrency,
        num_requests: c.num_requests,
        warmup_requests: c.warmup_requests,
        prompt_tokens: c.prompt_tokens,
        max_tokens: c.max_tokens,
        request_timeout_secs: c.request_timeout_secs,
        model_load_timeout_secs: c.model_load_timeout_secs,
        context_window: c.context_window ?? null,
        batch_size: c.batch_size ?? null,
      });
    });
    api
      .getLLMTargetModels(id)
      .then(({ models }) => setDiscoveredModels(models))
      .catch(() => setModelsError("Could not list models from this target — add one manually below."));
    refreshRuns();
  }, [id, refreshRuns]);

  const onProgress = useCallback(
    (evt: BenchmarkProgressEvent) => {
      setProgress(evt);
      if (evt.stage === "model_done" || evt.done) refreshRuns();
      if (evt.done) setBatchId(null);
    },
    [refreshRuns],
  );

  useBenchmarkStream(id ?? null, batchId, onProgress);

  const allModels = [...new Set([...discoveredModels, ...manualModels])].sort();
  const multiSelect = target?.supports_model_swap ?? false;

  function toggleModel(m: string) {
    if (multiSelect) {
      setSelectedModels((prev) => (prev.includes(m) ? prev.filter((x) => x !== m) : [...prev, m]));
    } else {
      setSelectedModels([m]);
    }
  }

  function onAddManualModel(e: FormEvent) {
    e.preventDefault();
    const m = manualModelInput.trim();
    if (!m) return;
    if (!allModels.includes(m)) setManualModels((prev) => [...prev, m]);
    if (multiSelect) {
      setSelectedModels((prev) => (prev.includes(m) ? prev : [...prev, m]));
    } else {
      setSelectedModels([m]);
    }
    setManualModelInput("");
  }

  async function onSaveConfig() {
    if (!id) return;
    setSavingConfig(true);
    setError(null);
    try {
      await api.updateLLMBenchmarkConfig(id, form);
    } catch {
      setError("Failed to save benchmark config");
    } finally {
      setSavingConfig(false);
    }
  }

  async function onRun() {
    if (!id || selectedModels.length === 0) return;
    setError(null);
    try {
      await api.updateLLMBenchmarkConfig(id, form);
      const { batch_id } = await api.runBenchmark(id, selectedModels);
      setProgress(null);
      setBatchId(batch_id);
    } catch {
      setError("Failed to start benchmark — check the target is reachable");
    }
  }

  async function onDeleteRun() {
    if (!id || !pendingDeleteRun) return;
    await api.deleteBenchmarkRun(id, pendingDeleteRun.id);
    if (expandedRunId === pendingDeleteRun.id) setExpandedRunId(null);
    setPendingDeleteRun(null);
    refreshRuns();
  }

  if (!target) return <div className="text-sm text-[var(--text-muted)]">Loading…</div>;

  return (
    <div>
      <button
        onClick={() => navigate("/llm-targets")}
        className="mb-4 flex items-center gap-1.5 text-sm text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
      >
        <ArrowLeft size={14} /> Back to targets
      </button>

      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">{target.name}</h1>
          <p className="text-xs text-[var(--text-muted)]">
            {target.base_url}
            {target.supports_model_swap ? " · multi-model" : ""}
          </p>
        </div>
        {isAdmin && (
          <Button onClick={onRun} disabled={!!batchId || selectedModels.length === 0}>
            <Play size={16} /> {batchId ? "Running…" : "Run benchmark"}
          </Button>
        )}
      </div>

      {error && <div className="mb-4 text-sm text-[var(--status-critical)]">{error}</div>}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Benchmark parameters</CardTitle>
            {isAdmin && (
              <button
                onClick={() => setForm(DEFAULTS)}
                className="flex items-center gap-1 text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)]"
              >
                <RotateCcw size={12} /> Reset to defaults
              </button>
            )}
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-3">
              <Field
                label="Concurrency"
                value={form.concurrency}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, concurrency: v }))}
              />
              <Field
                label="Requests"
                value={form.num_requests}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, num_requests: v }))}
              />
              <Field
                label="Warmup requests"
                value={form.warmup_requests}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, warmup_requests: v }))}
              />
              <Field
                label="Prompt tokens"
                value={form.prompt_tokens}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, prompt_tokens: v }))}
              />
              <Field
                label="Max tokens"
                value={form.max_tokens}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, max_tokens: v }))}
              />
              <Field
                label="Timeout (secs)"
                value={form.request_timeout_secs}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, request_timeout_secs: v }))}
              />
              <Field
                label="Model load timeout (secs)"
                value={form.model_load_timeout_secs}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, model_load_timeout_secs: v }))}
              />
              <OptionalField
                label="Context window (n_ctx)"
                value={form.context_window}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, context_window: v }))}
              />
              <OptionalField
                label="Batch size (n_batch)"
                value={form.batch_size}
                disabled={!isAdmin}
                onChange={(v) => setForm((f) => ({ ...f, batch_size: v }))}
              />
            </div>
            <p className="mt-2 text-xs text-[var(--text-muted)]">
              Context window and batch size are sent as best-effort overrides on each request — support depends on
              the target's backend. Leave blank to use the target's default.
            </p>
            {isAdmin && (
              <div className="mt-3 flex justify-end">
                <Button variant="secondary" onClick={onSaveConfig} disabled={savingConfig}>
                  Save parameters
                </Button>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{multiSelect ? "Models to test" : "Model to test"}</CardTitle>
          </CardHeader>
          <CardContent>
            {modelsError && <p className="mb-2 text-xs text-[var(--status-critical)]">{modelsError}</p>}
            {allModels.length === 0 && !modelsError && (
              <p className="mb-2 text-xs text-[var(--text-muted)]">Loading models…</p>
            )}
            <div className="flex max-h-48 flex-col gap-1.5 overflow-y-auto">
              {allModels.map((m) => (
                <label key={m} className="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
                  <input
                    type={multiSelect ? "checkbox" : "radio"}
                    name="model-select"
                    checked={selectedModels.includes(m)}
                    onChange={() => toggleModel(m)}
                    disabled={!isAdmin}
                  />
                  {m}
                </label>
              ))}
            </div>
            {isAdmin && (
              <form onSubmit={onAddManualModel} className="mt-3 flex gap-2">
                <Input
                  value={manualModelInput}
                  onChange={(e) => setManualModelInput(e.target.value)}
                  placeholder="add a model name manually"
                  className="flex-1"
                />
                <Button type="submit" variant="secondary" disabled={!manualModelInput.trim()}>
                  <Plus size={14} />
                </Button>
              </form>
            )}
            {!multiSelect && (
              <p className="mt-2 text-xs text-[var(--text-muted)]">
                This target doesn't support model swap, so only one model can be tested per run.
              </p>
            )}
          </CardContent>
        </Card>
      </div>

      {batchId && (
        <Card className="mt-4">
          <CardHeader>
            <CardTitle>Run in progress</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-col gap-2">
              <p className="text-sm text-[var(--text-primary)]">
                {stageLabel[progress?.stage ?? "loading"]}
                {progress?.model ? ` ${progress.model}` : ""}
                {progress?.models_total ? ` (model ${progress.model_index} of ${progress.models_total})` : ""}
              </p>
              {progress?.stage === "benchmarking" && (
                <>
                  <div className="h-2 w-full overflow-hidden rounded-full bg-[var(--gridline)]/40">
                    <div
                      className="h-full bg-[var(--series-1)] transition-all"
                      style={{
                        width: progress.total ? `${(100 * progress.completed) / progress.total}%` : "0%",
                      }}
                    />
                  </div>
                  <p className="text-xs text-[var(--text-muted)]">
                    {progress.completed} / {progress.total} requests
                    {progress.failed ? ` · ${progress.failed} failed` : ""}
                  </p>
                  {progress.last_tokens_per_sec != null && (
                    <p className="text-xs text-[var(--text-muted)]">
                      last: {progress.last_tokens_per_sec.toFixed(1)} tok/s, TTFT {progress.last_ttft_ms?.toFixed(0)}
                      ms
                    </p>
                  )}
                  {progress.last_error && (
                    <p className="text-xs text-[var(--status-critical)]">Last request failed: {progress.last_error}</p>
                  )}
                </>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      <Card className="mt-4">
        <CardHeader>
          <CardTitle>Run history</CardTitle>
        </CardHeader>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[var(--border)] text-left text-xs text-[var(--text-muted)]">
              <th className="w-6 px-4 py-2.5" />
              <th className="px-4 py-2.5 font-medium">Started</th>
              <th className="px-4 py-2.5 font-medium">Model</th>
              <th className="px-4 py-2.5 font-medium">Status</th>
              <th className="px-4 py-2.5 font-medium">Throughput</th>
              <th className="px-4 py-2.5 font-medium">TTFT (p50)</th>
              <th className="px-4 py-2.5 font-medium">Requests</th>
              {isAdmin && <th className="px-4 py-2.5" />}
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => {
              const errors = r.error ? [r.error] : r.summary?.errors ?? [];
              const hasDetail = errors.length > 0;
              const isExpanded = expandedRunId === r.id;
              return (
                <Fragment key={r.id}>
                  <tr
                    className={`border-b border-[var(--border)] last:border-0 ${hasDetail ? "cursor-pointer hover:bg-[var(--gridline)]/20" : ""}`}
                    onClick={() => hasDetail && setExpandedRunId(isExpanded ? null : r.id)}
                  >
                    <td className="px-4 py-2.5 text-[var(--text-muted)]">
                      {hasDetail &&
                        (isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />)}
                    </td>
                    <td className="px-4 py-2.5 text-[var(--text-secondary)]">{formatRelativeTime(r.started_at)}</td>
                    <td className="px-4 py-2.5 font-medium">{r.model}</td>
                    <td className="px-4 py-2.5">
                      <RunStatusBadge status={r.status} />
                    </td>
                    <td className="px-4 py-2.5 tabular-nums">
                      {r.summary ? `${r.summary.throughput_tokens_per_sec.toFixed(1)} tok/s` : "-"}
                    </td>
                    <td className="px-4 py-2.5 tabular-nums">
                      {r.summary ? `${r.summary.ttft_ms.p50.toFixed(0)}ms` : "-"}
                    </td>
                    <td className="px-4 py-2.5 text-[var(--text-secondary)]">
                      {r.summary
                        ? `${r.summary.requests}${r.summary.failed ? ` (+${r.summary.failed} failed)` : ""}`
                        : "-"}
                    </td>
                    {isAdmin && (
                      <td className="px-4 py-2.5 text-right">
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setPendingDeleteRun(r);
                          }}
                          className="text-[var(--text-muted)] hover:text-[var(--status-critical)]"
                        >
                          <Trash2 size={14} />
                        </button>
                      </td>
                    )}
                  </tr>
                  {isExpanded && (
                    <tr className="border-b border-[var(--border)] bg-[var(--page-plane)] last:border-0">
                      <td />
                      <td colSpan={isAdmin ? 7 : 6} className="px-4 py-3">
                        <p className="mb-1 text-xs font-medium text-[var(--text-secondary)]">
                          {r.status === "failed" ? "Failure reason" : `Failed request${errors.length === 1 ? "" : "s"} (sample)`}
                        </p>
                        <ul className="flex flex-col gap-1">
                          {errors.map((e, i) => (
                            <li key={i} className="break-all font-mono text-xs text-[var(--status-critical)]">
                              {e}
                            </li>
                          ))}
                        </ul>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
        {runs.length === 0 && (
          <div className="p-8 text-center text-sm text-[var(--text-muted)]">No benchmark runs yet.</div>
        )}
      </Card>

      {pendingDeleteRun && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Delete this run?</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="mb-4 text-sm text-[var(--text-secondary)]">
                Removes the {formatRelativeTime(pendingDeleteRun.started_at)} run of{" "}
                <span className="font-medium text-[var(--text-primary)]">{pendingDeleteRun.model}</span> from
                history. This can't be undone.
              </p>
              <div className="flex justify-end gap-2">
                <Button variant="secondary" onClick={() => setPendingDeleteRun(null)}>
                  Cancel
                </Button>
                <Button variant="danger" onClick={onDeleteRun}>
                  Delete run
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
