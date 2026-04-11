import type { SimulationResult } from "@/types"
import { formatMs } from "@/lib/utils"
import { cn } from "@/lib/utils"

export function KpiSidebar({ result }: { result: SimulationResult | null }) {
  if (!result) {
    return (
      <div className="p-5 h-full flex flex-col items-center justify-center text-center">
        <div className="w-10 h-10 rounded-full border border-border/50 flex items-center justify-center mb-3">
          <div className="w-2 h-2 rounded-full bg-muted-foreground/30" />
        </div>
        <p className="text-[11px] text-muted-foreground leading-relaxed">
          Run a simulation to<br />view real-time metrics
        </p>
      </div>
    )
  }

  const total = result.total_duration_ms
  const mapPct = Math.round((result.map_phase.duration_ms / total) * 100)
  const shufflePct = Math.round((result.shuffle_phase.duration_ms / total) * 100)
  const reducePct = Math.round((result.reduce_phase.duration_ms / total) * 100)

  return (
    <div className="p-4 space-y-4 text-sm">
      {/* Section header */}
      <h3 className="text-[10px] font-semibold uppercase tracking-[0.15em] text-muted-foreground">
        Metrics
      </h3>

      {/* Total Duration — hero metric */}
      <div>
        <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-0.5">Total Duration</p>
        <div className="flex items-baseline gap-1">
          <span className="text-3xl font-bold font-mono tracking-tighter text-foreground">
            {total < 1000 ? Math.round(total) : (total / 1000).toFixed(1)}
          </span>
          <span className="text-sm text-muted-foreground">{total < 1000 ? "ms" : "s"}</span>
        </div>
      </div>

      {/* Throughput — hero metric */}
      <div>
        <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-0.5">Throughput</p>
        <div className="flex items-baseline gap-1">
          <span className="text-2xl font-bold font-mono tracking-tighter" style={{ color: "#10b981" }}>
            {result.throughput_mb_s.toFixed(1)}
          </span>
          <span className="text-xs text-muted-foreground">MB/s</span>
        </div>
      </div>

      <Divider />

      {/* Latency grid */}
      <div>
        <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-2">Latency</p>
        <div className="grid grid-cols-2 gap-1.5">
          <MetricCell label="AVG" value={formatMs(result.latency.avg)} />
          <MetricCell label="P50" value={formatMs(result.latency.p50)} />
          <MetricCell label="P95" value={formatMs(result.latency.p95)} />
          <MetricCell label="P99" value={formatMs(result.latency.p99)} color="#f59e0b" />
        </div>
      </div>

      <Divider />

      {/* Phase breakdown */}
      <div>
        <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-2">Phase Breakdown</p>
        <div className="space-y-2">
          <PhaseBar label="Map" pct={mapPct} ms={result.map_phase.duration_ms} color="#06b6d4" />
          <PhaseBar label="Shuffle" pct={shufflePct} ms={result.shuffle_phase.duration_ms} color="#f59e0b" />
          <PhaseBar label="Reduce" pct={reducePct} ms={result.reduce_phase.duration_ms} color="#10b981" />
        </div>
      </div>

      <Divider />

      {/* Workers & Crashes */}
      <div className="grid grid-cols-2 gap-3">
        <div>
          <p className="text-[10px] text-muted-foreground uppercase tracking-wide">Workers</p>
          <span className="text-xl font-bold font-mono">{result.workers.length}</span>
        </div>
        <div>
          <p className="text-[10px] text-muted-foreground uppercase tracking-wide">Crashes</p>
          <span
            className={cn("text-xl font-bold font-mono", result.total_crashes > 0 && "text-destructive")}
          >
            {result.total_crashes}
          </span>
        </div>
      </div>

      {/* Worker utilization grid */}
      <div>
        <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-2">Utilization</p>
        <div className="flex gap-1.5 flex-wrap">
          {result.workers.map((w) => {
            const pct = Math.round(w.utilization * 100)
            const borderColor = pct >= 90 ? "#10b981" : pct >= 70 ? "#f59e0b" : "#ef4444"
            return (
              <div
                key={w.worker_id}
                className="w-9 h-9 rounded-lg bg-secondary/60 border flex flex-col items-center justify-center"
                style={{ borderColor: `${borderColor}60` }}
              >
                <span className="text-[10px] font-mono font-bold">{pct}</span>
                <span className="text-[7px] text-muted-foreground">%</span>
              </div>
            )
          })}
        </div>
      </div>

      {/* Capacity gauges */}
      <div>
        <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-2">
          Processing Capacity
        </p>
        <div className="flex justify-between gap-2">
          <GaugeMini label="Map" value={result.map_phase.throughput_mb_s} max={result.throughput_mb_s * 2} color="#06b6d4" />
          <GaugeMini label="Reduce" value={result.reduce_phase.throughput_mb_s} max={result.throughput_mb_s * 2} color="#10b981" />
        </div>
      </div>
    </div>
  )
}

/* ── Sub-components ──────────────────────────────────────── */

function Divider() {
  return <div className="h-px bg-border/40" />
}

function MetricCell({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div className="bg-secondary/40 rounded-md px-2.5 py-1.5">
      <p className="text-[8px] text-muted-foreground uppercase tracking-wider">{label}</p>
      <p className="text-xs font-mono font-medium" style={color ? { color } : undefined}>
        {value}
      </p>
    </div>
  )
}

function PhaseBar({ label, pct, ms, color }: { label: string; pct: number; ms: number; color: string }) {
  return (
    <div>
      <div className="flex justify-between items-center mb-0.5">
        <span className="text-[10px] font-medium">{label}</span>
        <span className="text-[10px] font-mono text-muted-foreground">
          {formatMs(ms)} · {pct}%
        </span>
      </div>
      <div className="h-1.5 bg-secondary/60 rounded-full overflow-hidden">
        <div
          className="h-full rounded-full transition-all duration-700"
          style={{ width: `${Math.max(pct, 2)}%`, backgroundColor: color }}
        />
      </div>
    </div>
  )
}

function GaugeMini({
  label,
  value,
  max,
  color,
}: {
  label: string
  value: number
  max: number
  color: string
}) {
  const pct = Math.min(Math.round((value / max) * 100), 100)
  return (
    <div className="flex-1 text-center">
      <div
        className="relative mx-auto w-14 h-14 rounded-full border-[3px] flex items-center justify-center"
        style={{
          borderColor: `${color}30`,
          background: `conic-gradient(${color} ${pct * 3.6}deg, transparent 0deg)`,
          WebkitMask: "radial-gradient(circle at center, transparent 60%, black 61%)",
          mask: "radial-gradient(circle at center, transparent 60%, black 61%)",
        }}
      >
        <span className="absolute text-[11px] font-mono font-bold" style={{ color }}>
          {pct}%
        </span>
      </div>
      <p className="text-[9px] text-muted-foreground mt-1">{label}</p>
    </div>
  )
}
