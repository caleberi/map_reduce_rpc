import { useState, useRef, useCallback } from "react"
import { useQuery, useMutation } from "@tanstack/react-query"
import { fetchPlugins, fetchFiles, uploadFiles, runSimulation } from "@/lib/api"
import type { SimulationResult } from "@/types"
import { MapReduceFlow } from "@/components/topology/map-reduce-flow"
import { KpiSidebar } from "@/components/kpi-sidebar"
import { Button } from "@/components/ui/button"
import { Select } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Slider } from "@/components/ui/slider"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { ThroughputChart } from "@/components/throughput-chart"
import { LatencyChart } from "@/components/latency-chart"
import { PhaseBreakdown } from "@/components/phase-breakdown"
import { WorkerTable } from "@/components/worker-table"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import { Play, Loader2, ChevronDown, ChevronUp, Upload, FileText } from "lucide-react"
import { formatMs } from "@/lib/utils"

export function SimulatePage() {
  const [plugin, setPlugin] = useState("wc")
  const [inputSizeMb, setInputSizeMb] = useState(50)
  const [numWorkers, setNumWorkers] = useState(4)
  const [numChunks, setNumChunks] = useState(16)
  const [networkLatencyMs, setNetworkLatencyMs] = useState(5)
  const [result, setResult] = useState<SimulationResult | null>(null)
  const [showCharts, setShowCharts] = useState(false)
  const [selectedFiles, setSelectedFiles] = useState<string[]>([])
  const [isDragOver, setIsDragOver] = useState(false)
  const [runMode, setRunMode] = useState<"cluster" | "simulation">("simulation")
  const fileInputRef = useRef<HTMLInputElement>(null)

  const { data: plugins } = useQuery({
    queryKey: ["plugins"],
    queryFn: fetchPlugins,
  })

  const { data: availableFiles, refetch: refetchFiles } = useQuery({
    queryKey: ["files"],
    queryFn: fetchFiles,
  })

  const uploadMutation = useMutation({
    mutationFn: uploadFiles,
    onSuccess: () => refetchFiles(),
  })

  const mutation = useMutation({
    mutationFn: runSimulation,
    onSuccess: (data) => setResult(data),
  })

  const handleFileDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault()
      setIsDragOver(false)
      const files = Array.from(e.dataTransfer.files).filter(
        (f) => f.name.endsWith(".txt") || f.name.endsWith(".csv") || f.name.endsWith(".json"),
      )
      if (files.length > 0) uploadMutation.mutate(files)
    },
    [uploadMutation],
  )

  const handleFileSelect = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const files = Array.from(e.target.files ?? [])
      if (files.length > 0) uploadMutation.mutate(files)
      e.target.value = "" // allow re-selecting the same file
    },
    [uploadMutation],
  )

  const toggleFile = (name: string) => {
    setSelectedFiles((prev) =>
      prev.includes(name) ? prev.filter((f) => f !== name) : [...prev, name],
    )
  }

  const handleRun = () => {
    if (runMode === "cluster") {
      mutation.mutate({
        plugin,
        input_size_mb: 0,
        num_workers: 0,
        num_chunks: 0,
        network_latency_ms: 0,
        iterations: 1,
        mode: "cluster",
        files: selectedFiles.length > 0 ? selectedFiles : undefined,
      })
    } else {
      mutation.mutate({
        plugin,
        input_size_mb: inputSizeMb,
        num_workers: numWorkers,
        num_chunks: numChunks,
        network_latency_ms: networkLatencyMs,
        iterations: 1,
        mode: "simulation",
      })
    }
  }

  const selectedPlugin = plugins?.find((p) => p.name === plugin)

  return (
    <div className="flex flex-col h-[calc(100vh-48px)]">
      {/* ── 3‑column main area ──────────────────────────────── */}
      <div className="flex flex-1 min-h-0">
        {/* Left: Config */}
        <aside className="w-[272px] shrink-0 border-r border-border/40 p-4 overflow-y-auto">
          <h3 className="text-[10px] font-semibold uppercase tracking-[0.15em] text-muted-foreground mb-4">
            Configuration
          </h3>

          <div className="space-y-4">
            {/* Mode toggle */}
            <Field label="Mode">
              <div className="flex rounded-md border border-border/60 overflow-hidden text-[11px]">
                <button
                  className={`flex-1 px-2 py-1.5 transition-colors ${
                    runMode === "cluster"
                      ? "bg-primary text-primary-foreground"
                      : "hover:bg-muted/50"
                  }`}
                  onClick={() => setRunMode("cluster")}
                >
                  Cluster
                </button>
                <button
                  className={`flex-1 px-2 py-1.5 transition-colors ${
                    runMode === "simulation"
                      ? "bg-primary text-primary-foreground"
                      : "hover:bg-muted/50"
                  }`}
                  onClick={() => setRunMode("simulation")}
                >
                  Simulation
                </button>
              </div>
              <p className="text-[9px] text-muted-foreground leading-relaxed mt-1">
                {runMode === "cluster"
                  ? "Runs real MapReduce on the cluster with actual files."
                  : "Math model — sliders drive a simulated result."}
              </p>
            </Field>

            {/* Plugin */}
            <Field label="Plugin">
              <Select value={plugin} onChange={(e) => setPlugin(e.target.value)}>
                {plugins?.map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.name}
                  </option>
                ))}
              </Select>
              {selectedPlugin && (
                <p className="text-[10px] text-muted-foreground leading-relaxed mt-1">
                  {selectedPlugin.description}
                </p>
              )}
            </Field>

            {/* Simulation-only controls */}
            {runMode === "simulation" && (
              <>
                {/* Input Size */}
                <Field label="Input Size" right={`${inputSizeMb} MB`}>
                  <Slider
                    min={1}
                    max={500}
                    step={1}
                    value={[inputSizeMb]}
                    onValueChange={(v) => setInputSizeMb(v[0])}
                  />
                </Field>

                {/* Workers */}
                <Field label="Workers" right={String(numWorkers)}>
                  <Slider
                    min={1}
                    max={16}
                    step={1}
                    value={[numWorkers]}
                    onValueChange={(v) => setNumWorkers(v[0])}
                  />
                </Field>

                {/* Chunks */}
                <Field label="Chunks" right={String(numChunks)}>
                  <Slider
                    min={1}
                    max={128}
                    step={1}
                    value={[numChunks]}
                    onValueChange={(v) => setNumChunks(v[0])}
                  />
                </Field>

                {/* Network Latency */}
                <Field label="Network Latency" right={`${networkLatencyMs} ms`}>
                  <Slider
                    min={0}
                    max={100}
                    step={1}
                    value={[networkLatencyMs]}
                    onValueChange={(v) => setNetworkLatencyMs(v[0])}
                  />
                </Field>
              </>
            )}

            {/* Plugin complexity badges */}
            {selectedPlugin && (
              <div className="flex flex-wrap gap-1.5">
                <Badge variant="secondary" className="text-[10px]">
                  Map {selectedPlugin.map_complexity}×
                </Badge>
                <Badge variant="secondary" className="text-[10px]">
                  Reduce {selectedPlugin.reduce_complexity}×
                </Badge>
                {selectedPlugin.crash_rate > 0 && (
                  <Badge variant="destructive" className="text-[10px]">
                    Crash {(selectedPlugin.crash_rate * 100).toFixed(0)}%
                  </Badge>
                )}
              </div>
            )}

            {/* ── File Upload (cluster mode) ──────────────────────── */}
            {runMode === "cluster" && (
            <div className="space-y-2">
              <Label className="text-[10px] font-semibold uppercase tracking-[0.15em] text-muted-foreground">
                Input Files
              </Label>

              {/* Drop zone */}
              <div
                className={`relative border-2 border-dashed rounded-md p-3 transition-colors cursor-pointer ${
                  isDragOver
                    ? "border-primary bg-primary/5"
                    : "border-border/60 hover:border-border"
                }`}
                onDragOver={(e) => {
                  e.preventDefault()
                  setIsDragOver(true)
                }}
                onDragLeave={() => setIsDragOver(false)}
                onDrop={handleFileDrop}
                onClick={() => fileInputRef.current?.click()}
              >
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  accept=".txt,.csv,.json"
                  className="hidden"
                  onChange={handleFileSelect}
                />
                <div className="flex flex-col items-center gap-1 text-center">
                  <Upload className="h-4 w-4 text-muted-foreground" />
                  <span className="text-[10px] text-muted-foreground">
                    {uploadMutation.isPending
                      ? "Uploading…"
                      : "Drop files or click to browse"}
                  </span>
                </div>
              </div>

              {/* Scrollable file list */}
              {availableFiles && availableFiles.length > 0 && (
                <div className="max-h-[140px] overflow-y-auto space-y-0.5 rounded border border-border/40 p-1.5">
                  {availableFiles.map((f) => (
                    <label
                      key={`${f.source}-${f.name}`}
                      className="flex items-center gap-2 px-1.5 py-1 rounded text-[11px] hover:bg-muted/40 cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={selectedFiles.includes(f.name)}
                        onChange={() => toggleFile(f.name)}
                        className="rounded border-border"
                      />
                      <FileText className="h-3 w-3 shrink-0 text-muted-foreground" />
                      <span className="truncate flex-1">{f.name}</span>
                      <span className="text-[9px] text-muted-foreground shrink-0">
                        {f.source === "upload" ? "📤" : "📁"} {(f.size / 1024).toFixed(0)} KB
                      </span>
                    </label>
                  ))}
                </div>
              )}

              {selectedFiles.length > 0 && (
                <button
                  className="text-[10px] text-muted-foreground hover:text-foreground underline"
                  onClick={() => setSelectedFiles([])}
                >
                  Clear selection ({selectedFiles.length})
                </button>
              )}
            </div>
            )}

            {/* Run button */}
            <Button className="w-full gap-2" onClick={handleRun} disabled={mutation.isPending}>
              {mutation.isPending ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  <span className="text-xs">{runMode === "cluster" ? "Processing…" : "Simulating…"}</span>
                </>
              ) : (
                <>
                  <Play className="h-3.5 w-3.5" />
                  <span className="text-xs">{runMode === "cluster" ? "Run on Cluster" : "Run Simulation"}</span>
                </>
              )}
            </Button>

            {mutation.isError && (
              <p className="text-[10px] text-destructive leading-relaxed">
                Failed — ensure API server is running on :4400
              </p>
            )}
          </div>
        </aside>

        {/* Center: Topology */}
        <div className="flex-1 min-w-0 flex items-center justify-center p-4 overflow-hidden">
          <MapReduceFlow
            numWorkers={numWorkers}
            isRunning={mutation.isPending}
            result={result}
            plugin={plugin}
            inputSizeMb={inputSizeMb}
            numChunks={numChunks}
          />
        </div>

        {/* Right: KPIs */}
        <aside className="w-[252px] shrink-0 border-l border-border/40 overflow-y-auto">
          {result?.mode && (
            <div className="px-4 pt-3">
              <Badge
                variant={result.mode === "cluster" ? "default" : "secondary"}
                className="text-[10px]"
              >
                {result.mode === "cluster" ? "⚡ Cluster" : "📊 Simulated"}
              </Badge>
            </div>
          )}
          <KpiSidebar result={result} />
        </aside>
      </div>

      {/* ── Bottom: Detailed charts (collapsible) ──────────── */}
      {result && (
        <div className="border-t border-border/40">
          <button
            className="w-full flex items-center justify-center gap-2 py-2 text-[11px] text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => setShowCharts(!showCharts)}
          >
            {showCharts ? (
              <ChevronDown className="h-3.5 w-3.5" />
            ) : (
              <ChevronUp className="h-3.5 w-3.5" />
            )}
            {showCharts ? "Hide" : "Show"} Detailed Charts
          </button>

          {showCharts && (
            <div className="p-5 max-w-7xl mx-auto">
              <Tabs defaultValue="throughput">
                <TabsList>
                  <TabsTrigger value="throughput">Throughput</TabsTrigger>
                  <TabsTrigger value="latency">Latency</TabsTrigger>
                  <TabsTrigger value="phases">Phases</TabsTrigger>
                  <TabsTrigger value="workers">Workers</TabsTrigger>
                </TabsList>

                <TabsContent value="throughput">
                  <Card>
                    <CardHeader>
                      <CardTitle>Throughput Over Time</CardTitle>
                      <CardDescription>Data processing rate throughout the simulation</CardDescription>
                    </CardHeader>
                    <CardContent>
                      <ThroughputChart data={result.throughput_over_time} />
                    </CardContent>
                  </Card>
                </TabsContent>

                <TabsContent value="latency">
                  <Card>
                    <CardHeader>
                      <CardTitle>Latency Distribution</CardTitle>
                      <CardDescription>
                        Avg: {formatMs(result.latency.avg)} | P99: {formatMs(result.latency.p99)}
                      </CardDescription>
                    </CardHeader>
                    <CardContent>
                      <LatencyChart data={result.latency_over_time} distribution={result.latency} />
                    </CardContent>
                  </Card>
                </TabsContent>

                <TabsContent value="phases">
                  <PhaseBreakdown result={result} />
                </TabsContent>

                <TabsContent value="workers">
                  <Card>
                    <CardHeader>
                      <CardTitle>Worker Metrics</CardTitle>
                      <CardDescription>
                        {result.workers.length} workers · {result.total_crashes} crashes
                      </CardDescription>
                    </CardHeader>
                    <CardContent>
                      <WorkerTable workers={result.workers} />
                    </CardContent>
                  </Card>
                </TabsContent>
              </Tabs>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/* ── Field wrapper ───────────────────────────────────────── */

function Field({
  label,
  right,
  children,
}: {
  label: string
  right?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <Label className="text-[10px] text-muted-foreground uppercase tracking-wide">{label}</Label>
        {right && <span className="text-[11px] font-mono text-foreground">{right}</span>}
      </div>
      {children}
    </div>
  )
}
