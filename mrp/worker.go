package mrp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"plugin"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dfscommon "github.com/caleberi/distributed-system/common"
	"github.com/caleberi/distributed-system/hercules"

	"github.com/rs/zerolog"
)

const (
	defaultWorkerMasterAddress = "localhost:1235"
	defaultIntermediateDir     = "./intermediate"
	defaultOutputDir           = "./output"
	defaultChunkReadSize       = 64 * 1024
	defaultSubmitTimeout       = 15 * time.Second
)

type Worker struct {
	serverAddress string
	masterAddress string
	fsServerAddr  string
	pluginName    string
	pluginPath    string
	nReduce       int
	intermediate  string
	outputDir     string

	mapf      func(string, string) []KeyValue
	reducef   func(string, []string) string
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once
	startErr  error
	stopCh    chan struct{}

	dfsClient   *hercules.HerculesClient // lazily initialized, reused
	dfsClientMu sync.Mutex

	logger   zerolog.Logger
	listener Listener
}

func NewWorker(serverAddress string) (*Worker, error) {
	nReduce := 1
	if parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MAPREDUCE_N_REDUCE"))); err == nil && parsed > 0 {
		nReduce = parsed
	}

	worker := &Worker{
		serverAddress: serverAddress,
		masterAddress: envOrDefault("MAPREDUCE_MASTER_SERVER_ADDRESS", defaultWorkerMasterAddress),
		fsServerAddr:  envOrDefault("MAPREDUCE_DFS_SERVER_ADDRESS", "localhost:8089"),
		pluginName:    strings.TrimSpace(os.Getenv("MAPREDUCE_PLUGIN_NAME")),
		pluginPath:    strings.TrimSpace(os.Getenv("MAPREDUCE_PLUGIN_PATH")),
		nReduce:       nReduce,
		intermediate:  envOrDefault("MAPREDUCE_WORKER_INTERMEDIATE_DIR", defaultIntermediateDir),
		outputDir:     envOrDefault("MAPREDUCE_WORKER_OUTPUT_DIR", defaultOutputDir),
		mapf:          defaultMap,
		reducef:       defaultReduce,
		logger:        zerolog.New(os.Stdout),
		stopCh:        make(chan struct{}),
	}

	rpcServer := rpc.NewServer()
	if err := rpcServer.Register(worker); err != nil {
		return nil, err
	}

	worker.listener = NewRPCListener(serverAddress, func(conn net.Conn) {
		rpcServer.ServeConn(conn)
	})

	return worker, nil
}

func (w *Worker) Start() error {
	w.startOnce.Do(func() {
		if err := w.loadMapReducePlugin(); err != nil {
			w.startErr = err
			return
		}

		if err := w.listener.Listen(); err != nil {
			w.startErr = err
			return
		}

		errs := w.listener.Errors()
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-w.stopCh:
					return
				case err, ok := <-errs:
					if !ok {
						return
					}
					if err != nil {
						w.logger.Error().Err(err).Msg("worker RPC listener error")
					}
				}
			}
		}()
	})

	return w.startErr
}

func (w *Worker) Close() error {
	var closeErr error

	w.stopOnce.Do(func() {
		close(w.stopCh)
		if w.listener != nil {
			if err := w.listener.Close(); err != nil {
				closeErr = fmt.Errorf("failed to close worker RPC listener: %w", err)
			}
		}
		w.wg.Wait()
	})

	return closeErr
}

func (w *Worker) Heartbeat(request HeartbeatRequest, reply *HeartbeatReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	reply.Status = "ok"
	reply.ServerUTC = time.Now().UTC().Format(time.RFC3339Nano)
	if request.WorkerID == "" {
		reply.Message = "worker heartbeat received"
		return nil
	}

	if request.Message == "" {
		reply.Message = fmt.Sprintf("worker heartbeat received from %s", request.WorkerID)
		return nil
	}

	reply.Message = fmt.Sprintf("worker heartbeat received from %s: %s", request.WorkerID, request.Message)
	return nil
}

// RPCGetPluginInfo reports which plugin this worker has loaded.
func (w *Worker) RPCGetPluginInfo(_ PluginInfoRequest, reply *PluginInfoReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}
	reply.PluginName = w.pluginName
	return nil
}

func (w *Worker) RPCMapReduce(request MapReduceRequest, reply *MapReduceReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	w.logger.Info().
		Uint64("handle_id", request.Handle.Id).
		Int("chunk_index", request.ChunkIndex).
		Int64("chunk_offset", request.ChunkInfo.Offset).
		Int64("chunk_size", request.ChunkInfo.Size).
		Str("file", request.File.SourceLog).
		Msg("received map reduce task")

	// Process asynchronously so the master dispatch goroutine is not blocked
	// for the full duration of map + reduce + result submission. (Fix #1)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-w.stopCh:
				cancel()
			case <-ctx.Done():
			}
		}()
		defer cancel()

		// Remove stale intermediate files from a previous attempt (Fix #9).
		w.cleanupIntermediateFiles(request.Handle.Id, request.ChunkIndex)

		chunkData, err := w.readChunkData(ctx, request)
		if err != nil {
			w.logger.Error().Err(err).
				Uint64("handle_id", request.Handle.Id).
				Int("chunk_index", request.ChunkIndex).
				Msg("failed to read chunk data")
			return
		}

		outputFile, outputData, err := w.runMapReducePipeline(request, chunkData)
		if err != nil {
			w.logger.Error().Err(err).
				Uint64("handle_id", request.Handle.Id).
				Int("chunk_index", request.ChunkIndex).
				Msg("failed map-reduce pipeline")
			return
		}

		// Clean up intermediate files after pipeline completes (Fix #4).
		w.cleanupIntermediateFiles(request.Handle.Id, request.ChunkIndex)

		if err := w.submitResultToMaster(request, outputFile, outputData); err != nil {
			w.logger.Error().Err(err).
				Uint64("handle_id", request.Handle.Id).
				Int("chunk_index", request.ChunkIndex).
				Msg("failed submitting result to master")
			return
		}

		w.logger.Info().
			Uint64("handle_id", request.Handle.Id).
			Int("chunk_index", request.ChunkIndex).
			Str("output_file", outputFile).
			Msg("completed map reduce task")
	}()

	reply.Status = "accepted"
	reply.Message = fmt.Sprintf("accepted chunk %d of handle %d for processing", request.ChunkIndex, request.Handle.Id)
	return nil
}

func (w *Worker) runMapReducePipeline(request MapReduceRequest, chunkData []byte) (string, string, error) {
	if err := os.MkdirAll(w.intermediate, 0o755); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(w.outputDir, 0o755); err != nil {
		return "", "", err
	}

	mapOutput := w.mapf(request.ChunkInfo.Path, string(chunkData))
	partitions := arrangeImmediate(mapOutput, w.nReduce)

	intermediateCh := make(chan string, len(partitions))
	writerErrCh := make(chan error, len(partitions))
	type reduceResult struct {
		outputFile string
		outputData string
		err        error
	}
	reduceResultCh := make(chan reduceResult, 1)

	go func() {
		outputFile, outputData, err := w.runReduceFromChannel(request, intermediateCh)
		reduceResultCh <- reduceResult{outputFile: outputFile, outputData: outputData, err: err}
	}()

	var wg sync.WaitGroup
	for reduceIndex := range partitions {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			partitionFile := filepath.Join(w.intermediate, fmt.Sprintf("mr-%d-%d-%d.json", request.Handle.Id, request.ChunkIndex, index))
			if err := writeIntermediatePartition(partitionFile, partitions[index]); err != nil {
				writerErrCh <- err
				return
			}
			intermediateCh <- partitionFile
		}(reduceIndex)
	}

	wg.Wait()
	close(intermediateCh)
	close(writerErrCh)

	var writerErr error
	for err := range writerErrCh {
		writerErr = errors.Join(writerErr, err)
	}

	reduceRes := <-reduceResultCh
	if writerErr != nil || reduceRes.err != nil {
		return "", "", errors.Join(writerErr, reduceRes.err)
	}

	return reduceRes.outputFile, reduceRes.outputData, nil
}

func (w *Worker) loadMapReducePlugin() error {
	resolvedPath := strings.TrimSpace(w.pluginPath)
	if resolvedPath == "" {
		name := strings.TrimSpace(w.pluginName)
		if name == "" {
			return nil
		}
		baseName := strings.TrimSuffix(name, ".so")
		soName := baseName + ".so"
		pluginDir := envOrDefault("MAPREDUCE_PLUGIN_DIR", "./plugins")
		resolvedPath = filepath.Join(pluginDir, baseName, soName)
	}

	p, err := plugin.Open(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to open plugin %s: %w", resolvedPath, err)
	}

	mapSymbol, err := p.Lookup("Map")
	if err != nil {
		return fmt.Errorf("plugin %s missing Map symbol: %w", resolvedPath, err)
	}
	reduceSymbol, err := p.Lookup("Reduce")
	if err != nil {
		return fmt.Errorf("plugin %s missing Reduce symbol: %w", resolvedPath, err)
	}

	mapf, ok := mapSymbol.(func(string, string) []KeyValue)
	if !ok {
		return fmt.Errorf("plugin %s Map has invalid signature", resolvedPath)
	}
	reducef, ok := reduceSymbol.(func(string, []string) string)
	if !ok {
		return fmt.Errorf("plugin %s Reduce has invalid signature", resolvedPath)
	}

	w.mapf = mapf
	w.reducef = reducef
	w.pluginPath = resolvedPath
	// Derive plugin name from the path if not explicitly set via env.
	if w.pluginName == "" {
		w.pluginName = strings.TrimSuffix(filepath.Base(resolvedPath), ".so")
	}
	w.logger.Info().Str("plugin", resolvedPath).Str("plugin_name", w.pluginName).Msg("loaded map reduce plugin")
	return nil
}

func (w *Worker) readChunkData(ctx context.Context, request MapReduceRequest) ([]byte, error) {
	if len(request.File.RawContent) > 0 {
		start := request.ChunkInfo.Offset
		if start < 0 {
			start = 0
		}
		if start >= int64(len(request.File.RawContent)) {
			return []byte{}, nil
		}

		end := int64(len(request.File.RawContent))
		if request.ChunkInfo.Size > 0 {
			candidate := start + request.ChunkInfo.Size
			if candidate < end {
				end = candidate
			}
		}

		return append([]byte(nil), request.File.RawContent[start:end]...), nil
	}

	if strings.TrimSpace(w.fsServerAddr) == "" {
		return nil, fmt.Errorf("DFS server address not configured")
	}

	path := strings.TrimSpace(request.ChunkInfo.Path)
	if path == "" {
		path = strings.TrimSpace(request.File.SourcePath)
	}
	if path == "" {
		return nil, fmt.Errorf("chunk path is empty")
	}

	readSize := request.ChunkInfo.Size
	if readSize <= 0 {
		if request.File.SizeBytes > request.ChunkInfo.Offset {
			readSize = request.File.SizeBytes - request.ChunkInfo.Offset
		} else {
			readSize = defaultChunkReadSize
		}
	}

	// Reuse a single DFS client across chunk reads (Fix #2).
	dfsClient := w.getOrCreateDFSClient(ctx)

	// Check for cancellation before starting potentially long DFS read (Fix #6).
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	buffer := make([]byte, readSize)
	n, err := dfsClient.Read(dfscommon.Path(path), dfscommon.Offset(request.ChunkInfo.Offset), buffer)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("invalid bytes read: %d", n)
	}
	if n == 0 {
		return []byte{}, nil
	}

	return buffer[:n], nil
}

func (w *Worker) runReduceFromChannel(request MapReduceRequest, intermediateFiles <-chan string) (string, string, error) {
	all := make([]KeyValue, 0)
	for file := range intermediateFiles {
		f, err := os.Open(file)
		if err != nil {
			return "", "", err
		}

		dec := json.NewDecoder(f)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				if err != io.EOF {
					f.Close()
					return "", "", fmt.Errorf("failed to decode intermediate file %s: %w", file, err)
				}
				break
			}
			all = append(all, kv)
		}
		f.Close()
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })

	var output bytes.Buffer
	for i := 0; i < len(all); {
		j := i + 1
		for j < len(all) && all[j].Key == all[i].Key {
			j++
		}

		values := make([]string, 0, j-i)
		for k := i; k < j; k++ {
			values = append(values, all[k].Value)
		}
		reduced := w.reducef(all[i].Key, values)
		fmt.Fprintf(&output, "%s %s\n", all[i].Key, reduced)
		i = j
	}

	// Atomic write: temp file then rename (Fix #7).
	outputFile := filepath.Join(w.outputDir, fmt.Sprintf("mr-out-%d-%d", request.Handle.Id, request.ChunkIndex))
	tmpFile := outputFile + ".tmp"
	if err := os.WriteFile(tmpFile, output.Bytes(), 0o644); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmpFile, outputFile); err != nil {
		os.Remove(tmpFile)
		return "", "", err
	}

	return outputFile, output.String(), nil
}

func writeIntermediatePartition(partitionFile string, kvs []KeyValue) error {
	of, err := os.Create(partitionFile)
	if err != nil {
		return err
	}
	defer of.Close()

	enc := json.NewEncoder(of)
	for _, kv := range kvs {
		if err := enc.Encode(&kv); err != nil {
			return err
		}
	}

	return nil
}

func (w *Worker) submitResultToMaster(request MapReduceRequest, outputFile, outputData string) error {
	if strings.TrimSpace(w.masterAddress) == "" {
		return fmt.Errorf("master address not configured")
	}

	// Use a deadline so we don't block indefinitely if master is down (Fix #3).
	conn, err := net.DialTimeout("tcp", w.masterAddress, defaultSubmitTimeout)
	if err != nil {
		return err
	}
	client := rpc.NewClient(conn)
	defer client.Close()

	rpcRequest := MapReduceResultRequest{
		Handle:      request.Handle,
		ChunkIndex:  request.ChunkIndex,
		WorkerAddr:  w.serverAddress,
		OutputFile:  outputFile,
		OutputData:  outputData,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	var rpcReply MapReduceResultReply
	if err := client.Call("Master.RPCSubmitMapReduceResult", rpcRequest, &rpcReply); err != nil {
		return err
	}
	if rpcReply.Status == "error" {
		return fmt.Errorf("master rejected result: %s", rpcReply.ErrorMessage)
	}

	return nil
}

func (w *Worker) getOrCreateDFSClient(ctx context.Context) *hercules.HerculesClient {
	w.dfsClientMu.Lock()
	defer w.dfsClientMu.Unlock()
	if w.dfsClient != nil {
		return w.dfsClient
	}
	w.dfsClient = hercules.NewHerculesClient(
		ctx, dfscommon.ServerAddr(strings.TrimSpace(w.fsServerAddr)),
		1*time.Minute)
	return w.dfsClient
}

func (w *Worker) cleanupIntermediateFiles(handleId uint64, chunkIndex int) {
	pattern := filepath.Join(w.intermediate, fmt.Sprintf("mr-%d-%d-*.json", handleId, chunkIndex))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		w.logger.Warn().Err(err).Str("pattern", pattern).Msg("failed to glob intermediate files for cleanup")
		return
	}
	for _, f := range matches {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			w.logger.Warn().Err(err).Str("file", f).Msg("failed to remove intermediate file")
		}
	}
}

func arrangeImmediate(kvs []KeyValue, nReduce int) [][]KeyValue {
	kvap := make([][]KeyValue, nReduce)
	for _, kv := range kvs {
		bucket := Ihash(kv.Key) % nReduce
		kvap[bucket] = append(kvap[bucket], kv)
	}
	return kvap
}

func defaultMap(_ string, contents string) []KeyValue {
	words := strings.Fields(contents)
	result := make([]KeyValue, 0, len(words))
	for _, word := range words {
		result = append(result, KeyValue{Key: word, Value: "1"})
	}
	return result
}

func defaultReduce(_ string, values []string) string {
	return strconv.Itoa(len(values))
}
