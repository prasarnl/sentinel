import { useEffect, useState, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { Monitor, Cpu, MemoryStick, HardDrive, Server as ServerIcon } from "lucide-react";
import { api, type Host, type LatestSnapshot } from "../lib/api";
import { useHostStream, type IngestEvent } from "../lib/useHostStream";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import { StatusBadge } from "../components/StatusBadge";
import { Sparkline } from "../components/Sparkline";
import { formatBytes, formatPct, formatRelativeTime } from "../lib/format";

const MAX_POINTS = 40;

function HostCard({ host }: { host: Host }) {
  const navigate = useNavigate();
  const [snapshot, setSnapshot] = useState<LatestSnapshot>({});
  const [cpuSeries, setCpuSeries] = useState<{ time: string; value: number }[]>([]);
  const [memSeries, setMemSeries] = useState<{ time: string; value: number }[]>([]);

  useEffect(() => {
    api
      .latestSnapshot(host.id)
      .then(setSnapshot)
      .catch(() => {});
    api
      .historyCPU(host.id, "1h")
      .then((points) =>
        setCpuSeries(points.slice(-MAX_POINTS).map((p) => ({ time: p.time, value: p.usage_pct }))),
      )
      .catch(() => {});
    api
      .historyMem(host.id, "1h")
      .then((points) =>
        setMemSeries(
          points.slice(-MAX_POINTS).map((p) => ({ time: p.time, value: (p.used_bytes / p.total_bytes) * 100 })),
        ),
      )
      .catch(() => {});
  }, [host.id]);

  const onEvent = useCallback((evt: IngestEvent) => {
    const cpuPoint = evt.payload.cpu?.[evt.payload.cpu.length - 1];
    const memPoint = evt.payload.mem?.[evt.payload.mem.length - 1];
    const diskPoints = evt.payload.disk;
    const gpuPoints = evt.payload.gpu;

    setSnapshot((prev) => ({
      cpu: cpuPoint ?? prev.cpu,
      mem: memPoint ?? prev.mem,
      disk: diskPoints ?? prev.disk,
      gpu: gpuPoints ?? prev.gpu,
    }));

    if (cpuPoint) {
      setCpuSeries((prev) => [...prev, { time: cpuPoint.time, value: cpuPoint.usage_pct }].slice(-MAX_POINTS));
    }
    if (memPoint) {
      setMemSeries((prev) =>
        [...prev, { time: memPoint.time, value: (memPoint.used_bytes / memPoint.total_bytes) * 100 }].slice(
          -MAX_POINTS,
        ),
      );
    }
  }, []);

  useHostStream(host.status !== "removed" ? host.id : null, onEvent);

  const primaryDisk = snapshot.disk?.[0];

  return (
    <Card
      className="cursor-pointer transition-shadow hover:shadow-md"
      onClick={() => navigate(`/hosts/${host.id}`)}
    >
      <CardHeader>
        <div className="flex items-center gap-2">
          <Monitor size={16} className="text-[var(--text-muted)]" />
          <CardTitle>{host.name}</CardTitle>
        </div>
        <StatusBadge status={host.status} />
      </CardHeader>
      <CardContent>
        <div className="mb-3 flex items-center justify-between text-xs text-[var(--text-muted)]">
          <span>{host.os}</span>
          <span>{formatRelativeTime(host.last_seen)}</span>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <div className="mb-1 flex items-center gap-1.5 text-xs text-[var(--text-secondary)]">
              <Cpu size={12} /> CPU
              <span className="ml-auto tabular-nums font-medium text-[var(--text-primary)]">
                {formatPct(snapshot.cpu?.usage_pct)}
              </span>
            </div>
            <Sparkline data={cpuSeries} color="var(--series-1)" max={100} />
          </div>
          <div>
            <div className="mb-1 flex items-center gap-1.5 text-xs text-[var(--text-secondary)]">
              <MemoryStick size={12} /> Memory
              <span className="ml-auto tabular-nums font-medium text-[var(--text-primary)]">
                {snapshot.mem ? formatPct((snapshot.mem.used_bytes / snapshot.mem.total_bytes) * 100) : "-"}
              </span>
            </div>
            <Sparkline data={memSeries} color="var(--series-2)" max={100} />
          </div>
        </div>

        <div className="mt-3 flex items-center justify-between border-t border-[var(--border)] pt-3 text-xs text-[var(--text-secondary)]">
          <div className="flex items-center gap-1.5">
            <HardDrive size={12} />
            {primaryDisk ? `${formatBytes(primaryDisk.used_bytes)} / ${formatBytes(primaryDisk.total_bytes)}` : "-"}
          </div>
          {snapshot.gpu && snapshot.gpu.length > 0 && (
            <div className="flex items-center gap-1.5">
              <ServerIcon size={12} />
              GPU {formatPct(snapshot.gpu[0].utilization_pct)}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export function Dashboard() {
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .listHosts()
      .then(setHosts)
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="text-sm text-[var(--text-muted)]">Loading…</div>;

  if (hosts.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-center">
        <ServerIcon size={32} className="mb-3 text-[var(--text-muted)]" />
        <p className="mb-1 font-medium">No hosts yet</p>
        <p className="text-sm text-[var(--text-muted)]">Add a host from the Hosts page to start monitoring it.</p>
      </div>
    );
  }

  return (
    <div>
      <h1 className="mb-4 text-lg font-semibold">Dashboard</h1>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {hosts.map((h) => (
          <HostCard key={h.id} host={h} />
        ))}
      </div>
    </div>
  );
}
