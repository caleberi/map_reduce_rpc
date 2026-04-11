import type { SimulationResult } from "@/types"
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell } from "recharts"
import { formatMs, formatMBs } from "@/lib/utils"

export function PhaseBreakdown({ result }: { result: SimulationResult }) {
  const phases = [
    {
      name: "Map",
      duration: result.map_phase.duration_ms,
      throughput: result.map_phase.throughput_mb_s,
      color: "var(--chart-1)",
    },
    {
      name: "Shuffle",
      duration: result.shuffle_phase.duration_ms,
      throughput: result.shuffle_phase.throughput_mb_s,
      color: "var(--chart-2)",
    },
    {
      name: "Reduce",
      duration: result.reduce_phase.duration_ms,
      throughput: result.reduce_phase.throughput_mb_s,
      color: "var(--chart-4)",
    },
  ]

  const totalDuration = result.total_duration_ms

  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>Phase Duration</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-[250px]">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={phases} layout="vertical" margin={{ top: 5, right: 30, left: 60, bottom: 5 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis
                  type="number"
                  tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
                  label={{ value: "ms", position: "insideBottomRight", offset: -5, fill: "var(--muted-foreground)", fontSize: 11 }}
                />
                <YAxis type="category" dataKey="name" tick={{ fill: "var(--muted-foreground)", fontSize: 12 }} />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "var(--card)",
                    border: "1px solid var(--border)",
                    borderRadius: "8px",
                    fontSize: "12px",
                  }}
                  formatter={(value) => [formatMs(Number(value)), "Duration"]}
                />
                <Bar dataKey="duration" radius={[0, 4, 4, 0]}>
                  {phases.map((p, i) => (
                    <Cell key={i} fill={p.color} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Phase Breakdown</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {phases.map((phase) => {
              const pct = (phase.duration / totalDuration) * 100

              return (
                <div key={phase.name} className="space-y-1.5">
                  <div className="flex items-center justify-between text-sm">
                    <span className="font-medium">{phase.name}</span>
                    <span className="text-muted-foreground">
                      {formatMs(phase.duration)} ({pct.toFixed(1)}%)
                    </span>
                  </div>
                  <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full transition-all"
                      style={{ width: `${pct}%`, backgroundColor: phase.color }}
                    />
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Throughput: {formatMBs(phase.throughput)}
                  </p>
                </div>
              )
            })}
            <div className="pt-2 border-t">
              <div className="flex items-center justify-between text-sm font-medium">
                <span>Total</span>
                <span>{formatMs(totalDuration)}</span>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
