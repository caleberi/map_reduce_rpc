import type { SimulationResult } from "@/types"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend, RadarChart, PolarGrid, PolarAngleAxis, PolarRadiusAxis, Radar } from "recharts"
import { formatMs, formatMBs } from "@/lib/utils"

const COLORS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
  "var(--destructive)",
  "var(--primary)",
  "var(--accent-foreground)",
]

export function ComparisonChart({ results }: { results: SimulationResult[] }) {
  // Duration comparison
  const durationData = results.map((r) => ({
    plugin: r.plugin,
    map: r.map_phase.duration_ms,
    shuffle: r.shuffle_phase.duration_ms,
    reduce: r.reduce_phase.duration_ms,
    total: r.total_duration_ms,
  }))

  // Throughput comparison
  const throughputData = results.map((r) => ({
    plugin: r.plugin,
    throughput: r.throughput_mb_s,
  }))

  // Radar data: normalize each metric to 0-100
  const maxDuration = Math.max(...results.map((r) => r.total_duration_ms))
  const maxThroughput = Math.max(...results.map((r) => r.throughput_mb_s))
  const maxLatency = Math.max(...results.map((r) => r.latency.p99))

  const radarData = [
    {
      metric: "Speed",
      ...Object.fromEntries(results.map((r) => [r.plugin, Math.round((1 - r.total_duration_ms / maxDuration) * 100 + 20)])),
    },
    {
      metric: "Throughput",
      ...Object.fromEntries(results.map((r) => [r.plugin, Math.round((r.throughput_mb_s / maxThroughput) * 100)])),
    },
    {
      metric: "Low Latency",
      ...Object.fromEntries(results.map((r) => [r.plugin, Math.round((1 - r.latency.avg / maxLatency) * 100 + 10)])),
    },
    {
      metric: "Consistency",
      ...Object.fromEntries(results.map((r) => [r.plugin, Math.round((1 - (r.latency.p99 - r.latency.p50) / maxLatency) * 100)])),
    },
    {
      metric: "Reliability",
      ...Object.fromEntries(results.map((r) => [r.plugin, r.total_crashes === 0 ? 100 : Math.max(10, 100 - r.total_crashes * 20)])),
    },
  ]

  return (
    <div className="grid gap-4 md:grid-cols-2">
      {/* Duration bars */}
      <Card>
        <CardHeader>
          <CardTitle>Phase Duration</CardTitle>
          <CardDescription>Stacked phase durations per plugin</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="h-[300px]">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={durationData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="plugin" tick={{ fill: "var(--muted-foreground)", fontSize: 11 }} />
                <YAxis tick={{ fill: "var(--muted-foreground)", fontSize: 11 }} label={{ value: "ms", angle: -90, position: "insideLeft", fill: "var(--muted-foreground)", fontSize: 11 }} />
                <Tooltip
                  contentStyle={{ backgroundColor: "var(--card)", border: "1px solid var(--border)", borderRadius: "8px", fontSize: "12px" }}
                  formatter={(value, name) => [formatMs(Number(value)), String(name).charAt(0).toUpperCase() + String(name).slice(1)]}
                />
                <Legend />
                <Bar dataKey="map" stackId="a" fill="var(--chart-1)" radius={[0, 0, 0, 0]} />
                <Bar dataKey="shuffle" stackId="a" fill="var(--chart-2)" />
                <Bar dataKey="reduce" stackId="a" fill="var(--chart-4)" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      {/* Throughput bars */}
      <Card>
        <CardHeader>
          <CardTitle>Throughput</CardTitle>
          <CardDescription>End-to-end data processing rate</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="h-[300px]">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={throughputData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="plugin" tick={{ fill: "var(--muted-foreground)", fontSize: 11 }} />
                <YAxis tick={{ fill: "var(--muted-foreground)", fontSize: 11 }} label={{ value: "MB/s", angle: -90, position: "insideLeft", fill: "var(--muted-foreground)", fontSize: 11 }} />
                <Tooltip
                  contentStyle={{ backgroundColor: "var(--card)", border: "1px solid var(--border)", borderRadius: "8px", fontSize: "12px" }}
                  formatter={(value) => [formatMBs(Number(value)), "Throughput"]}
                />
                <Bar dataKey="throughput" radius={[4, 4, 0, 0]}>
                  {throughputData.map((_, idx) => (
                    <Bar key={idx} dataKey="throughput" fill={COLORS[idx % COLORS.length]} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      {/* Radar chart */}
      <Card className="md:col-span-2">
        <CardHeader>
          <CardTitle>Performance Profile</CardTitle>
          <CardDescription>Multi-dimensional performance comparison</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="h-[400px]">
            <ResponsiveContainer width="100%" height="100%">
              <RadarChart data={radarData}>
                <PolarGrid className="stroke-border" />
                <PolarAngleAxis dataKey="metric" tick={{ fill: "var(--muted-foreground)", fontSize: 12 }} />
                <PolarRadiusAxis angle={30} domain={[0, 100]} tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} />
                {results.map((r, idx) => (
                  <Radar
                    key={r.plugin}
                    name={r.plugin}
                    dataKey={r.plugin}
                    stroke={COLORS[idx % COLORS.length]}
                    fill={COLORS[idx % COLORS.length]}
                    fillOpacity={0.15}
                    strokeWidth={2}
                  />
                ))}
                <Legend />
                <Tooltip />
              </RadarChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
