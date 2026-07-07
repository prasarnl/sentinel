import { useEffect, useState, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { api, type Host, type LatestSnapshot } from "../lib/api";
import { useHostStream, type IngestEvent } from "../lib/useHostStream";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { Select } from "../components/ui/Select";
import { StatusBadge } from "../components/StatusBadge";
import { HistoryChart } from "../components/HistoryChart";
import { formatBytes, formatBytesPerSec, formatPct, formatRelativeTime } from "../lib/format";

const RANGES = [
  { value: "1h", label: "Last hour" },
  { value: "6h", label: "Last 6 hours" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
];

export function HostDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [host, setHost] = useState<Host | null>(null);
  const [range, setRange] = useState("1h");
  const [snapshot, setSnapshot] = useState<LatestSnapshot>({});
  const [cpuData, setCpuData] = useState<Record<string, unknown>[]>([]);
  const [memData, setMemData] = useState<Record<string, unknown>[]>([]);
  const [diskData, setDiskData] = useState<Record<string, unknown>[]>([]);
  const [selectedMount, setSelectedMount] = useState<string>("");
  const [gpuData, setGpuData] = useState<Record<string, unknown>[]>([]);

  useEffect(() => {
    api.listHosts().then((hosts) => setHost(hosts.find((h) => h.id === id) ?? null));
  }, [id]);

  useEffect(() => {
    if (!id) return;
    api.latestSnapshot(id).then(setSnapshot).catch(() => {});
  }, [id]);

  useEffect(() => {
    if (!id) return;
    api
      .historyCPU(id, range)
      .then((points) => setCpuData(points.map((p) => ({ time: p.time, usage_pct: p.usage_pct }))));
    api
      .historyMem(id, range)
      .then((points) =>
        setMemData(
          points.map((p) => ({ time: p.time, used_pct: (p.used_bytes / p.total_bytes) * 100 })),
        ),
      );
  }, [id, range]);

  useEffect(() => {
    if (!id || !snapshot.disk?.length) return;
    if (!selectedMount) {
      setSelectedMount(snapshot.disk[0].mount);
      return;
    }
    api
      .historyDisk(id, selectedMount, range)
      .then((points) =>
        setDiskData(points.map((p) => ({ time: p.time, read: p.read_bytes_sec, write: p.write_bytes_sec }))),
      );
  }, [id, range, selectedMount, snapshot.disk]);

  useEffect(() => {
    if (!id || !snapshot.gpu?.length) return;
    api
      .historyGPU(id, snapshot.gpu[0].gpu_index, range)
      .then((points) =>
        setGpuData(points.map((p) => ({ time: p.time, utilization_pct: p.utilization_pct ?? 0 }))),
      );
  }, [id, range, snapshot.gpu]);

  const onEvent = useCallback((evt: IngestEvent) => {
    const cpuPoint = evt.payload.cpu?.[evt.payload.cpu.length - 1];
    const memPoint = evt.payload.mem?.[evt.payload.mem.length - 1];
    setSnapshot((prev) => ({
      cpu: cpuPoint ?? prev.cpu,
      mem: memPoint ?? prev.mem,
      disk: evt.payload.disk ?? prev.disk,
      gpu: evt.payload.gpu ?? prev.gpu,
    }));
    if (cpuPoint) {
      setCpuData((prev) => [...prev, { time: cpuPoint.time, usage_pct: cpuPoint.usage_pct }].slice(-500));
    }
    if (memPoint) {
      setMemData((prev) =>
        [...prev, { time: memPoint.time, used_pct: (memPoint.used_bytes / memPoint.total_bytes) * 100 }].slice(-500),
      );
    }
  }, []);

  useHostStream(id ?? null, onEvent);

  if (!host) return <div className="text-sm text-[var(--text-muted)]">Loading…</div>;

  return (
    <div>
      <button
        onClick={() => navigate("/")}
        className="mb-4 flex items-center gap-1.5 text-sm text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
      >
        <ArrowLeft size={14} /> Back to dashboard
      </button>

      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-lg font-semibold">
            {host.name}
            <StatusBadge status={host.status} />
          </h1>
          <p className="text-xs text-[var(--text-muted)]">
            {host.os} · last seen {formatRelativeTime(host.last_seen)}
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

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>CPU usage</CardTitle>
            <span className="tabular-nums text-sm font-medium">{formatPct(snapshot.cpu?.usage_pct)}</span>
          </CardHeader>
          <CardContent>
            <HistoryChart
              data={cpuData}
              series={[{ key: "usage_pct", label: "CPU %", color: "var(--series-1)" }]}
              yFormatter={(v) => `${v}%`}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Memory usage</CardTitle>
            <span className="tabular-nums text-sm font-medium">
              {snapshot.mem ? formatBytes(snapshot.mem.used_bytes) + " / " + formatBytes(snapshot.mem.total_bytes) : "-"}
            </span>
          </CardHeader>
          <CardContent>
            <HistoryChart
              data={memData}
              series={[{ key: "used_pct", label: "Memory %", color: "var(--series-2)" }]}
              yFormatter={(v) => `${v}%`}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Disk I/O</CardTitle>
            {snapshot.disk && snapshot.disk.length > 1 && (
              <Select value={selectedMount} onChange={(e) => setSelectedMount(e.target.value)}>
                {snapshot.disk.map((d) => (
                  <option key={d.mount} value={d.mount}>
                    {d.mount}
                  </option>
                ))}
              </Select>
            )}
          </CardHeader>
          <CardContent>
            <HistoryChart
              data={diskData}
              series={[
                { key: "read", label: "Read", color: "var(--series-1)" },
                { key: "write", label: "Write", color: "var(--series-8)" },
              ]}
              yFormatter={(v) => formatBytesPerSec(v)}
            />
            {snapshot.disk?.find((d) => d.mount === selectedMount) && (
              <p className="mt-2 text-xs text-[var(--text-muted)]">
                {formatBytes(snapshot.disk.find((d) => d.mount === selectedMount)!.used_bytes)} /{" "}
                {formatBytes(snapshot.disk.find((d) => d.mount === selectedMount)!.total_bytes)} used
              </p>
            )}
          </CardContent>
        </Card>

        {snapshot.gpu && snapshot.gpu.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>GPU utilization — {snapshot.gpu[0].name || snapshot.gpu[0].vendor}</CardTitle>
              <span className="tabular-nums text-sm font-medium">{formatPct(snapshot.gpu[0].utilization_pct)}</span>
            </CardHeader>
            <CardContent>
              <HistoryChart
                data={gpuData}
                series={[{ key: "utilization_pct", label: "GPU %", color: "var(--series-5)" }]}
                yFormatter={(v) => `${v}%`}
              />
              {snapshot.gpu[0].temp_c != null && (
                <p className="mt-2 text-xs text-[var(--text-muted)]">Temp: {snapshot.gpu[0].temp_c.toFixed(0)}°C</p>
              )}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}
