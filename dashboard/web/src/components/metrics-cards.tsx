import type { SimulationResult } from "@/types"
import { Card, CardContent } from "@/components/ui/card"
import { formatMs, formatMBs } from "@/lib/utils"
import { Clock, Gauge, Timer, AlertTriangle } from "lucide-react"

export function MetricsCards({ result }: { result: SimulationResult }) {
  const cards = [
    {
      title: "Total Duration",
      value: formatMs(result.total_duration_ms),
      icon: Clock,
      description: `${result.config.input_size_mb} MB processed`,
    },
    {
      title: "Throughput",
      value: formatMBs(result.throughput_mb_s),
      icon: Gauge,
      description: "End-to-end data rate",
    },
    {
      title: "Avg Latency",
      value: formatMs(result.latency.avg),
      icon: Timer,
      description: `P99: ${formatMs(result.latency.p99)}`,
    },
    {
      title: "Crashes",
      value: result.total_crashes.toString(),
      icon: AlertTriangle,
      description: result.total_crashes > 0 ? `Recovery: ${formatMs(result.recovery_time_ms)}` : "No crashes",
    },
  ]

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {cards.map((card) => (
        <Card key={card.title}>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <p className="text-sm text-muted-foreground">{card.title}</p>
              <card.icon className="h-4 w-4 text-muted-foreground" />
            </div>
            <p className="text-2xl font-bold mt-1">{card.value}</p>
            <p className="text-xs text-muted-foreground mt-1">{card.description}</p>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
