import type { PluginProfile, SimulationRequest, SimulationResult, CompareResult, ClusterStatus, FileInfo } from "@/types"

const BASE_URL = "/api"

export async function fetchPlugins(): Promise<PluginProfile[]> {
  const res = await fetch(`${BASE_URL}/plugins`)
  if (!res.ok) throw new Error("Failed to fetch plugins")
  return res.json()
}

export async function fetchClusterStatus(): Promise<ClusterStatus> {
  const res = await fetch(`${BASE_URL}/status`)
  if (!res.ok) throw new Error("Failed to fetch cluster status")
  return res.json()
}

export async function fetchFiles(): Promise<FileInfo[]> {
  const res = await fetch(`${BASE_URL}/files`)
  if (!res.ok) throw new Error("Failed to fetch files")
  return res.json()
}

export async function uploadFiles(files: File[]): Promise<{ status: string; files: FileInfo[] }> {
  const form = new FormData()
  for (const f of files) {
    form.append("files", f)
  }
  const res = await fetch(`${BASE_URL}/upload`, {
    method: "POST",
    body: form,
  })
  if (!res.ok) throw new Error("Upload failed")
  return res.json()
}

export async function runSimulation(req: SimulationRequest): Promise<SimulationResult> {
  const res = await fetch(`${BASE_URL}/simulate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  })
  if (!res.ok) throw new Error("Simulation failed")
  return res.json()
}

export async function runComparison(
  plugins: string[],
  inputSizeMb: number,
  numWorkers: number,
  numChunks: number,
  networkLatencyMs: number,
): Promise<CompareResult> {
  const res = await fetch(`${BASE_URL}/compare`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      plugins,
      input_size_mb: inputSizeMb,
      num_workers: numWorkers,
      num_chunks: numChunks,
      network_latency_ms: networkLatencyMs,
    }),
  })
  if (!res.ok) throw new Error("Comparison failed")
  return res.json()
}
