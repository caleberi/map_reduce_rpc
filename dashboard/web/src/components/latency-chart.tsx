import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, BarChart, Bar, Cell } from "recharts"
import type { TimeSeriesPoint, LatencyDistribution } from "@/types"
import { formatMs } from "@/lib/utils"

export function LatencyChart({ data, distribution }: { data: TimeSeriesPoint[]; distribution: LatencyDistribution }) {
  const lineData = data.map((p) => ({
    time: (p.timestamp_ms / 1000).toFixed(2),
    latency: p.value,
  }))

  const barData = [
    { label: "Min", value: distribution.min, color: "var(--chart-2)" },
    { label: "P50", value: distribution.p50, color: "var(--chart-1)" },
    { label: "P75", value: distribution.p75, color: "var(--chart-4)" },
    { label: "P90", value: distribution.p90, color: "var(--chart-5)" },
    { label: "P95", value: distribution.p95, color: "var(--chart-3)" },
    { label: "P99", value: distribution.p99, color: "var(--destructive)" },
    { label: "Max", value: distribution.max, color: "var(--destructive)" },
  ]

  return (
    <div className="space-y-6">
      {/* Time series */}
      <div className="h-[250px]">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={lineData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
            <XAxis
              dataKey="time"
              tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
              label={{ value: "Time (s)", position: "insideBottomRight", offset: -5, fill: "var(--muted-foreground)", fontSize: 11 }}
            />
            <YAxis
              tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
              label={{ value: "ms", angle: -90, position: "insideLeft", fill: "var(--muted-foreground)", fontSize: 11 }}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "var(--card)",
                border: "1px solid var(--border)",
                borderRadius: "8px",
                fontSize: "12px",
              }}
              formatter={(value) => [formatMs(Number(value)), "Latency"]}
              labelFormatter={(label) => `Time: ${label}s`}
            />
            <Line type="monotone" dataKey="latency" stroke="var(--chart-2)" strokeWidth={2} dot={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>

      {/* Distribution */}
      <div>
        <p className="text-sm font-medium mb-2">Percentile Distribution</p>
        <div className="h-[180px]">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={barData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
              <XAxis dataKey="label" tick={{ fill: "var(--muted-foreground)", fontSize: 11 }} />
              <YAxis tick={{ fill: "var(--muted-foreground)", fontSize: 11 }} />
              <Tooltip
                contentStyle={{
                  backgroundColor: "var(--card)",
                  border: "1px solid var(--border)",
                  borderRadius: "8px",
                  fontSize: "12px",
                }}
                formatter={(value) => [formatMs(Number(value)), "Latency"]}
              />
              <Bar dataKey="value" radius={[4, 4, 0, 0]}>
                {barData.map((entry, index) => (
                  <Cell key={index} fill={entry.color} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      </div>
    </div>
  )
}
