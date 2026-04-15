package mrp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	dfscommon "github.com/caleberi/distributed-system/common"
	"github.com/caleberi/distributed-system/hercules"
	"github.com/rs/zerolog"
)

const (
	defaultDispatchTimeout = 30 * time.Second

	MAPREDUCE_DFS_SERVER_ADDRESS  = "MAPREDUCE_DFS_SERVER_ADDRESS"
	MAPREDUCE_COORDINATOR_ADDRESS = "MAPREDUCE_COORDINATOR_ADDRESS"
	MAPREDUCE_WORKER_ADDRESSES    = "MAPREDUCE_WORKER_ADDRESSES"
	MAPREDUCE_MASTER_RESULT_DIR   = "MAPREDUCE_MASTER_RESULT_DIR"
)

type jobMeta struct {
	expectedChunks int
	dispatchedAt   time.Time
}

type Master struct {
	serverAddress      string
	fsServerAddress    string
	coordinatorAddress string
	workerAddresses    []string
	resultMux          sync.Mutex
	results            map[string]MapReduceResultRequest
	jobsMu             sync.Mutex
	jobs               map[uint64]jobMeta // handle ID → expected chunk count
	wg                 sync.WaitGroup
	startOnce          sync.Once
	stopOnce           sync.Once
	startErr           error
	stopCh             chan struct{}

	// Plugin-aware worker routing.
	workerPlugins   map[string]string // worker addr → plugin name (cached)
	workerPluginsMu sync.Mutex

	logger    zerolog.Logger
	listener  Listener
	dfsClient *hercules.HerculesClient // lazily initialized, reused
}

func NewMaster(serverAddress string) (*Master, error) {
	workerAddresses := parseWorkerAddresses(envOrDefault(MAPREDUCE_WORKER_ADDRESSES, ""))

	master := &Master{
		serverAddress:      serverAddress,
		fsServerAddress:    envOrDefault(MAPREDUCE_DFS_SERVER_ADDRESS, "localhost:8089"),
		coordinatorAddress: envOrDefault(MAPREDUCE_COORDINATOR_ADDRESS, ""),
		workerAddresses:    workerAddresses,
		results:            make(map[string]MapReduceResultRequest),
		jobs:               make(map[uint64]jobMeta),
		workerPlugins:      make(map[string]string),
		logger:             zerolog.New(os.Stdout),
		stopCh:             make(chan struct{}),
	}

	rpcServer := rpc.NewServer()
	if err := rpcServer.Register(master); err != nil {
		return nil, err
	}

	master.listener = NewRPCListener(
		serverAddress,
		func(conn net.Conn) {
			logger := master.logger.With().Str(
				"remote_addr", conn.RemoteAddr().String()).Logger()
			logger.Info().Msg("[x] accepted new connection")
			logger.Info().Msgf("|- network type: %s", conn.RemoteAddr().Network())
			logger.Info().Msgf("|- local address: %s", conn.LocalAddr().String())

			rpcServer.ServeConn(conn)
		})

	return master, nil
}

func parseWorkerAddresses(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		address := strings.TrimSpace(part)
		if address == "" {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, address)
	}

	return result
}

func (m *Master) Start() error {
	m.startOnce.Do(func() {
		if err := m.listener.Listen(); err != nil {
			m.startErr = err
			return
		}

		errs := m.listener.Errors()
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			for {
				select {
				case <-m.stopCh:
					return
				case err, ok := <-errs:
					if !ok {
						return
					}
					if err != nil {
						m.logger.Error().Err(err).Msg("master RPC listener error")
					}
				}
			}
		}()
	})

	return m.startErr
}

func (m *Master) Close() error {
	var closeErr error

	m.stopOnce.Do(func() {
		close(m.stopCh)
		if m.listener != nil {
			if err := m.listener.Close(); err != nil {
				closeErr = fmt.Errorf("failed to close master RPC listener: %w", err)
			}
		}
		m.wg.Wait()
	})

	return closeErr
}

func (m *Master) Heartbeat(request HeartbeatRequest, reply *HeartbeatReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	reply.Status = "ok"
	reply.ServerUTC = time.Now().UTC().Format(time.RFC3339Nano)
	if request.WorkerID == "" {
		reply.Message = "heartbeat received"
		return nil
	}

	if request.Message == "" {
		reply.Message = fmt.Sprintf("heartbeat received from %s", request.WorkerID)
		return nil
	}

	reply.Message = fmt.Sprintf("heartbeat received from %s: %s", request.WorkerID, request.Message)
	return nil
}

func (m *Master) RPCStartMapReduce(request StartMapReduceRequest, reply *StartMapReduceReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	m.logger.Info().
		Uint64("handle_id", request.Handle.Id).
		Int64("timestamp", request.Handle.TimeStamp).
		Msg("received map reduce start request")

	ctx, cancel := context.WithTimeout(context.Background(), defaultDispatchTimeout)
	defer cancel()

	fileMetadata, err := m.fetchFileMetadataFromDFS(ctx, request.Handle)
	if err != nil {
		reply.Status = "error"
		reply.ErrorCode = 500
		reply.ErrorMessage = fmt.Sprintf("failed to fetch file metadata: %v", err)
		m.logger.Error().Err(err).Msg("failed to fetch file metadata from DFS")
		return nil
	}

	chunks := fileMetadata.Chunks
	if len(chunks) == 0 {
		reply.Status = "error"
		reply.ErrorCode = 500
		reply.ErrorMessage = "no chunks available from DFS metadata"
		m.logger.Error().Msg("no chunks available for map reduce")
		return nil
	}

	if len(m.workerAddresses) == 0 {
		reply.Status = "error"
		reply.ErrorCode = 503
		reply.ErrorMessage = "no workers available"
		m.logger.Warn().Msg("no workers available for map reduce")
		return nil
	}

	targetWorkers := m.workerAddresses
	if plugin := strings.TrimSpace(request.Plugin); plugin != "" {
		targetWorkers = m.getWorkersForPlugin(plugin)
		if len(targetWorkers) == 0 {
			reply.Status = "error"
			reply.ErrorCode = 503
			reply.ErrorMessage = fmt.Sprintf("no workers available for plugin %q", plugin)
			m.logger.Warn().Str("plugin", plugin).Msg("no workers found for requested plugin")
			return nil
		}
	}

	// Record expected chunk count so RPCSubmitMapReduceResult can detect completion.
	m.jobsMu.Lock()
	m.jobs[request.Handle.Id] = jobMeta{
		expectedChunks: len(chunks),
		dispatchedAt:   time.Now(),
	}
	m.jobsMu.Unlock()

	// Dispatch chunks to workers concurrently.
	type dispatchResult struct {
		chunkIndex int
		err        error
	}
	resultsCh := make(chan dispatchResult, len(chunks))

	var dwg sync.WaitGroup
	for i, chunk := range chunks {
		dwg.Add(1)
		go func(idx int, ci ChunkInfo) {
			defer dwg.Done()

			select {
			case <-ctx.Done():
				resultsCh <- dispatchResult{chunkIndex: idx, err: ctx.Err()}
				return
			default:
			}

			workerAddr := targetWorkers[idx%len(targetWorkers)]
			client, dialErr := rpc.Dial("tcp", workerAddr)
			if dialErr != nil {
				m.logger.Error().Err(dialErr).Str("worker", workerAddr).Int("chunk", idx).Msg("failed to connect to worker")
				resultsCh <- dispatchResult{chunkIndex: idx, err: dialErr}
				return
			}

			mapReduceReq := MapReduceRequest{
				Handle:     request.Handle,
				File:       *fileMetadata,
				ChunkIndex: idx,
				ChunkInfo:  ci,
			}

			var mapReduceReply MapReduceReply
			callErr := client.Call("Worker.RPCMapReduce", mapReduceReq, &mapReduceReply)
			client.Close()

			if callErr != nil {
				m.logger.Error().Err(callErr).Str("worker", workerAddr).Int("chunk", idx).Msg("worker map reduce call failed")
				resultsCh <- dispatchResult{chunkIndex: idx, err: callErr}
				return
			}

			if mapReduceReply.Status == "error" {
				m.logger.Error().Str("worker", workerAddr).Int("chunk", idx).Str("error", mapReduceReply.ErrorMessage).Msg("worker returned error")
				resultsCh <- dispatchResult{chunkIndex: idx, err: fmt.Errorf("worker %s chunk %d: %s", workerAddr, idx, mapReduceReply.ErrorMessage)}
				return
			}

			m.logger.Info().Str("worker", workerAddr).Int("chunk", idx).Str("status", mapReduceReply.Status).Msg("dispatched chunk to worker")
			resultsCh <- dispatchResult{chunkIndex: idx}
		}(i, chunk)
	}
	dwg.Wait()
	close(resultsCh)

	var dispatched, failed int
	var dispatchErr error
	for dr := range resultsCh {
		if dr.err != nil {
			failed++
			dispatchErr = errors.Join(dispatchErr, dr.err)
		} else {
			dispatched++
		}
	}

	if dispatched == 0 {
		// All chunks failed — clean up job tracker so coordinator can retry.
		m.jobsMu.Lock()
		delete(m.jobs, request.Handle.Id)
		m.jobsMu.Unlock()

		reply.Status = "error"
		reply.ErrorCode = 500
		reply.ErrorMessage = fmt.Sprintf("all %d chunk dispatches failed: %v", len(chunks), dispatchErr)
		m.logger.Error().Err(dispatchErr).Msg("all chunk dispatches failed")
		return nil
	}

	if failed > 0 {
		reply.Status = "partial"
		reply.ErrorCode = 207
		reply.ErrorMessage = fmt.Sprintf("%d of %d chunks failed to dispatch", failed, len(chunks))
		reply.Message = fmt.Sprintf("map reduce partially dispatched for handle %d: %d/%d succeeded", request.Handle.Id, dispatched, len(chunks))
		m.logger.Warn().Int("dispatched", dispatched).Int("failed", failed).Msg("partial chunk dispatch")
		return nil
	}

	reply.Status = "accepted"
	reply.Message = fmt.Sprintf("map reduce dispatched for handle %d with %d chunks", request.Handle.Id, len(chunks))
	return nil
}

func (m *Master) getOrCreateDFSClient(ctx context.Context) *hercules.HerculesClient {
	if m.dfsClient != nil {
		return m.dfsClient
	}
	m.dfsClient = hercules.NewHerculesClient(
		ctx, dfscommon.ServerAddr(strings.TrimSpace(m.fsServerAddress)),
		1*time.Minute)
	return m.dfsClient
}

func (m *Master) fetchFileMetadataFromDFS(ctx context.Context, handle Handle) (*FileMetadata, error) {
	if strings.TrimSpace(m.fsServerAddress) == "" {
		return nil, fmt.Errorf("DFS server address not configured")
	}

	dfsClient := m.getOrCreateDFSClient(ctx)
	logFile := fmt.Sprintf("download_log_%d-%d", handle.Id, handle.TimeStamp)
	prefixes := resolveUploadPrefixes()

	var fetchErr error
	for _, remotePrefix := range prefixes {
		remoteFile := strings.TrimRight(remotePrefix, "/") + "/" + logFile + ".json"
		contentFile := strings.TrimRight(remotePrefix, "/") + "/" + logFile + ".content"
		contentDfsPath := dfscommon.Path(contentFile)

		contentInfo, err := dfsClient.GetFile(contentDfsPath)
		if err != nil || contentInfo == nil || contentInfo.Length == 0 {
			fetchErr = errors.Join(fetchErr, fmt.Errorf("%s: content file not found", contentFile))

			dfsPath := dfscommon.Path(remoteFile)
			fileInfo, jsonErr := dfsClient.GetFile(dfsPath)
			if jsonErr != nil {
				fetchErr = errors.Join(fetchErr, fmt.Errorf("%s: %w", remoteFile, jsonErr))
				continue
			}
			if fileInfo == nil {
				fetchErr = errors.Join(fetchErr, fmt.Errorf("DFS returned empty file info for %s", remoteFile))
				continue
			}

			metadata := &FileMetadata{
				Handle:     handle,
				SourceLog:  logFile + ".json",
				SourcePath: remoteFile,
				SizeBytes:  fileInfo.Length,
				Completed:  true,
			}
			metadata.Chunks = []ChunkInfo{{
				Index:  0,
				Offset: 0,
				Size:   fileInfo.Length,
				Path:   remoteFile,
			}}
			return metadata, nil
		}

		metadata := &FileMetadata{
			Handle:     handle,
			SourceLog:  logFile + ".content",
			SourcePath: contentFile,
			SizeBytes:  contentInfo.Length,
			Completed:  true,
		}

		if contentInfo.Chunks <= 0 || contentInfo.Length == 0 {
			metadata.Chunks = []ChunkInfo{{
				Index:  0,
				Offset: 0,
				Size:   0,
				Path:   contentFile,
			}}
			return metadata, nil
		}

		chunks := make([]ChunkInfo, 0, contentInfo.Chunks)
		for index := int64(0); index < contentInfo.Chunks; index++ {
			chunkHandle, err := dfsClient.GetChunkHandle(contentDfsPath, dfscommon.ChunkIndex(index))
			if err != nil {
				return nil, fmt.Errorf("failed to fetch chunk handle for %s at index %d: %w", contentFile, index, err)
			}

			offset := index * int64(dfscommon.ChunkMaxSizeInByte)
			size := int64(dfscommon.ChunkMaxSizeInByte)
			if remaining := contentInfo.Length - offset; remaining < size {
				size = max(remaining, 0)
			}

			chunks = append(chunks, ChunkInfo{
				Index:  int(index),
				Offset: offset,
				Size:   size,
				Path:   contentFile,
				Handle: int64(chunkHandle),
			})
		}
		metadata.Chunks = chunks
		return metadata, nil
	}

	if fetchErr != nil {
		return nil, fmt.Errorf("failed to fetch metadata file info from DFS: %w", fetchErr)
	}

	return nil, fmt.Errorf("failed to fetch metadata file info from DFS")
}

func resolveUploadPrefixes() []string {
	configured := strings.TrimSpace(envOrDefault(MAPREDUCE_DFS_UPLOAD_PREFIX, "/mapreduce"))
	prefixes := []string{configured, "/mapreduce", "/mapreduce/download_logs"}

	result := make([]string, 0, len(prefixes))
	seen := make(map[string]struct{}, len(prefixes))
	for _, prefix := range prefixes {
		cleaned := strings.TrimSpace(prefix)
		if cleaned == "" {
			continue
		}
		cleaned = "/" + strings.Trim(strings.TrimPrefix(cleaned, "/"), " ")
		cleaned = strings.TrimRight(cleaned, "/")
		if cleaned == "" {
			cleaned = "/"
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}

	return result
}

func (m *Master) RPCSubmitMapReduceResult(request MapReduceResultRequest, reply *MapReduceResultReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	resultKey := fmt.Sprintf("%d:%d", request.Handle.Id, request.ChunkIndex)

	m.logger.Info().
		Uint64("handle_id", request.Handle.Id).
		Int("chunk_index", request.ChunkIndex).
		Str("worker", request.WorkerAddr).
		Str("output_file", request.OutputFile).
		Msg("received map reduce output from worker")

	m.resultMux.Lock()
	m.results[resultKey] = request

	m.jobsMu.Lock()
	job, tracked := m.jobs[request.Handle.Id]
	m.jobsMu.Unlock()

	if !tracked {
		m.resultMux.Unlock()
		reply.Status = "success"
		reply.Message = "result accepted (job not tracked)"
		return nil
	}

	prefix := fmt.Sprintf("%d:", request.Handle.Id)
	received := 0
	for key := range m.results {
		if strings.HasPrefix(key, prefix) {
			received++
		}
	}

	if received < job.expectedChunks {
		m.resultMux.Unlock()
		reply.Status = "success"
		reply.Message = fmt.Sprintf("result accepted (%d/%d chunks received)", received, job.expectedChunks)
		return nil
	}

	type chunkResult struct {
		ChunkIndex  int    `json:"chunk_index"`
		WorkerAddr  string `json:"worker_addr"`
		OutputFile  string `json:"output_file"`
		OutputData  string `json:"output_data"`
		GeneratedAt string `json:"generated_at"`
	}
	type collatedResult struct {
		Handle         Handle        `json:"handle"`
		ChunkCount     int           `json:"chunk_count"`
		Chunks         []chunkResult `json:"chunks"`
		CombinedOutput string        `json:"combined_output"`
		LastUpdatedAt  string        `json:"last_updated_at"`
	}

	chunks := make([]chunkResult, 0, received)
	for key, item := range m.results {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		chunks = append(chunks, chunkResult{
			ChunkIndex:  item.ChunkIndex,
			WorkerAddr:  item.WorkerAddr,
			OutputFile:  item.OutputFile,
			OutputData:  item.OutputData,
			GeneratedAt: item.GeneratedAt,
		})
	}
	m.resultMux.Unlock()

	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].ChunkIndex < chunks[j].ChunkIndex
	})

	combined := strings.Builder{}
	ForEach(chunks, func(i int, chunk chunkResult) {
		if i > 0 && !strings.HasSuffix(combined.String(), "\n") {
			combined.WriteString("\n")
		}
		combined.WriteString(chunk.OutputData)
		if !strings.HasSuffix(chunk.OutputData, "\n") {
			combined.WriteString("\n")
		}
	})

	payload := collatedResult{
		Handle:         request.Handle,
		ChunkCount:     len(chunks),
		Chunks:         chunks,
		CombinedOutput: combined.String(),
		LastUpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}

	resultDir := envOrDefault(MAPREDUCE_MASTER_RESULT_DIR, "./results")
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		m.logger.Error().Err(err).Str("dir", resultDir).Msg("failed to create result directory")
		reply.Status = "error"
		reply.ErrorCode = 500
		reply.ErrorMessage = fmt.Sprintf("failed to create result directory: %v", err)
		return nil
	}

	resultFile := filepath.Join(resultDir, fmt.Sprintf("map_reduce_result_%d.json", request.Handle.Id))
	tmpFile := resultFile + ".tmp"
	file, err := os.Create(tmpFile)
	if err != nil {
		m.logger.Error().Err(err).Str("file", tmpFile).Msg("failed to create temp result file")
		reply.Status = "error"
		reply.ErrorCode = 500
		reply.ErrorMessage = fmt.Sprintf("failed to create temp result file: %v", err)
		return nil
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		file.Close()
		os.Remove(tmpFile)
		m.logger.Error().Err(err).Str("file", tmpFile).Msg("failed to write collated result file")
		reply.Status = "error"
		reply.ErrorCode = 500
		reply.ErrorMessage = fmt.Sprintf("failed to write collated result file: %v", err)
		return nil
	}
	file.Close()

	if err := os.Rename(tmpFile, resultFile); err != nil {
		os.Remove(tmpFile)
		m.logger.Error().Err(err).Str("file", resultFile).Msg("failed to rename result file")
		reply.Status = "error"
		reply.ErrorCode = 500
		reply.ErrorMessage = fmt.Sprintf("failed to finalize result file: %v", err)
		return nil
	}

	m.jobsMu.Lock()
	delete(m.jobs, request.Handle.Id)
	m.jobsMu.Unlock()

	m.logger.Info().
		Uint64("handle_id", request.Handle.Id).
		Int("chunk_count", len(chunks)).
		Str("result_file", resultFile).
		Msg("all chunks received, collated result written")

	m.notifyCoordinator(request.Handle, resultFile)

	reply.Status = "success"
	reply.Message = fmt.Sprintf("all %d chunks collated for handle %d", len(chunks), request.Handle.Id)
	return nil
}

func (m *Master) notifyCoordinator(handle Handle, resultFile string) {
	if strings.TrimSpace(m.coordinatorAddress) == "" {
		m.logger.Debug().Uint64("handle_id", handle.Id).
			Msg("coordinator address not configured, skipping completion notification")
		return
	}

	conn, err := net.DialTimeout("tcp", m.coordinatorAddress, 10*time.Second)
	if err != nil {
		m.logger.Warn().Err(err).Uint64("handle_id", handle.Id).
			Msg("failed to connect to coordinator for job completion notification")
		return
	}
	client := rpc.NewClient(conn)
	defer client.Close()

	req := JobCompleteRequest{
		Handle:     handle,
		ResultFile: resultFile,
		Status:     "completed",
		Message:    fmt.Sprintf("all chunks collated for handle %d", handle.Id),
	}
	var reply JobCompleteReply
	if err := client.Call("Coordinator.RPCNotifyJobComplete", req, &reply); err != nil {
		m.logger.Warn().Err(err).Uint64("handle_id", handle.Id).
			Msg("failed to notify coordinator of job completion")
	}
}

func (m *Master) getWorkersForPlugin(plugin string) []string {
	m.workerPluginsMu.Lock()
	defer m.workerPluginsMu.Unlock()

	for _, addr := range m.workerAddresses {
		if _, ok := m.workerPlugins[addr]; ok {
			continue
		}
		var reply PluginInfoReply
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			m.logger.Warn().Err(err).Str("worker", addr).Msg("cannot probe worker plugin")
			continue
		}
		client := rpc.NewClient(conn)
		if err := client.Call("Worker.RPCGetPluginInfo", PluginInfoRequest{}, &reply); err != nil {
			m.logger.Warn().Err(err).Str("worker", addr).Msg("RPCGetPluginInfo failed")
			client.Close()
			continue
		}
		client.Close()
		m.workerPlugins[addr] = reply.PluginName
		m.logger.Info().Str("worker", addr).Str("plugin", reply.PluginName).Msg("discovered worker plugin")
	}

	var matched []string
	for _, addr := range m.workerAddresses {
		if m.workerPlugins[addr] == plugin {
			matched = append(matched, addr)
		}
	}
	return matched
}

func (m *Master) RPCGetJobResult(request GetJobResultRequest, reply *GetJobResultReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	resultDir := envOrDefault(MAPREDUCE_MASTER_RESULT_DIR, "./results")
	resultFile := filepath.Join(resultDir, fmt.Sprintf("map_reduce_result_%d.json", request.HandleId))

	data, err := os.ReadFile(resultFile)
	if err != nil {
		if os.IsNotExist(err) {
			m.jobsMu.Lock()
			_, tracked := m.jobs[request.HandleId]
			m.jobsMu.Unlock()

			reply.Status = "ok"
			reply.Complete = false
			if tracked {
				reply.ErrorMessage = "job in progress"
			} else {
				reply.ErrorMessage = "job not found"
			}
			return nil
		}
		reply.Status = "error"
		reply.ErrorMessage = fmt.Sprintf("failed to read result: %v", err)
		return nil
	}

	reply.Status = "ok"
	reply.Complete = true
	reply.ResultJSON = string(data)
	return nil
}
