package mrp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dfscommon "github.com/caleberi/distributed-system/common"
	"github.com/caleberi/distributed-system/hercules"
	"github.com/rs/zerolog"
)

type completedHandleMetadata struct {
	Handle      Handle `json:"handle"`
	SourceLog   string `json:"source_log"`
	SourcePath  string `json:"source_path"`
	SizeBytes   int64  `json:"size_bytes"`
	ModUnixNano int64  `json:"mod_unix_nano"`
	UploadedAt  string `json:"uploaded_at"`
	Completed   bool   `json:"completed"`
	RawContent  []byte `json:"raw_content,omitempty"`
}

const (
	MAPREDUCE_DOWNLOAD_LOG_DIR      = "MAPREDUCE_DOWNLOAD_LOG_DIR"
	MAPREDUCE_MASTER_SERVER_ADDRESS = "MAPREDUCE_MASTER_SERVER_ADDRESS"
)

var (
	defaultDownloadBufferSize = 1024
	defaultMasterAddress      = "localhost:1235"
	downloadLogDir            = "./mapreduce_download_logs"
	uploadPollInterval        = 15 * time.Second
	defaultCloseTimeout       = 30 * time.Second
)

type Coordinator struct {
	serverAddress   string
	fsServerAddress string
	masterAddress   string
	wg              sync.WaitGroup
	startOnce       sync.Once
	stopOnce        sync.Once
	startErr        error
	stopCh          chan struct{}

	uploadMu  sync.Mutex // guards uploadFileToStorage against concurrent calls
	logger    zerolog.Logger
	listener  Listener
	dfsClient *hercules.HerculesClient // lazily initialized, reused across ticks

	durableLog *DurableBuffer
}

func NewCoordinator(serverAddress, fileServerAddress string) (*Coordinator, error) {
	effectiveLogDir := envOrDefault(
		MAPREDUCE_DOWNLOAD_LOG_DIR,
		downloadLogDir,
	)

	coord := &Coordinator{
		serverAddress:   serverAddress,
		fsServerAddress: fileServerAddress,
		masterAddress: envOrDefault(
			MAPREDUCE_MASTER_SERVER_ADDRESS,
			defaultMasterAddress,
		),
		logger: zerolog.New(os.Stdout),
		stopCh: make(chan struct{}),
		durableLog: NewDurableBuffer(
			uint64(defaultDownloadBufferSize),
			true,
			1*time.Minute,
			5*time.Minute,
			1*time.Hour,
			effectiveLogDir,
		),
	}
	rpc := rpc.NewServer()
	err := rpc.Register(coord)
	if err != nil {
		return nil, err
	}

	coord.listener = NewRPCListener(
		serverAddress,
		func(conn net.Conn) {
			logger := coord.logger.With().Str(
				"remote_addr", conn.RemoteAddr().String()).Logger()
			logger.Info().Msg("[x] accepted new connection")
			logger.Info().Msgf("|- network type: %s", conn.RemoteAddr().Network())
			logger.Info().Msgf("|- local address: %s", conn.LocalAddr().String())

			rpc.ServeConn(conn)
		})

	return coord, nil
}

func (c *Coordinator) Start() error {
	c.startOnce.Do(func() {
		if err := c.listener.Listen(); err != nil {
			c.startErr = err
			return
		}

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			ticker := time.NewTicker(uploadPollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-c.stopCh:
					return
				case <-ticker.C:
					if err := c.uploadFileToStorage(context.Background()); err != nil {
						c.logger.Error().Err(err).Msg(
							"failed uploading completed log metadata to DFS")
					}
					if err := c.startMapReduce(); err != nil {
						c.logger.Error().Err(err).Msg(
							"failed starting map reduce processing")
					}
				}
			}
		}()

		errs := c.listener.Errors()
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.stopCh:
					return
				case err, ok := <-errs:
					if !ok {
						return
					}
					if err != nil {
						c.logger.Error().Err(err).Msg("RPC listener error")
					}
				}
			}
		}()
	})

	return c.startErr
}

func (c *Coordinator) Close() error {
	var closeErr error

	c.stopOnce.Do(func() {
		close(c.stopCh)
		// Wait for ticker and error goroutines to exit before final upload
		// to prevent concurrent uploadFileToStorage calls.
		c.wg.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
		defer cancel()

		if err := c.uploadFileToStorage(ctx); err != nil {
			c.logger.Error().Err(err).Msg(
				"failed final upload of completed log metadata to DFS")
		}

		if c.listener != nil {
			if err := c.listener.Close(); err != nil {
				closeErr = errors.Join(
					closeErr, fmt.Errorf("failed to close RPC listener: %w", err),
				)
			}
		}

		if c.durableLog != nil {
			if err := c.durableLog.Close(); err != nil {
				closeErr = errors.Join(closeErr, fmt.Errorf("failed to close durable log: %w", err))
			}
		}
	})

	return closeErr
}

func (c *Coordinator) RPCPing(message string, reply *string) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}
	if message == "" {
		*reply = "pong"
		return nil
	}
	*reply = "pong: " + message
	return nil
}

func (c *Coordinator) RPCForwardDownload(request DownloadRequest, reply *DownloadReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	content := Content{
		Id:   request.Handle,
		Data: request.Data,
		Eof:  request.Eof,
	}
	err := c.durableLog.Write(content)
	if err != nil {
		reply.Status = "error"
		if errors.Is(err, ErrInvalidHandle) || errors.Is(err, ErrBufferFull) {
			reply.ErrorCode = 400
		} else {
			reply.ErrorCode = 500
		}
		reply.ErrorMessage = fmt.Sprintf("failed to write content: %v", err)
		return nil
	}
	reply.Status = "success"
	return nil
}

func (c *Coordinator) RPCGenerateDownloadHandle(request HandleRequest, reply *HandleReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	reply.Status = "success"
	reply.Handle = c.durableLog.GenerateHandle()
	return nil
}

func (c *Coordinator) RPCNotifyJobComplete(request JobCompleteRequest, reply *JobCompleteReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	c.logger.Info().
		Uint64("handle_id", request.Handle.Id).
		Str("result_file", request.ResultFile).
		Str("status", request.Status).
		Msg("received job completion notification from master")

	c.durableLog.ReleaseProcessingHandle(request.Handle)

	reply.Status = "success"
	reply.Message = fmt.Sprintf("acknowledged completion of handle %d", request.Handle.Id)
	return nil
}

// RPCTriggerProcessing immediately flushes the durable buffer, uploads
// completed handles to DFS, and dispatches them to the master — bypassing
// the 15-second poll timer.
func (c *Coordinator) RPCTriggerProcessing(request TriggerProcessingRequest, reply *TriggerProcessingReply) error {
	if reply == nil {
		return fmt.Errorf("reply cannot be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := c.uploadFileToStorage(ctx); err != nil {
		reply.Status = "error"
		reply.ErrorMessage = fmt.Sprintf("upload to DFS failed: %v", err)
		return nil
	}

	if err := c.startMapReduce(request.Plugin); err != nil {
		reply.Status = "error"
		reply.ErrorMessage = fmt.Sprintf("dispatch to master failed: %v", err)
		return nil
	}

	reply.Status = "success"
	return nil
}

func (c *Coordinator) startMapReduce(plugin ...string) error {
	handles := c.durableLog.GetUploadedHandles()
	if len(handles) == 0 {
		return nil
	}

	p := ""
	if len(plugin) > 0 {
		p = plugin[0]
	}

	var processingErr error
	ForEach(handles, func(_ int, handle Handle) {
		err := c.runMapReduceForHandle(handle, p)
		if err != nil {
			// Release the handle from processingHandles so it can be retried
			// on the next poll tick.
			c.durableLog.ReleaseProcessingHandle(handle)
			processingErr = errors.Join(processingErr, err)
		}
	})

	return processingErr
}

func (c *Coordinator) runMapReduceForHandle(handle Handle, plugin string) error {
	c.logger.Info().Uint64("handle_id", handle.Id).
		Int64("timestamp", handle.TimeStamp).
		Msg("scheduled map reduce task")
	if strings.TrimSpace(c.masterAddress) == "" {
		return fmt.Errorf("master address is empty")
	}

	client, err := rpc.Dial("tcp", c.masterAddress)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to connect to master")
		return err
	}
	defer client.Close()

	request := StartMapReduceRequest{Handle: handle, Plugin: plugin}
	var reply StartMapReduceReply
	if err := client.Call("Master.RPCStartMapReduce", request, &reply); err != nil {
		c.logger.Error().Err(err).Msg("failed to invoke master map reduce RPC")
		return err
	}

	if reply.Status == "error" {
		return fmt.Errorf("master failed to start map reduce: %s", reply.ErrorMessage)
	}

	if reply.Status == "partial" {
		c.logger.Warn().Uint64("handle_id", handle.Id).
			Str("message", reply.Message).
			Msg("master partially dispatched map reduce — some chunks may need retry")
	}

	return nil
}

func (c *Coordinator) getOrCreateDFSClient(ctx context.Context) *hercules.HerculesClient {
	if c.dfsClient != nil {
		return c.dfsClient
	}
	c.dfsClient = hercules.NewHerculesClient(
		ctx, dfscommon.ServerAddr(strings.TrimSpace(c.fsServerAddress)),
		1*time.Minute)
	return c.dfsClient
}

func (c *Coordinator) uploadFileToStorage(ctx context.Context) error {
	if strings.TrimSpace(c.fsServerAddress) == "" {
		return nil
	}

	c.uploadMu.Lock()
	defer c.uploadMu.Unlock()

	// Flush pending ring-buffer entries to disk before reading .dlog files.
	// Without this, handles may be marked complete in memory while their
	// data is still in the ring buffer, causing "file not found" errors.
	if err := c.durableLog.Flush(); err != nil {
		c.logger.Warn().Err(err).Msg("failed to flush durable log before upload")
	}

	completedHandles := c.durableLog.GetCompletedHandleForRetransmission()
	if len(completedHandles) == 0 {
		return nil
	}

	dfsClient := c.getOrCreateDFSClient(ctx)
	uploaded := make([]Handle, 0, len(completedHandles))
	var uploadErr error

	ForEach(completedHandles, func(_ int, handle Handle) {
		select {
		case <-ctx.Done():
			uploadErr = errors.Join(uploadErr, ctx.Err())
			return
		default:
		}

		payload, remotePath, err := c.buildHandleMetadataPayload(handle)
		if err != nil {
			uploadErr = errors.Join(uploadErr, fmt.Errorf("failed to build metadata payload for handle %d: %w", handle.Id, err))
			return
		}

		if err := ensureRemoteDirectoryExists(dfsClient, remotePath); err != nil {
			uploadErr = errors.Join(uploadErr, fmt.Errorf("failed to ensure remote directory for %s: %w", remotePath, err))
			return
		}

		// Upload the actual file content as a separate .content file on DFS.
		// Workers will read directly from this file, avoiding the need to
		// embed large content inside JSON metadata.
		contentPath := strings.TrimSuffix(remotePath, ".json") + ".content"
		contentDfsPath := dfscommon.Path(contentPath)
		if err := dfsClient.CreateFile(contentDfsPath); err != nil && !strings.Contains(strings.ToLower(err.Error()), "exist") {
			uploadErr = errors.Join(uploadErr, fmt.Errorf("failed to create content file %s: %w", contentPath, err))
			return
		}

		contentData, err := c.decodeDlogContent(handle)
		if err != nil {
			uploadErr = errors.Join(uploadErr, fmt.Errorf("failed to decode dlog for handle %d: %w", handle.Id, err))
			return
		}

		if _, err := dfsClient.Write(contentDfsPath, 0, contentData); err != nil {
			uploadErr = errors.Join(uploadErr, fmt.Errorf("failed to write content file %s: %w", contentPath, err))
			return
		}

		// Upload the small JSON metadata file (no RawContent).
		dfsPath := dfscommon.Path(remotePath)
		if err := dfsClient.CreateFile(dfsPath); err != nil && !strings.Contains(strings.ToLower(err.Error()), "exist") {
			uploadErr = errors.Join(uploadErr, fmt.Errorf("failed to create remote path %s: %w", remotePath, err))
			return
		}

		if _, err := dfsClient.Write(dfsPath, 0, payload); err != nil {
			uploadErr = errors.Join(uploadErr, fmt.Errorf("failed to write remote metadata %s: %w", remotePath, err))
			return
		}

		uploaded = append(uploaded, handle)
	})

	if len(uploaded) > 0 {
		c.durableLog.MarkHandlesUploaded(uploaded)
	}
	c.logger.Info().
		Int("uploaded_count", len(uploaded)).
		Msg("completed log metadata uploaded to DFS")

	return uploadErr
}

func (c *Coordinator) buildHandleMetadataPayload(handle Handle) ([]byte, string, error) {
	logFile := fmt.Sprintf(downloadLogFileNameFormat, handle.Id, handle.TimeStamp)
	logPath := filepath.Join(c.durableLog.LogDir(), logFile)

	var (
		sizeBytes int64
		modTime   int64
	)
	info, err := os.Stat(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.logger.Warn().Str("log_file", logFile).
				Msg("dlog file missing during metadata build — content will be empty")
		} else {
			return nil, "", err
		}
	} else {
		modTime = info.ModTime().UnixNano()
		sizeBytes = info.Size()
	}

	metadata := completedHandleMetadata{
		Handle:      handle,
		SourceLog:   logFile,
		SourcePath:  logPath,
		SizeBytes:   sizeBytes,
		ModUnixNano: modTime,
		UploadedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Completed:   true,
	}

	payload, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", err
	}

	remotePrefix := envOrDefault("MAPREDUCE_DFS_UPLOAD_PREFIX", "/mapreduce")
	remotePath := strings.TrimRight(remotePrefix, "/") + "/" + strings.TrimSuffix(logFile, filepath.Ext(logFile)) + ".json"
	return payload, remotePath, nil
}

func (c *Coordinator) decodeDlogContent(handle Handle) ([]byte, error) {
	logFile := fmt.Sprintf(downloadLogFileNameFormat, handle.Id, handle.TimeStamp)
	logPath := filepath.Join(c.durableLog.LogDir(), logFile)
	return decodeDlogFileContent(logPath)
}

// decodeDlogFileContent reads a .dlog binary file and extracts the actual file
// data by stripping the WAL binary framing (handle ID, timestamp, data length,
// EOF flag) from each record, then concatenates the raw data payloads.
func decodeDlogFileContent(logPath string) ([]byte, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result bytes.Buffer
	for {
		var (
			handleId  uint64
			timestamp int64
			dataLen   uint32
		)
		if err := binary.Read(f, binary.LittleEndian, &handleId); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("reading handle id: %w", err)
		}
		if err := binary.Read(f, binary.LittleEndian, &timestamp); err != nil {
			return nil, fmt.Errorf("reading timestamp: %w", err)
		}
		if err := binary.Read(f, binary.LittleEndian, &dataLen); err != nil {
			return nil, fmt.Errorf("reading data length: %w", err)
		}

		data := make([]byte, dataLen)
		if err := binary.Read(f, binary.LittleEndian, &data); err != nil {
			return nil, fmt.Errorf("reading data: %w", err)
		}

		var eof bool
		if err := binary.Read(f, binary.LittleEndian, &eof); err != nil {
			return nil, fmt.Errorf("reading eof flag: %w", err)
		}

		result.Write(data)
	}

	return result.Bytes(), nil
}

func ensureRemoteDirectoryExists(client *hercules.HerculesClient, remoteFilePath string) error {
	remoteDir := path.Dir(strings.TrimSpace(remoteFilePath))
	if remoteDir == "." || remoteDir == "" || remoteDir == "/" {
		return nil
	}

	parts := strings.Split(strings.Trim(remoteDir, "/"), "/")
	if len(parts) == 0 {
		return nil
	}

	var current strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		current.WriteString("/" + part)
		err := client.MkDir(dfscommon.Path(current.String()))
		if err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "exist") || strings.Contains(lower, "already") {
				continue
			}
			return err
		}
	}

	return nil
}
