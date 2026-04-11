export interface PluginProfile {
  name: string
  description: string
  map_complexity: number
  reduce_complexity: number
  crash_rate: number
  memory_factor: number
}

export interface SimulationRequest {
  plugin: string
  input_size_mb: number
  num_workers: number
  num_chunks: number
  network_latency_ms: number
  iterations: number
  files?: string[]
  mode?: "cluster" | "simulation"
}

export interface PhaseMetrics {
  duration_ms: number
  throughput_mb_s: number
}

export interface LatencyDistribution {
  p50: number
  p75: number
  p90: number
  p95: number
  p99: number
  min: number
  max: number
  avg: number
}

export interface WorkerMetrics {
  worker_id: number
  tasks_completed: number
  total_time_ms: number
  utilization: number
  crashes: number
}

export interface TimeSeriesPoint {
  timestamp_ms: number
  value: number
  label?: string
}

export interface SimulationResult {
  plugin: string
  config: SimulationRequest
  total_duration_ms: number
  map_phase: PhaseMetrics
  shuffle_phase: PhaseMetrics
  reduce_phase: PhaseMetrics
  throughput_mb_s: number
  latency: LatencyDistribution
  workers: WorkerMetrics[]
  throughput_over_time: TimeSeriesPoint[]
  latency_over_time: TimeSeriesPoint[]
  cpu_over_time: TimeSeriesPoint[]
  total_crashes: number
  recovery_time_ms: number
  timestamp: string
  mode?: string
}

export interface FileInfo {
  name: string
  size: number
  source: string // "input" or "upload"
}

export interface CompareResult {
  results: SimulationResult[]
}

export interface ServiceStatus {
  name: string
  address: string
  online: boolean
}

export interface ClusterStatus {
  coordinator: ServiceStatus
  master: ServiceStatus
  workers: ServiceStatus[]
  connected_at: string
}
