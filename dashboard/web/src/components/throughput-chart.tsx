import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts"
import type { TimeSeriesPoint } from "@/types"

export function ThroughputChart({ data }: { data: TimeSeriesPoint[] }) {
  const chartData = data.map((p) => ({
    time: (p.timestamp_ms / 1000).toFixed(2),
    throughput: p.value,
  }))

  return (
    <div className="h-[350px]">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={chartData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
          <defs>
            <linearGradient id="throughputGradient" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="var(--chart-1)" stopOpacity={0.3} />
              <stop offset="95%" stopColor="var(--chart-1)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
          <XAxis
            dataKey="time"
            className="text-xs"
            tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
            label={{ value: "Time (s)", position: "insideBottomRight", offset: -5, fill: "var(--muted-foreground)", fontSize: 11 }}
          />
          <YAxis
            className="text-xs"
            tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
            label={{ value: "MB/s", angle: -90, position: "insideLeft", fill: "var(--muted-foreground)", fontSize: 11 }}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "var(--card)",
              border: "1px solid var(--border)",
              borderRadius: "8px",
              fontSize: "12px",
            }}
            formatter={(value) => [`${Number(value).toFixed(2)} MB/s`, "Throughput"]}
            labelFormatter={(label) => `Time: ${label}s`}
          />
          <Area
            type="monotone"
            dataKey="throughput"
            stroke="var(--chart-1)"
            fill="url(#throughputGradient)"
            strokeWidth={2}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
