import { useMemo } from "react"
import type { SimulationResult, WorkerMetrics } from "@/types"
import { formatMs, formatMBs } from "@/lib/utils"

/* ── ViewBox constants ──────────────────────────────────── */
const W = 1000
const H = 680

/* ── Fixed node positions (viewBox coords) ──────────────── */
const INPUT = { x: 500, y: 55 }
const COORD = { x: 500, y: 175 }
const MASTER = { x: 500, y: 305 }
const OUTPUT = { x: 500, y: 565 }
const WORKER_Y = 440
const MAX_VISIBLE = 8

/* ── Flow colors per stage ──────────────────────────────── */
const FLOW = {
  upload: "#06b6d4",
  dispatch: "#0ea5e9",
  assign: "#10b981",
  collect: "#f59e0b",
}

/* ── Helpers ─────────────────────────────────────────────── */

function workerXPositions(n: number) {
  const count = Math.min(n, MAX_VISIBLE)
  if (count <= 0) return []
  if (count === 1) return [500]
  const totalW = Math.min(count * 110, 760)
  const start = 500 - totalW / 2
  const step = totalW / (count - 1)
  return Array.from({ length: count }, (_, i) => Math.round(start + i * step))
}

function bezier(fx: number, fy: number, tx: number, ty: number, dy = 25) {
  const y1 = fy + dy
  const y2 = ty - dy
  const mid = (y1 + y2) / 2
  return `M${fx},${y1} C${fx},${mid} ${tx},${mid} ${tx},${y2}`
}

interface Conn {
  id: string
  d: string
  color: string
}

function buildConnections(workerXs: number[]): Conn[] {
  const paths: Conn[] = []
  paths.push({ id: "p-in-coord", d: bezier(INPUT.x, INPUT.y, COORD.x, COORD.y), color: FLOW.upload })
  paths.push({ id: "p-coord-master", d: bezier(COORD.x, COORD.y, MASTER.x, MASTER.y), color: FLOW.dispatch })
  workerXs.forEach((wx, i) => {
    paths.push({ id: `p-m-w${i}`, d: bezier(MASTER.x, MASTER.y, wx, WORKER_Y), color: FLOW.assign })
  })
  workerXs.forEach((wx, i) => {
    paths.push({ id: `p-w${i}-out`, d: bezier(wx, WORKER_Y, OUTPUT.x, OUTPUT.y), color: FLOW.collect })
  })
  return paths
}

/* ── Props ───────────────────────────────────────────────── */

interface Props {
  numWorkers: number
  isRunning: boolean
  result: SimulationResult | null
  plugin: string
  inputSizeMb: number
  numChunks: number
}

/* ── Main component ──────────────────────────────────────── */

export function MapReduceFlow({ numWorkers, isRunning, result, plugin, inputSizeMb, numChunks }: Props) {
  const workerXs = useMemo(() => workerXPositions(numWorkers), [numWorkers])
  const connections = useMemo(() => buildConnections(workerXs), [workerXs])
  const isActive = isRunning || !!result

  const pct = (x: number, y: number) => ({
    left: `${(x / W) * 100}%`,
    top: `${(y / H) * 100}%`,
  })

  return (
    <div className="relative w-full max-w-[920px]" style={{ aspectRatio: `${W}/${H}` }}>
      {/* ── SVG Layer ──────────────────────────────────────── */}
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="absolute inset-0 w-full h-full"
        xmlns="http://www.w3.org/2000/svg"
      >
        <defs>
          <filter id="blur-glow" x="-50%" y="-50%" width="200%" height="200%">
            <feGaussianBlur stdDeviation="6" />
          </filter>
          <pattern id="topo-grid" width="50" height="50" patternUnits="userSpaceOnUse">
            <path d="M 50 0 L 0 0 0 50" fill="none" stroke="var(--topo-grid-stroke)" strokeWidth="0.5" />
          </pattern>
        </defs>

        {/* Subtle grid */}
        <rect width={W} height={H} fill="url(#topo-grid)" />

        {/* Glow layers (wide, blurred halos behind paths) */}
        {connections.map((c) => (
          <path
            key={`${c.id}-glow`}
            d={c.d}
            fill="none"
            stroke={c.color}
            strokeWidth={isActive ? 10 : 4}
            opacity={isActive ? 0.12 : 0.04}
            filter="url(#blur-glow)"
            className={isActive ? "flow-glow-layer" : ""}
          />
        ))}

        {/* Main strokes */}
        {connections.map((c) => (
          <path
            key={c.id}
            id={c.id}
            d={c.d}
            fill="none"
            stroke={c.color}
            strokeWidth={isActive ? 2 : 1}
            strokeLinecap="round"
            className={isActive ? "flow-active" : "flow-idle"}
          />
        ))}

        {/* Animated particles */}
        {isActive &&
          connections.map((c) => {
            const isWorkerPath = c.id.includes("-w")
            const delays = isWorkerPath ? [0, 1.0] : [0, 0.7, 1.4]
            const dur = isWorkerPath ? 1.8 : 2.2
            const r = isWorkerPath ? 2.5 : 3
            return delays.map((delay, pi) => (
              <circle key={`${c.id}-p${pi}`} r={r} fill={c.color}>
                <animate attributeName="opacity" values="0.5;1;0.5" dur="1s" repeatCount="indefinite" />
                <animateMotion dur={`${dur}s`} repeatCount="indefinite" begin={`${delay}s`}>
                  <mpath href={`#${c.id}`} />
                </animateMotion>
              </circle>
            ))
          })}
      </svg>

      {/* ── HTML Node Overlays ─────────────────────────────── */}

      {/* Input node */}
      <NodeCard
        style={pct(INPUT.x, INPUT.y)}
        label="INPUT FILES"
        color={FLOW.upload}
        isActive={isActive}
        metrics={[
          { k: "Size", v: `${inputSizeMb} MB` },
          { k: "Chunks", v: `${numChunks}` },
        ]}
      />

      {/* Coordinator */}
      <NodeCard
        style={pct(COORD.x, COORD.y)}
        label="COORDINATOR"
        color="#0ea5e9"
        isActive={isActive}
        sublabel="Buffer → WAL → DFS"
        metrics={
          result
            ? [{ k: "Uploaded", v: `${inputSizeMb} MB` }]
            : undefined
        }
      />

      {/* Master */}
      <NodeCard
        style={pct(MASTER.x, MASTER.y)}
        label="MASTER"
        color="#8b5cf6"
        isActive={isActive}
        sublabel="Dispatch & Orchestrate"
        metrics={
          result
            ? [
                { k: "Throughput", v: formatMBs(result.throughput_mb_s) },
                { k: "Duration", v: formatMs(result.total_duration_ms) },
              ]
            : undefined
        }
      />

      {/* Workers */}
      {workerXs.map((wx, i) => {
        const wm: WorkerMetrics | undefined = result?.workers[i]
        return (
          <NodeCard
            key={i}
            style={pct(wx, WORKER_Y)}
            label={`W${i + 1}`}
            color="#10b981"
            isActive={isActive}
            small
            metrics={
              wm
                ? [
                    { k: "Tasks", v: `${wm.tasks_completed}` },
                    { k: "Util", v: `${Math.round(wm.utilization * 100)}%` },
                  ]
                : undefined
            }
          />
        )
      })}

      {/* Overflow badge */}
      {numWorkers > MAX_VISIBLE && (
        <div
          className="absolute -translate-x-1/2 -translate-y-1/2 text-[10px] text-muted-foreground font-mono"
          style={{ left: "92%", top: `${(WORKER_Y / H) * 100}%` }}
        >
          +{numWorkers - MAX_VISIBLE}
        </div>
      )}

      {/* Output */}
      <NodeCard
        style={pct(OUTPUT.x, OUTPUT.y)}
        label="OUTPUT"
        color="#f59e0b"
        isActive={isActive}
        sublabel={result ? `${plugin} complete` : "Results"}
        metrics={
          result
            ? [
                { k: "Total", v: formatMs(result.total_duration_ms) },
                { k: "Crashes", v: `${result.total_crashes}` },
              ]
            : undefined
        }
      />

      {/* Phase legend */}
      <div className="absolute bottom-2 left-1/2 -translate-x-1/2 flex gap-5 text-[10px] text-muted-foreground">
        {[
          { label: "Upload", color: FLOW.upload },
          { label: "Dispatch", color: FLOW.dispatch },
          { label: "Map / Reduce", color: FLOW.assign },
          { label: "Collect", color: FLOW.collect },
        ].map((l) => (
          <span key={l.label} className="flex items-center gap-1.5">
            <span
              className="w-4 h-[3px] rounded-full"
              style={{ backgroundColor: l.color, boxShadow: `0 0 6px ${l.color}60` }}
            />
            {l.label}
          </span>
        ))}
      </div>

      {/* Running overlay */}
      {isRunning && (
        <div className="absolute top-2 left-1/2 -translate-x-1/2 flex items-center gap-2 px-3 py-1 rounded-full bg-card/60 backdrop-blur-sm border border-primary/30">
          <span className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary opacity-75" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
          </span>
          <span className="text-[11px] font-medium text-primary">Processing...</span>
        </div>
      )}
    </div>
  )
}

/* ── Node Card ──────────────────────────────────────────── */

function NodeCard({
  style,
  label,
  color,
  isActive,
  sublabel,
  metrics,
  small,
}: {
  style: { left: string; top: string }
  label: string
  color: string
  isActive: boolean
  sublabel?: string
  metrics?: { k: string; v: string }[]
  small?: boolean
}) {
  return (
    <div className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none" style={style}>
      <div
        className={`rounded-xl border backdrop-blur-sm transition-all duration-700 ${
          small ? "px-2.5 py-1.5 min-w-[68px]" : "px-4 py-2.5 min-w-[152px]"
        }`}
        style={{
          background: "var(--node-bg)",
          borderColor: isActive ? `${color}50` : "var(--node-border)",
          boxShadow: isActive
            ? `0 0 16px ${color}20, 0 0 40px ${color}08, inset 0 1px 0 ${color}10`
            : "0 1px 3px rgba(0,0,0,0.08)",
        }}
      >
        {/* Dot + label */}
        <div className="flex items-center gap-1.5">
          <span
            className="rounded-full shrink-0"
            style={{
              width: small ? 5 : 6,
              height: small ? 5 : 6,
              backgroundColor: color,
              boxShadow: isActive ? `0 0 8px ${color}` : "none",
            }}
          />
          <span
            className={`font-semibold tracking-wider uppercase ${small ? "text-[9px]" : "text-[10px]"}`}
            style={{ color }}
          >
            {label}
          </span>
        </div>

        {/* Sublabel */}
        {sublabel && !small && (
          <p className="text-[9px] text-muted-foreground mt-0.5 pl-[calc(6px+0.375rem)]">{sublabel}</p>
        )}

        {/* Metrics */}
        {metrics && metrics.length > 0 && (
          <div className={`mt-1 space-y-px ${small ? "" : "pl-[calc(6px+0.375rem)]"}`}>
            {metrics.map((m) => (
              <div key={m.k} className="flex items-center justify-between gap-3">
                <span className="text-[9px] text-muted-foreground">{m.k}</span>
                <span className="text-[10px] font-mono font-medium text-foreground">{m.v}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
