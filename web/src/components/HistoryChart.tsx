import { LineChart, Line, CartesianGrid, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";
import { formatClockTime } from "../lib/format";

export interface ChartSeries {
  key: string;
  label: string;
  color: string;
}

interface HistoryChartProps {
  data: Record<string, unknown>[];
  series: ChartSeries[];
  yFormatter?: (v: number) => string;
  height?: number;
}

function CustomTooltip({
  active,
  payload,
  label,
  series,
  yFormatter,
}: {
  active?: boolean;
  payload?: { value: number; dataKey: string }[];
  label?: string;
  series: ChartSeries[];
  yFormatter: (v: number) => string;
}) {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--surface-1)] px-3 py-2 text-xs shadow-md">
      <div className="mb-1 text-[var(--text-muted)]">{label ? formatClockTime(label) : ""}</div>
      {payload.map((p) => {
        const s = series.find((s) => s.key === p.dataKey);
        if (!s) return null;
        return (
          <div key={p.dataKey} className="flex items-center gap-1.5 text-[var(--text-primary)]">
            <span className="inline-block h-2 w-2 rounded-full" style={{ background: s.color }} />
            <span className="text-[var(--text-secondary)]">{s.label}:</span>
            <span className="tabular-nums font-medium">{yFormatter(p.value)}</span>
          </div>
        );
      })}
    </div>
  );
}

export function HistoryChart({ data, series, yFormatter = (v) => `${v}`, height = 220 }: HistoryChartProps) {
  return (
    <div>
      {series.length > 1 && (
        <div className="mb-2 flex gap-4">
          {series.map((s) => (
            <div key={s.key} className="flex items-center gap-1.5 text-xs text-[var(--text-secondary)]">
              <span className="inline-block h-2 w-2 rounded-full" style={{ background: s.color }} />
              {s.label}
            </div>
          ))}
        </div>
      )}
      <ResponsiveContainer width="100%" height={height}>
        <LineChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="var(--gridline)" vertical={false} />
          <XAxis
            dataKey="time"
            tickFormatter={formatClockTime}
            stroke="var(--baseline)"
            tick={{ fill: "var(--text-muted)", fontSize: 11 }}
            tickLine={false}
            axisLine={{ stroke: "var(--baseline)" }}
            minTickGap={40}
          />
          <YAxis
            tickFormatter={yFormatter}
            stroke="var(--baseline)"
            tick={{ fill: "var(--text-muted)", fontSize: 11 }}
            tickLine={false}
            axisLine={false}
            width={56}
          />
          <Tooltip
            content={<CustomTooltip series={series} yFormatter={yFormatter} />}
            cursor={{ stroke: "var(--baseline)", strokeWidth: 1 }}
          />
          {series.map((s) => (
            <Line
              key={s.key}
              type="monotone"
              dataKey={s.key}
              stroke={s.color}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
              connectNulls
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
