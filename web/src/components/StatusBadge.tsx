import { CheckCircle2, Clock, AlertCircle } from "lucide-react";
import type { HostStatus } from "../lib/api";

const config: Record<HostStatus, { label: string; color: string; Icon: typeof CheckCircle2 }> = {
  online: { label: "Online", color: "var(--status-good)", Icon: CheckCircle2 },
  pending: { label: "Pending", color: "var(--status-warning)", Icon: Clock },
  offline: { label: "Offline", color: "var(--status-critical)", Icon: AlertCircle },
  removed: { label: "Removed", color: "var(--text-muted)", Icon: AlertCircle },
};

export function StatusBadge({ status }: { status: HostStatus }) {
  const { label, color, Icon } = config[status];
  return (
    <span className="inline-flex items-center gap-1.5 text-xs font-medium" style={{ color }}>
      <Icon size={14} strokeWidth={2.5} />
      {label}
    </span>
  );
}
