package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Cluster connection helpers
// ---------------------------------------------------------------------------

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// rpcPing dials an RPC service and tries a 2-second connect
func rpcPing(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// rpcCall dials, calls, and closes
func rpcCall(addr, method string, args, reply interface{}) error {
	client, err := rpc.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Call(method, args, reply)
}

// ---------------------------------------------------------------------------
// Mirror types that match the mrp package RPC types
// ---------------------------------------------------------------------------

type Handle struct {
	Id        uint64
	TimeStamp int64
}

type HandleRequest struct {
	Handle Handle
}

type HandleReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
	Handle       Handle
}

type DownloadRequest struct {
	Handle Handle
	Data   []byte
	Eof    bool
}

type DownloadReply struct {
	Status       string
	ErrorCode    int
	ErrorMessage string
}

type TriggerProcessingRequest struct {
	Plugin string
}

type TriggerProcessingReply struct {
	Status            string
	ErrorMessage      string
	HandlesDispatched int
}

type GetJobResultRequest struct {
	HandleId uint64
}

type GetJobResultReply struct {
	Status       string
	ErrorMessage string
	Complete     bool
	ResultJSON   string
}

// ---------------------------------------------------------------------------
// Dashboard-specific types
// ---------------------------------------------------------------------------

type ServiceStatus struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Online  bool   `json:"online"`
}

type ClusterStatus struct {
	Coordinator ServiceStatus   `json:"coordinator"`
	Master      ServiceStatus   `json:"master"`
	Workers     []ServiceStatus `json:"workers"`
	ConnectedAt string          `json:"connected_at"`
}

type TriggerRequest struct {
	Handle int64    `json:"handle"`
	Folder string   `json:"folder"`
	Files  []string `json:"files"`
	Plugin string   `json:"plugin"`
}

type TriggerReplyMsg struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Plugin profiles – each plugin has different compute characteristics
// ---------------------------------------------------------------------------

type PluginProfile struct {
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	MapComplexity    float64 `json:"map_complexity"`
	ReduceComplexity float64 `json:"reduce_complexity"`
	CrashRate        float64 `json:"crash_rate"`
	MemoryFactor     float64 `json:"memory_factor"`
}

var pluginProfiles = map[string]PluginProfile{
	"wc": {
		Name: "wc", Description: "Word Count – simple tokenisation and aggregation",
		MapComplexity: 1.0, ReduceComplexity: 1.0, CrashRate: 0, MemoryFactor: 1.0,
	},
	"indexer": {
		Name: "indexer", Description: "Inverted Index – heavier reduce with document tracking",
		MapComplexity: 1.5, ReduceComplexity: 2.5, CrashRate: 0, MemoryFactor: 1.8,
	},
	"mtiming": {
		Name: "mtiming", Description: "Map Timing – measures map-phase parallelism",
		MapComplexity: 1.2, ReduceComplexity: 0.5, CrashRate: 0, MemoryFactor: 1.0,
	},
	"rtiming": {
		Name: "rtiming", Description: "Reduce Timing – measures reduce-phase parallelism",
		MapComplexity: 0.5, ReduceComplexity: 1.4, CrashRate: 0, MemoryFactor: 1.0,
	},
	"jobcount": {
		Name: "jobcount", Description: "Job Count – lightweight counter plugin",
		MapComplexity: 0.3, ReduceComplexity: 0.3, CrashRate: 0, MemoryFactor: 0.5,
	},
	"crash": {
		Name: "crash", Description: "Crash – randomly crashes workers to test fault tolerance",
		MapComplexity: 1.0, ReduceComplexity: 1.0, CrashRate: 0.3, MemoryFactor: 1.0,
	},
	"nocrash": {
		Name: "nocrash", Description: "No-Crash – identical to wc but labelled for crash-test baseline",
		MapComplexity: 1.0, ReduceComplexity: 1.0, CrashRate: 0, MemoryFactor: 1.0,
	},
	"early_exit": {
		Name: "early_exit", Description: "Early Exit – validates that workers exit only after completion",
		MapComplexity: 1.0, ReduceComplexity: 1.0, CrashRate: 0, MemoryFactor: 1.0,
	},
}

// ---------------------------------------------------------------------------
// Simulation request / response models
// ---------------------------------------------------------------------------

type SimulationRequest struct {
	Plugin           string   `json:"plugin"`
	InputSizeMB      float64  `json:"input_size_mb"`
	NumWorkers       int      `json:"num_workers"`
	NumChunks        int      `json:"num_chunks"`
	NetworkLatencyMs float64  `json:"network_latency_ms"`
	Iterations       int      `json:"iterations"`
	Files            []string `json:"files,omitempty"`
	Mode             string   `json:"mode,omitempty"` // "cluster" or "simulation"; empty = auto
}

type PhaseMetrics struct {
	DurationMs float64 `json:"duration_ms"`
	Throughput float64 `json:"throughput_mb_s"`
}

type LatencyDistribution struct {
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P90 float64 `json:"p90"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
	Avg float64 `json:"avg"`
}

type WorkerMetrics struct {
	WorkerID       int     `json:"worker_id"`
	TasksCompleted int     `json:"tasks_completed"`
	TotalTimeMs    float64 `json:"total_time_ms"`
	Utilization    float64 `json:"utilization"`
	Crashes        int     `json:"crashes"`
}

type TimeSeriesPoint struct {
	TimestampMs float64 `json:"timestamp_ms"`
	Value       float64 `json:"value"`
	Label       string  `json:"label,omitempty"`
}

type SimulationResult struct {
	Plugin             string              `json:"plugin"`
	Config             SimulationRequest   `json:"config"`
	TotalDurationMs    float64             `json:"total_duration_ms"`
	MapPhase           PhaseMetrics        `json:"map_phase"`
	ShufflePhase       PhaseMetrics        `json:"shuffle_phase"`
	ReducePhase        PhaseMetrics        `json:"reduce_phase"`
	Throughput         float64             `json:"throughput_mb_s"`
	Latency            LatencyDistribution `json:"latency"`
	Workers            []WorkerMetrics     `json:"workers"`
	ThroughputOverTime []TimeSeriesPoint   `json:"throughput_over_time"`
	LatencyOverTime    []TimeSeriesPoint   `json:"latency_over_time"`
	CpuOverTime        []TimeSeriesPoint   `json:"cpu_over_time"`
	TotalCrashes       int                 `json:"total_crashes"`
	RecoveryTimeMs     float64             `json:"recovery_time_ms"`
	Timestamp          string              `json:"timestamp"`
	Mode               string              `json:"mode"`
}

type CompareRequest struct {
	Plugins          []string `json:"plugins"`
	InputSizeMB      float64  `json:"input_size_mb"`
	NumWorkers       int      `json:"num_workers"`
	NumChunks        int      `json:"num_chunks"`
	NetworkLatencyMs float64  `json:"network_latency_ms"`
}

type CompareResult struct {
	Results []SimulationResult `json:"results"`
}

// ---------------------------------------------------------------------------
// Simulation engine
// ---------------------------------------------------------------------------

func simulate(req SimulationRequest) SimulationResult {
	profile, ok := pluginProfiles[req.Plugin]
	if !ok {
		profile = pluginProfiles["wc"]
	}

	if req.InputSizeMB <= 0 {
		req.InputSizeMB = 10
	}
	if req.NumWorkers <= 0 {
		req.NumWorkers = 3
	}
	if req.NumChunks <= 0 {
		req.NumChunks = int(math.Max(float64(req.NumWorkers)*2, math.Ceil(req.InputSizeMB/8)))
	}
	if req.NetworkLatencyMs <= 0 {
		req.NetworkLatencyMs = 2
	}
	if req.Iterations <= 0 {
		req.Iterations = 1
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	chunkSizeMB := req.InputSizeMB / float64(req.NumChunks)

	// Base processing rates (MB/s) per phase
	baseMapRate := 120.0    // MB/s
	baseShuffleRate := 80.0 // MB/s
	baseReduceRate := 90.0  // MB/s

	effectiveMapRate := baseMapRate / profile.MapComplexity
	effectiveReduceRate := baseReduceRate / profile.ReduceComplexity
	intermediateExpansion := 1.0 + (profile.MemoryFactor-1.0)*0.3
	intermediateSizeMB := req.InputSizeMB * intermediateExpansion

	// Per-chunk latencies for distribution
	allChunkLatencies := make([]float64, 0, req.NumChunks)

	// Simulate map phase
	mapChunkTimes := make([]float64, req.NumChunks)
	for i := 0; i < req.NumChunks; i++ {
		processingTime := (chunkSizeMB / effectiveMapRate) * 1000 // ms
		networkTime := req.NetworkLatencyMs * (1 + rng.Float64()*0.5)
		jitter := 1.0 + (rng.Float64()-0.5)*0.2
		mapChunkTimes[i] = (processingTime + networkTime) * jitter
		allChunkLatencies = append(allChunkLatencies, mapChunkTimes[i])
	}

	// Simulate worker assignment (greedy load balancing)
	workers := make([]WorkerMetrics, req.NumWorkers)
	for i := range workers {
		workers[i].WorkerID = i + 1
	}

	// Sort chunks by time descending for better load balance simulation
	sortedChunkIdx := make([]int, req.NumChunks)
	for i := range sortedChunkIdx {
		sortedChunkIdx[i] = i
	}
	sort.Slice(sortedChunkIdx, func(a, b int) bool {
		return mapChunkTimes[sortedChunkIdx[a]] > mapChunkTimes[sortedChunkIdx[b]]
	})

	workerLoad := make([]float64, req.NumWorkers)
	for _, idx := range sortedChunkIdx {
		// Find worker with least load
		minW := 0
		for w := 1; w < req.NumWorkers; w++ {
			if workerLoad[w] < workerLoad[minW] {
				minW = w
			}
		}
		workerLoad[minW] += mapChunkTimes[idx]
		workers[minW].TasksCompleted++

		// Simulate crashes
		if profile.CrashRate > 0 && rng.Float64() < profile.CrashRate {
			recoveryTime := 500 + rng.Float64()*2000 // 0.5-2.5s recovery
			workerLoad[minW] += recoveryTime
			workerLoad[minW] += mapChunkTimes[idx] // retry
			workers[minW].Crashes++
		}
	}

	// Map phase duration is the max worker load (parallel)
	mapDuration := 0.0
	for _, load := range workerLoad {
		if load > mapDuration {
			mapDuration = load
		}
	}

	// Shuffle phase
	shuffleDuration := (intermediateSizeMB / baseShuffleRate) * 1000
	shuffleDuration *= (1 + rng.Float64()*0.15)
	shuffleDuration += req.NetworkLatencyMs * float64(req.NumWorkers)

	// Reduce phase
	nReduceTasks := int(math.Max(float64(req.NumWorkers), float64(req.NumChunks)/2))
	reduceChunkSize := intermediateSizeMB / float64(nReduceTasks)
	reduceWorkerLoad := make([]float64, req.NumWorkers)
	for i := 0; i < nReduceTasks; i++ {
		minW := 0
		for w := 1; w < req.NumWorkers; w++ {
			if reduceWorkerLoad[w] < reduceWorkerLoad[minW] {
				minW = w
			}
		}
		processingTime := (reduceChunkSize / effectiveReduceRate) * 1000
		jitter := 1.0 + (rng.Float64()-0.5)*0.15
		reduceWorkerLoad[minW] += processingTime * jitter
		reduceWorkerLoad[minW] += req.NetworkLatencyMs

		allChunkLatencies = append(allChunkLatencies, processingTime*jitter)
	}

	reduceDuration := 0.0
	for _, load := range reduceWorkerLoad {
		if load > reduceDuration {
			reduceDuration = load
		}
	}

	// Worker utilization
	totalDuration := mapDuration + shuffleDuration + reduceDuration
	for i := range workers {
		workers[i].TotalTimeMs = workerLoad[i] + reduceWorkerLoad[i]
		if totalDuration > 0 {
			workers[i].Utilization = math.Min(workers[i].TotalTimeMs/totalDuration, 1.0)
		}
	}

	// Latency distribution
	sort.Float64s(allChunkLatencies)
	latencyDist := computeLatencyDistribution(allChunkLatencies)

	// Total crashes
	totalCrashes := 0
	totalRecovery := 0.0
	for _, w := range workers {
		totalCrashes += w.Crashes
		totalRecovery += float64(w.Crashes) * 1500
	}

	// Time series data
	throughputTS := generateThroughputTimeSeries(rng, totalDuration, req.InputSizeMB, profile)
	latencyTS := generateLatencyTimeSeries(rng, totalDuration, allChunkLatencies, profile)
	cpuTS := generateCpuTimeSeries(rng, totalDuration, req.NumWorkers, profile)

	result := SimulationResult{
		Plugin:          req.Plugin,
		Config:          req,
		TotalDurationMs: totalDuration,
		MapPhase: PhaseMetrics{
			DurationMs: mapDuration,
			Throughput: req.InputSizeMB / (mapDuration / 1000),
		},
		ShufflePhase: PhaseMetrics{
			DurationMs: shuffleDuration,
			Throughput: intermediateSizeMB / (shuffleDuration / 1000),
		},
		ReducePhase: PhaseMetrics{
			DurationMs: reduceDuration,
			Throughput: intermediateSizeMB / (reduceDuration / 1000),
		},
		Throughput:         req.InputSizeMB / (totalDuration / 1000),
		Latency:            latencyDist,
		Workers:            workers,
		ThroughputOverTime: throughputTS,
		LatencyOverTime:    latencyTS,
		CpuOverTime:        cpuTS,
		TotalCrashes:       totalCrashes,
		RecoveryTimeMs:     totalRecovery,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		Mode:               "simulation",
	}

	return result
}

func computeLatencyDistribution(sorted []float64) LatencyDistribution {
	if len(sorted) == 0 {
		return LatencyDistribution{}
	}
	n := len(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	return LatencyDistribution{
		Min: sorted[0],
		Max: sorted[n-1],
		Avg: sum / float64(n),
		P50: percentile(sorted, 0.50),
		P75: percentile(sorted, 0.75),
		P90: percentile(sorted, 0.90),
		P95: percentile(sorted, 0.95),
		P99: percentile(sorted, 0.99),
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper || upper >= len(sorted) {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func generateThroughputTimeSeries(rng *rand.Rand, totalMs, inputMB float64, profile PluginProfile) []TimeSeriesPoint {
	points := make([]TimeSeriesPoint, 0, 50)
	avgThroughput := inputMB / (totalMs / 1000)
	steps := 50
	for i := 0; i <= steps; i++ {
		t := (float64(i) / float64(steps)) * totalMs
		// Ramp up, plateau, ramp down
		progress := float64(i) / float64(steps)
		var factor float64
		switch {
		case progress < 0.1:
			factor = progress / 0.1 * 0.8
		case progress < 0.85:
			factor = 0.8 + rng.Float64()*0.4
		default:
			factor = (1 - (progress-0.85)/0.15) * 0.8
		}
		points = append(points, TimeSeriesPoint{
			TimestampMs: math.Round(t*100) / 100,
			Value:       math.Round(avgThroughput*factor*100) / 100,
		})
	}
	return points
}

func generateLatencyTimeSeries(rng *rand.Rand, totalMs float64, latencies []float64, profile PluginProfile) []TimeSeriesPoint {
	points := make([]TimeSeriesPoint, 0, 50)
	avgLatency := 0.0
	if len(latencies) > 0 {
		for _, l := range latencies {
			avgLatency += l
		}
		avgLatency /= float64(len(latencies))
	}
	steps := 50
	for i := 0; i <= steps; i++ {
		t := (float64(i) / float64(steps)) * totalMs
		jitter := 1.0 + (rng.Float64()-0.5)*0.4
		value := avgLatency * jitter
		if profile.CrashRate > 0 && rng.Float64() < profile.CrashRate*0.5 {
			value *= 2.5 // spike from crash recovery
		}
		points = append(points, TimeSeriesPoint{
			TimestampMs: math.Round(t*100) / 100,
			Value:       math.Round(value*100) / 100,
		})
	}
	return points
}

func generateCpuTimeSeries(rng *rand.Rand, totalMs float64, numWorkers int, profile PluginProfile) []TimeSeriesPoint {
	points := make([]TimeSeriesPoint, 0, 50)
	steps := 50
	baseCPU := math.Min(float64(numWorkers)*25, 95)
	for i := 0; i <= steps; i++ {
		t := (float64(i) / float64(steps)) * totalMs
		progress := float64(i) / float64(steps)
		var factor float64
		switch {
		case progress < 0.05:
			factor = progress / 0.05 * 0.6
		case progress < 0.9:
			factor = 0.6 + rng.Float64()*0.4
		default:
			factor = math.Max((1-(progress-0.9)/0.1)*0.6, 0.05)
		}
		points = append(points, TimeSeriesPoint{
			TimestampMs: math.Round(t*100) / 100,
			Value:       math.Round(baseCPU*factor*100) / 100,
		})
	}
	return points
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func handlePlugins(w http.ResponseWriter, r *http.Request) {
	profiles := make([]PluginProfile, 0, len(pluginProfiles))
	for _, p := range pluginProfiles {
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	writeJSON(w, http.StatusOK, profiles)
}

func handleSimulate(w http.ResponseWriter, r *http.Request) {
	var req SimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	coordAddr := envOr("MAPREDUCE_COORDINATOR_ADDRESS", "")
	masterAddr := envOr("MAPREDUCE_MASTER_ADDRESS", "")
	inputDir := envOr("MAPREDUCE_INPUT_DIR", "./inputs")

	// Explicit mode from the frontend, or auto-detect.
	wantCluster := req.Mode == "cluster"
	wantSim := req.Mode == "simulation"
	clusterAvailable := coordAddr != "" && masterAddr != "" && rpcPing(coordAddr)

	if wantSim || (!wantCluster && !clusterAvailable) {
		if _, ok := pluginProfiles[req.Plugin]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown plugin: " + req.Plugin})
			return
		}
		result := simulate(req)
		writeJSON(w, http.StatusOK, result)
		return
	}

	if wantCluster && !clusterAvailable {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cluster is not reachable"})
		return
	}

	// ---- Real pipeline execution ----
	result, err := runRealJob(coordAddr, masterAddr, inputDir, req)
	if err != nil {
		log.Printf("[simulate] runRealJob failed: %v", err)
		if wantCluster {
			// User explicitly requested cluster — surface the error.
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}
		// Auto-detect mode: fall back to math simulation.
		log.Printf("[simulate] falling back to math simulation")
		result = simulate(req)
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// Real pipeline helpers
// ---------------------------------------------------------------------------

// runRealJob executes one full MapReduce pipeline for a given plugin.
func runRealJob(coordAddr, masterAddr, inputDir string, req SimulationRequest) (SimulationResult, error) {
	startTime := time.Now()

	var inputFiles []string
	if len(req.Files) > 0 {
		// Resolve user-supplied file names from upload or input directories.
		inputFiles = resolveFileNames(req.Files, inputDir, uploadDir())
	} else {
		var err error
		inputFiles, err = discoverInputFiles(inputDir)
		if err != nil || len(inputFiles) == 0 {
			return SimulationResult{}, fmt.Errorf("no input files in %s: %v", inputDir, err)
		}
	}
	if len(inputFiles) == 0 {
		return SimulationResult{}, fmt.Errorf("no matching files found for the supplied file list")
	}

	handles, totalBytes, err := uploadFilesToCoordinator(coordAddr, inputFiles)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("upload: %w", err)
	}
	uploadDone := time.Now()

	var triggerReply TriggerProcessingReply
	if err := rpcCall(coordAddr, "Coordinator.RPCTriggerProcessing",
		TriggerProcessingRequest{Plugin: req.Plugin}, &triggerReply); err != nil {
		return SimulationResult{}, fmt.Errorf("trigger: %w", err)
	}
	if triggerReply.Status == "error" {
		return SimulationResult{}, fmt.Errorf("trigger: %s", triggerReply.ErrorMessage)
	}
	dispatchDone := time.Now()

	results, err := pollForResults(masterAddr, handles, 120*time.Second)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("poll: %w", err)
	}
	endTime := time.Now()

	return buildRealResult(req, handles, totalBytes, results, startTime, uploadDone, dispatchDone, endTime), nil
}

const uploadChunkSize = 64 * 1024 // 64 KB — matches uploader CLI

func discoverInputFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "pg-") && strings.HasSuffix(e.Name(), ".txt") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// resolveFileNames maps user-supplied filenames to absolute paths, searching
// in the input and upload directories. Only files that actually exist are returned.
func resolveFileNames(names []string, dirs ...string) []string {
	var resolved []string
	for _, name := range names {
		base := filepath.Base(name) // strip any path prefix for safety
		for _, dir := range dirs {
			candidate := filepath.Join(dir, base)
			if _, err := os.Stat(candidate); err == nil {
				resolved = append(resolved, candidate)
				break
			}
		}
	}
	return resolved
}

func uploadFilesToCoordinator(coordAddr string, files []string) ([]Handle, int64, error) {
	var handles []Handle
	var totalBytes int64

	for _, path := range files {
		client, err := rpc.Dial("tcp", coordAddr)
		if err != nil {
			return nil, 0, fmt.Errorf("dial coordinator: %w", err)
		}

		// Generate handle.
		var hReply HandleReply
		if err := client.Call("Coordinator.RPCGenerateDownloadHandle", HandleRequest{}, &hReply); err != nil {
			client.Close()
			return nil, 0, fmt.Errorf("generate handle for %s: %w", filepath.Base(path), err)
		}
		if hReply.ErrorCode != 0 {
			client.Close()
			return nil, 0, fmt.Errorf("generate handle error: %s", hReply.ErrorMessage)
		}
		handle := hReply.Handle

		// Stream file content.
		f, err := os.Open(path)
		if err != nil {
			client.Close()
			return nil, 0, fmt.Errorf("open %s: %w", path, err)
		}

		buf := make([]byte, uploadChunkSize)
		for {
			n, readErr := f.Read(buf)
			eof := readErr == io.EOF
			if n > 0 || eof {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				var dReply DownloadReply
				if err := client.Call("Coordinator.RPCForwardDownload", DownloadRequest{
					Handle: handle,
					Data:   chunk,
					Eof:    eof,
				}, &dReply); err != nil {
					f.Close()
					client.Close()
					return nil, 0, fmt.Errorf("forward download for %s: %w", filepath.Base(path), err)
				}
				totalBytes += int64(n)
			}
			if eof || (readErr != nil && readErr != io.EOF) {
				break
			}
		}
		f.Close()
		client.Close()

		handles = append(handles, handle)
	}

	return handles, totalBytes, nil
}

// collatedResult represents the JSON structure written by master.
type collatedResult struct {
	Handle         uint64 `json:"handle"`
	ChunkCount     int    `json:"chunk_count"`
	CombinedOutput string `json:"combined_output"`
	LastUpdatedAt  string `json:"last_updated_at"`
	Chunks         []struct {
		ChunkIndex  int    `json:"chunk_index"`
		WorkerAddr  string `json:"worker_addr"`
		OutputFile  string `json:"output_file"`
		OutputData  string `json:"output_data"`
		GeneratedAt string `json:"generated_at"`
	} `json:"chunks"`
}

func pollForResults(masterAddr string, handles []Handle, timeout time.Duration) ([]collatedResult, error) {
	deadline := time.Now().Add(timeout)
	results := make([]collatedResult, 0, len(handles))
	pending := make(map[uint64]bool, len(handles))
	for _, h := range handles {
		pending[h.Id] = true
	}

	for len(pending) > 0 {
		if time.Now().After(deadline) {
			return results, fmt.Errorf("timed out waiting for %d handles", len(pending))
		}

		for hid := range pending {
			var reply GetJobResultReply
			if err := rpcCall(masterAddr, "Master.RPCGetJobResult", GetJobResultRequest{HandleId: hid}, &reply); err != nil {
				// Transient RPC failure — retry after delay.
				continue
			}
			if reply.Complete {
				var cr collatedResult
				if err := json.Unmarshal([]byte(reply.ResultJSON), &cr); err == nil {
					results = append(results, cr)
				}
				delete(pending, hid)
			}
		}

		if len(pending) > 0 {
			time.Sleep(1 * time.Second)
		}
	}

	return results, nil
}

func buildRealResult(
	req SimulationRequest,
	handles []Handle,
	totalBytes int64,
	results []collatedResult,
	startTime, uploadDone, dispatchDone, endTime time.Time,
) SimulationResult {
	totalMs := float64(endTime.Sub(startTime).Milliseconds())
	uploadMs := float64(uploadDone.Sub(startTime).Milliseconds())
	dispatchMs := float64(dispatchDone.Sub(uploadDone).Milliseconds())
	processMs := float64(endTime.Sub(dispatchDone).Milliseconds())
	inputMB := float64(totalBytes) / (1024 * 1024)

	// Aggregate per-worker stats from chunk results.
	workerMap := make(map[string]*WorkerMetrics)
	var chunkLatencies []float64
	for _, cr := range results {
		for _, ch := range cr.Chunks {
			wm, ok := workerMap[ch.WorkerAddr]
			if !ok {
				wm = &WorkerMetrics{WorkerID: len(workerMap) + 1}
				workerMap[ch.WorkerAddr] = wm
			}
			wm.TasksCompleted++
			// Parse chunk generation time for latency estimates.
			if t, err := time.Parse(time.RFC3339Nano, ch.GeneratedAt); err == nil {
				lat := float64(t.Sub(dispatchDone).Milliseconds())
				if lat < 0 {
					lat = 0
				}
				chunkLatencies = append(chunkLatencies, lat)
			}
		}
	}

	workers := make([]WorkerMetrics, 0, len(workerMap))
	for _, wm := range workerMap {
		if totalMs > 0 {
			wm.TotalTimeMs = processMs * float64(wm.TasksCompleted) / math.Max(1, float64(len(chunkLatencies)))
			wm.Utilization = math.Min(wm.TotalTimeMs/processMs, 1.0)
		}
		workers = append(workers, *wm)
	}
	sort.Slice(workers, func(i, j int) bool { return workers[i].WorkerID < workers[j].WorkerID })

	sort.Float64s(chunkLatencies)
	latency := computeLatencyDistribution(chunkLatencies)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	profile := pluginProfiles[req.Plugin]

	throughputTS := generateThroughputTimeSeries(rng, totalMs, inputMB, profile)
	latencyTS := generateLatencyTimeSeries(rng, totalMs, chunkLatencies, profile)
	cpuTS := generateCpuTimeSeries(rng, totalMs, len(workers), profile)

	mapPhaseDuration := uploadMs + dispatchMs
	if processMs > 0 {
		mapPhaseDuration = processMs * 0.6 // rough split: 60% map, 40% reduce
	}
	reducePhaseDuration := processMs - mapPhaseDuration

	return SimulationResult{
		Plugin: req.Plugin,
		Config: SimulationRequest{
			Plugin:           req.Plugin,
			InputSizeMB:      math.Round(inputMB*100) / 100, // actual bytes, not slider
			NumWorkers:       len(workers),                  // actual discovered workers
			NumChunks:        len(chunkLatencies),
			NetworkLatencyMs: req.NetworkLatencyMs,
			Iterations:       req.Iterations,
			Files:            req.Files,
		},
		TotalDurationMs: totalMs,
		MapPhase: PhaseMetrics{
			DurationMs: mapPhaseDuration,
			Throughput: safeDiv(inputMB, mapPhaseDuration/1000),
		},
		ShufflePhase: PhaseMetrics{
			DurationMs: dispatchMs,
			Throughput: safeDiv(inputMB, dispatchMs/1000),
		},
		ReducePhase: PhaseMetrics{
			DurationMs: reducePhaseDuration,
			Throughput: safeDiv(inputMB, reducePhaseDuration/1000),
		},
		Throughput:         safeDiv(inputMB, totalMs/1000),
		Latency:            latency,
		Workers:            workers,
		ThroughputOverTime: throughputTS,
		LatencyOverTime:    latencyTS,
		CpuOverTime:        cpuTS,
		TotalCrashes:       0,
		RecoveryTimeMs:     0,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		Mode:               "cluster",
	}
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func handleCompare(w http.ResponseWriter, r *http.Request) {
	var req CompareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if len(req.Plugins) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one plugin required"})
		return
	}

	coordAddr := envOr("MAPREDUCE_COORDINATOR_ADDRESS", "")
	masterAddr := envOr("MAPREDUCE_MASTER_ADDRESS", "")
	inputDir := envOr("MAPREDUCE_INPUT_DIR", "./inputs")

	useCluster := coordAddr != "" && masterAddr != "" && rpcPing(coordAddr)

	var results []SimulationResult

	// Run each plugin sequentially to avoid handle confusion on the coordinator.
	for _, plugin := range req.Plugins {
		if useCluster {
			result, err := runRealJob(coordAddr, masterAddr, inputDir, SimulationRequest{
				Plugin:           plugin,
				InputSizeMB:      req.InputSizeMB,
				NumWorkers:       req.NumWorkers,
				NumChunks:        req.NumChunks,
				NetworkLatencyMs: req.NetworkLatencyMs,
				Iterations:       1,
			})
			if err != nil {
				// Fall back to simulation for this plugin on error.
				log.Printf("[compare] real exec failed for %s, falling back to sim: %v", plugin, err)
				result = simulate(SimulationRequest{
					Plugin:           plugin,
					InputSizeMB:      req.InputSizeMB,
					NumWorkers:       req.NumWorkers,
					NumChunks:        req.NumChunks,
					NetworkLatencyMs: req.NetworkLatencyMs,
					Iterations:       1,
				})
			}
			results = append(results, result)
		} else {
			simReq := SimulationRequest{
				Plugin:           plugin,
				InputSizeMB:      req.InputSizeMB,
				NumWorkers:       req.NumWorkers,
				NumChunks:        req.NumChunks,
				NetworkLatencyMs: req.NetworkLatencyMs,
				Iterations:       1,
			}
			results = append(results, simulate(simReq))
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalDurationMs < results[j].TotalDurationMs
	})

	writeJSON(w, http.StatusOK, CompareResult{Results: results})
}

func handleSimulateStream(w http.ResponseWriter, r *http.Request) {
	var req SimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	iterations := req.Iterations
	if iterations <= 0 {
		iterations = 10
	}

	for i := 0; i < iterations; i++ {
		result := simulate(req)
		data, _ := json.Marshal(result)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// Cluster status & trigger handlers
// ---------------------------------------------------------------------------

func handleStatus(w http.ResponseWriter, r *http.Request) {
	coordAddr := envOr("MAPREDUCE_COORDINATOR_ADDRESS", "")
	masterAddr := envOr("MAPREDUCE_MASTER_ADDRESS", "")
	workerAddrs := strings.Split(envOr("MAPREDUCE_WORKER_ADDRESSES", ""), ",")

	status := ClusterStatus{
		Coordinator: ServiceStatus{
			Name:    "coordinator",
			Address: coordAddr,
			Online:  coordAddr != "" && rpcPing(coordAddr),
		},
		Master: ServiceStatus{
			Name:    "master",
			Address: masterAddr,
			Online:  masterAddr != "" && rpcPing(masterAddr),
		},
		ConnectedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, addr := range workerAddrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		status.Workers = append(status.Workers, ServiceStatus{
			Name:    "worker",
			Address: addr,
			Online:  rpcPing(addr),
		})
	}

	writeJSON(w, http.StatusOK, status)
}

func handleTrigger(w http.ResponseWriter, r *http.Request) {
	coordAddr := envOr("MAPREDUCE_COORDINATOR_ADDRESS", "")
	if coordAddr == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "coordinator address not configured",
		})
		return
	}

	if !rpcPing(coordAddr) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "coordinator is not reachable at " + coordAddr,
		})
		return
	}

	writeJSON(w, http.StatusOK, TriggerReplyMsg{
		Status:  "ok",
		Message: "Coordinator is online at " + coordAddr + ". Use the uploader CLI to submit files.",
	})
}

// ---------------------------------------------------------------------------
// File upload & listing handlers
// ---------------------------------------------------------------------------

func uploadDir() string { return envOr("MAPREDUCE_UPLOAD_DIR", "./uploads") }

func handleUpload(w http.ResponseWriter, r *http.Request) {
	const maxUpload = 200 << 20 // 200 MB
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "failed to parse multipart form: " + err.Error(),
		})
		return
	}

	dir := uploadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "cannot create upload directory: " + err.Error(),
		})
		return
	}

	var uploaded []FileInfo
	for _, fh := range r.MultipartForm.File["files"] {
		src, err := fh.Open()
		if err != nil {
			continue
		}

		// Sanitize filename — strip path components.
		name := filepath.Base(fh.Filename)
		if name == "" || name == "." || name == ".." {
			src.Close()
			continue
		}

		dstPath := filepath.Join(dir, name)
		dst, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			continue
		}
		written, _ := io.Copy(dst, src)
		src.Close()
		dst.Close()

		uploaded = append(uploaded, FileInfo{
			Name:   name,
			Size:   written,
			Source: "upload",
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "uploaded",
		"files":  uploaded,
	})
}

type FileInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Source string `json:"source"` // "input" or "upload"
}

func handleFiles(w http.ResponseWriter, r *http.Request) {
	inputDir := envOr("MAPREDUCE_INPUT_DIR", "./inputs")
	upDir := uploadDir()

	var files []FileInfo

	// List built-in input files.
	listDirFiles(inputDir, "input", &files)
	// List uploaded files.
	listDirFiles(upDir, "upload", &files)

	writeJSON(w, http.StatusOK, files)
}

func listDirFiles(dir, source string, out *[]FileInfo) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		*out = append(*out, FileInfo{
			Name:   e.Name(),
			Size:   info.Size(),
			Source: source,
		})
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/plugins", handlePlugins)
	mux.HandleFunc("POST /api/simulate", handleSimulate)
	mux.HandleFunc("POST /api/simulate/stream", handleSimulateStream)
	mux.HandleFunc("POST /api/compare", handleCompare)
	mux.HandleFunc("GET /api/status", handleStatus)
	mux.HandleFunc("POST /api/trigger", handleTrigger)
	mux.HandleFunc("POST /api/upload", handleUpload)
	mux.HandleFunc("GET /api/files", handleFiles)

	// CORS middleware
	handler := corsMiddleware(mux)

	addr := envOr("DASHBOARD_LISTEN_ADDR", ":4400")
	log.Printf("Dashboard API server listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
