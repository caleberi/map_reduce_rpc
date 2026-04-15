package mrp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

const (
	OverflowDropOldest OverflowPolicy = iota
	OverflowReject
)

const (
	uploadedHandlesFileName    = "uploaded_handles.dat"
	defaultUploadedHandleTTL   = 48 * time.Hour
	defaultProcessingHandleTTL = 5 * time.Minute
)

var (
	downloadLogFileNameFormat = "download_log_%d-%d.dlog"
	ErrInvalidHandle          = fmt.Errorf("invalid content handle")
	ErrBufferFull             = fmt.Errorf("buffer full, data may be lost")
)

// SafeLogDeletionDuration computes the minimum logDeletionDuration that prevents
// two failure modes:
//
//  1. Deletion before replay: an uploaded file is removed before a crash-recovery
//     replay can process it. The file must survive at least one full flush cycle
//     (cleanupTimeInterval) after the handle expires (handleExpiryDuration), plus
//     any expected restart/replay overhead (expectedRecoveryTime).
//
//  2. Duplicate replay: a file lives so long it is replayed on multiple restarts.
//     The returned duration is the smallest value that satisfies constraint 1, so
//     callers should not inflate it beyond necessity.
//
// Formula:
//
//	logDeletionDuration = (handleExpiryDuration + cleanupTimeInterval + expectedRecoveryTime) * safetyFactor
//
// safetyFactor must be > 1; values below 1 are clamped to 2.
func SafeLogDeletionDuration(
	handleExpiryDuration time.Duration,
	cleanupTimeInterval time.Duration,
	expectedRecoveryTime time.Duration,
	safetyFactor float64,
) time.Duration {
	if safetyFactor < 1 {
		safetyFactor = 2
	}
	base := handleExpiryDuration + cleanupTimeInterval + expectedRecoveryTime
	return time.Duration(float64(base) * safetyFactor)
}

type Handle struct {
	Id        uint64
	TimeStamp int64
}

type Content struct {
	Id   Handle
	Eof  bool
	Data []byte
}

type OverflowPolicy uint32

// DurableLog defines the interface for a durable write-ahead log that accepts
// content, persists it to disk, and supports crash recovery via replay.
type DurableLog interface {
	GenerateHandle() Handle
	Write(content Content) error
	Flush() error
	Replay(visitor func(Content) bool)
	Close() error
	SetOverflowPolicy(policy OverflowPolicy)
	MarkHandlesUploaded(handles []Handle)
	GetCompletedHandleForRetransmission() []Handle
	GetUploadedHandles() []Handle
	ReleaseProcessingHandle(handle Handle)
	LogDir() string
}

var _ DurableLog = (*DurableBuffer)(nil)

type DurableBuffer struct {
	recoveryMode bool

	inRecovery atomic.Bool
	closed     atomic.Bool

	nextHandle     atomic.Uint64
	lastTimestamp  atomic.Int64
	overflowPolicy atomic.Uint32

	capacity             uint64
	logMu                sync.Mutex
	log                  *RingBuffer[Content]
	handles              sync.Map
	uploadedHandles      sync.Map
	processingHandles    sync.Map
	handleExpiryDuration time.Duration
	cleanupTimeInterval  time.Duration
	logDeletionDuration  time.Duration
	uploadedHandleTTL    time.Duration
	processingHandleTTL  time.Duration
	logDir               string
	manualShutdown       chan struct{}
	done                 chan struct{}

	logger zerolog.Logger
}

func NewDurableBuffer(
	capacity uint64,
	recoveryMode bool,
	handleExpiryDuration,
	cleanupTimeInterval,
	logDeletionDuration time.Duration,
	logDir string) *DurableBuffer {
	if handleExpiryDuration <= 0 {
		handleExpiryDuration = time.Minute
	}
	if logDir == "" {
		logDir = "./download_logs/"
	}
	if capacity == 0 {
		capacity = 1024
	}

	if cleanupTimeInterval <= 0 {
		cleanupTimeInterval = 30 * time.Second
	}

	// Enforce a safe minimum for logDeletionDuration (covers issues #2 and #3):
	// the file must outlive one full expiry+flush cycle before it may be deleted.
	minDeletion := SafeLogDeletionDuration(handleExpiryDuration, cleanupTimeInterval, 0, 2)
	if logDeletionDuration < minDeletion {
		logDeletionDuration = minDeletion
	}

	// uploadedHandleTTL must be >= logDeletionDuration.
	// If uploadedHandleTTL were shorter, pruneUploadedHandles could evict a
	// handle's registry entry before its log file reached the deletion threshold.
	// On the next Replay, isHandleUploaded would return false and the still-present
	// file would be re-processed, causing duplicate delivery.
	uploadedHandleTTL := defaultUploadedHandleTTL
	if uploadedHandleTTL < logDeletionDuration {
		uploadedHandleTTL = logDeletionDuration
	}

	db := &DurableBuffer{
		capacity:             capacity,
		handleExpiryDuration: handleExpiryDuration,
		cleanupTimeInterval:  cleanupTimeInterval,
		logDeletionDuration:  logDeletionDuration,
		uploadedHandleTTL:    uploadedHandleTTL,
		processingHandleTTL:  defaultProcessingHandleTTL,
		recoveryMode:         recoveryMode,
		logDir:               logDir,
		log:                  NewRingBuffer[Content](capacity),
		manualShutdown:       make(chan struct{}),
		done:                 make(chan struct{}),
		logger:               zerolog.New(os.Stdout),
	}
	db.SetOverflowPolicy(OverflowDropOldest)
	db.loadUploadedHandles()
	go db.monitor()
	return db
}

func NewDurableBufferWithPolicy(
	capacity uint64,
	recoveryMode bool,
	handleExpiryDuration,
	cleanupTimeInterval,
	logDeletionDuration time.Duration,
	logDir string,
	policy OverflowPolicy) *DurableBuffer {
	d := NewDurableBuffer(
		capacity, recoveryMode, handleExpiryDuration,
		cleanupTimeInterval, logDeletionDuration, logDir,
	)
	d.SetOverflowPolicy(policy)
	return d
}

func (d *DurableBuffer) SetOverflowPolicy(policy OverflowPolicy) {
	if policy != OverflowDropOldest && policy != OverflowReject {
		policy = OverflowDropOldest
	}
	d.overflowPolicy.Store(uint32(policy))
}

func (d *DurableBuffer) OverflowPolicy() OverflowPolicy {
	return OverflowPolicy(d.overflowPolicy.Load())
}

func (d *DurableBuffer) LogDir() string { return d.logDir }

func (d *DurableBuffer) GenerateHandle() Handle {
	id := d.nextHandle.Add(1)
	timestamp := time.Now().UnixNano()
	for {
		last := d.lastTimestamp.Load()
		if timestamp <= last {
			timestamp = last + 1
		}
		if d.lastTimestamp.CompareAndSwap(last, timestamp) {
			break
		}
	}
	return Handle{Id: id, TimeStamp: timestamp}
}

func (d *DurableBuffer) isHandleExpired(handle Handle) bool {
	if d.inRecovery.Load() {
		return false
	}

	currentId := d.nextHandle.Load()
	if handle.Id == 0 || handle.Id > currentId {
		return true
	}

	currentTime := time.Now().UnixNano()
	if currentTime-handle.TimeStamp > d.handleExpiryDuration.Nanoseconds() {
		return true
	}

	return false
}

func (d *DurableBuffer) syncContentsToFile(contents []Content) error {
	type writeFunc func() error

	buildLogFileName := func(handle Handle) string {
		return fmt.Sprintf(downloadLogFileNameFormat, handle.Id, handle.TimeStamp)
	}

	buildLogContent := func(handle Handle, entry Content) ([]byte, error) {
		buffer := new(bytes.Buffer)

		write := func(data any) error {
			return binary.Write(buffer, binary.LittleEndian, data)
		}
		wrxOrder := []writeFunc{
			func() error { return write(handle.Id) },
			func() error { return write(handle.TimeStamp) },
			func() error { return write(uint32(len(entry.Data))) },
			func() error { return write(entry.Data) },
			func() error { return write(entry.Eof) },
		}
		for _, wf := range wrxOrder {
			if err := wf(); err != nil {
				return nil, err
			}
		}

		return buffer.Bytes(), nil
	}

	fileBuffers := make(map[string]*bytes.Buffer)

	for _, entry := range contents {
		if entry.Id.Id == 0 || d.isHandleExpired(entry.Id) {
			continue
		}
		fileName := buildLogFileName(entry.Id)
		logContent, err := buildLogContent(entry.Id, entry)
		if err != nil {
			return fmt.Errorf("failed to build log content: %w", err)
		}

		if fileBuffers[fileName] == nil {
			fileBuffers[fileName] = new(bytes.Buffer)
		}
		fileBuffers[fileName].Write(logContent)
	}

	if err := os.MkdirAll(d.logDir, 0755); err != nil {
		return err
	}

	writeToFile := func(filePath string, data []byte) error {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", filePath, err)
		}
		defer file.Close()

		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("failed to write to file %s: %w", filePath, err)
		}

		if err := file.Sync(); err != nil {
			return fmt.Errorf("failed to sync file %s: %w", filePath, err)
		}
		return nil
	}

	for fileName, buffer := range fileBuffers {
		filePath := filepath.Join(d.logDir, fileName)
		if err := writeToFile(filePath, buffer.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

// Flush drains the in-memory ring buffer and syncs all pending content to
// their respective .dlog files on disk. Safe to call concurrently.
func (d *DurableBuffer) Flush() error {
	return d.syncLogToFile()
}

func (d *DurableBuffer) syncLogToFile() error {
	d.logMu.Lock()
	capacity := d.log.Cap()
	buffer := make([]Content, capacity)
	count := d.log.Drain(buffer)
	d.logMu.Unlock()

	if count == 0 {
		return nil
	}

	snapshot := make([]Content, 0, count)
	for i := range count {
		entry := buffer[i]
		data := make([]byte, len(entry.Data))
		copy(data, entry.Data)
		snapshot = append(snapshot, Content{
			Id:   entry.Id,
			Eof:  entry.Eof,
			Data: data,
		})
	}

	return d.syncContentsToFile(snapshot)
}

func (d *DurableBuffer) cleanup() {
	if err := d.syncLogToFile(); err != nil {
		d.logger.Warn().Err(err).Msg(
			"failed to sync log to file during cleanup")
	}
	d.pruneProcessingHandles()
	d.saveUploadedHandles()
}

// pruneProcessingHandles removes entries whose TTL has elapsed so stale
// dispatches can be retried by GetUploadedHandles on the next poll tick.
func (d *DurableBuffer) pruneProcessingHandles() {
	now := time.Now().UnixNano()
	ttlNs := d.processingHandleTTL.Nanoseconds()

	d.processingHandles.Range(func(key, value any) bool {
		ts, ok := value.(int64)
		if !ok {
			d.processingHandles.Delete(key)
			return true
		}
		if now-ts > ttlNs {
			d.processingHandles.Delete(key)
		}
		return true
	})
}

func (d *DurableBuffer) performLogDeletion() {
	files, err := os.ReadDir(d.logDir)
	if err != nil {
		d.logger.Warn().Err(err).Msg(
			"failed to read log directory for deletion")
		return
	}
	threshold := time.Now().Add(-d.logDeletionDuration)
	for _, file := range files {
		if !isDownloadLogFile(file.Name()) {
			continue
		}

		// Each .dlog file is named after exactly one handle
		// (download_log_{id}-{timestamp}.dlog), so checking isHandleUploaded
		// on the parsed handle is sufficient to determine whether the entire
		// file is safe to delete.
		//
		// Deletion has two paths:
		//  1. Eager  — MarkHandlesUploaded removes the file immediately after
		//             the caller confirms upload and persists the handle to disk.
		//  2. Deferred — performLogDeletion (this function) is a fallback that
		//             removes any file that survived the eager path once its
		//             mod time has crossed logDeletionDuration. The uploaded
		//             handles registry (uploaded_handles.dat) survives restarts,
		//             so this guard holds across crashes.
		handle, err := parseHandleFromDownloadLogFile(file.Name())
		if err != nil {
			d.logger.Warn().Err(err).Str("log_file", file.Name()).Msg(
				"failed to parse handle from log filename")
			continue
		}

		if !d.isHandleUploaded(handle) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			d.logger.Warn().Err(err).Str("log_file", file.Name()).Msg(
				"failed to get file info")
			continue
		}
		if info.ModTime().Before(threshold) {
			if err := os.Remove(filepath.Join(d.logDir, file.Name())); err != nil {
				d.logger.Warn().Err(err).Str("log_file", file.Name()).Msg(
					"failed to delete log file")
			} else {
				d.uploadedHandles.Delete(handle)
			}
		}
	}
	d.pruneUploadedHandles()
}

func (d *DurableBuffer) Write(content Content) error {
	if d.isHandleExpired(content.Id) {
		return ErrInvalidHandle
	}
	const maxRetries = 100

	data := make([]byte, len(content.Data))
	copy(data, content.Data)
	entry := Content{Id: content.Id, Data: data, Eof: content.Eof}

	retries := 0
	for {
		d.logMu.Lock()
		if d.log.Push(entry) {
			d.logMu.Unlock()
			break
		}

		if d.OverflowPolicy() == OverflowReject {
			d.logMu.Unlock()
			return ErrBufferFull
		}

		_, _ = d.log.Pop()
		d.logger.Warn().Uint64("handle_id", content.Id.Id).Msg(
			"Buffer full, dropping oldest entry")
		if d.log.Push(entry) {
			d.logMu.Unlock()
			break
		}
		d.logMu.Unlock()

		retries++
		if retries > maxRetries {
			return fmt.Errorf("%w after %d retries", ErrBufferFull, maxRetries)
		}
		runtime.Gosched()
	}

	if entry.Eof {
		d.handles.Store(entry.Id, true)
	} else {
		if existing, ok := d.handles.Load(entry.Id); ok {
			if completed, ok := existing.(bool); ok && completed {
				return nil
			}
		}
		d.handles.Store(entry.Id, false)
	}
	return nil
}

func (d *DurableBuffer) Replay(visitor func(Content) bool) {
	if visitor == nil {
		return
	}

	files, err := os.ReadDir(d.logDir)
	if err != nil {
		if err := os.MkdirAll(d.logDir, 0755); err != nil {
			d.logger.Warn().Err(err).Msg(
				"failed to create log directory for replay")
			return
		}
		files = []os.DirEntry{}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	for _, file := range files {
		if !isDownloadLogFile(file.Name()) {
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}

		isEmptyLogFile := info.Size() == 0
		if isEmptyLogFile {
			continue
		}

		filePath := filepath.Join(d.logDir, file.Name())
		f, err := os.Open(filePath)
		if err != nil {
			d.logger.Warn().Err(err).Str("log_file", file.Name()).Msg(
				"failed to open log file for replay")
			continue
		}

		var (
			handleId   uint64
			timestamp  int64
			dataLength uint32
			data       []byte
			eof        bool
		)

		readOrBreak := func(dest any) bool {
			return binary.Read(f, binary.LittleEndian, dest) == nil
		}

		for {
			if !readOrBreak(&handleId) ||
				!readOrBreak(&timestamp) ||
				!readOrBreak(&dataLength) {
				break
			}

			data = make([]byte, dataLength)
			if !readOrBreak(&data) || !readOrBreak(&eof) {
				break
			}

			content := Content{
				Id: Handle{
					Id:        handleId,
					TimeStamp: timestamp,
				},
				Data: data,
				Eof:  eof,
			}

			if d.isHandleUploaded(content.Id) {
				continue
			}

			if !visitor(content) {
				f.Close()
				return
			}
		}

		f.Close()
	}

}

func isDownloadLogFile(name string) bool {
	return filepath.Ext(name) == ".dlog"
}

func (d *DurableBuffer) Close() error {
	if d.closed.CompareAndSwap(false, true) {
		close(d.manualShutdown)
		<-d.done
	}
	return nil
}

func (d *DurableBuffer) monitor() {
	defer close(d.done)

	cleanupTicker := time.NewTicker(d.cleanupTimeInterval)
	defer cleanupTicker.Stop()

	logDeletionInterval := max(d.logDeletionDuration, time.Minute)
	logDeletionTicker := time.NewTicker(logDeletionInterval)
	defer logDeletionTicker.Stop()

	if d.recoveryMode {
		d.inRecovery.Store(true)
		d.Replay(func(content Content) bool {
			d.logger.Info().Uint64("handle_id", content.Id.Id).
				Int64("timestamp", content.Id.TimeStamp).
				Str("data", string(content.Data)).
				Bool("eof", content.Eof).
				Msg("Replaying content")

			for {
				currentNext := d.nextHandle.Load()
				if content.Id.Id >= currentNext {
					if d.nextHandle.CompareAndSwap(currentNext, content.Id.Id+1) {
						break
					}
				} else {
					break
				}
			}

			const maxRetries = 100
			retries := 0
			for {
				d.logMu.Lock()
				ok := d.log.Push(content)
				d.logMu.Unlock()
				if ok {
					break
				}
				retries++
				if retries > maxRetries {
					d.logger.Warn().Msgf(
						"failed to push replayed content after %d retries, high contention",
						maxRetries)
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if content.Eof {
				d.handles.Store(content.Id, true)
			} else {
				if existing, ok := d.handles.Load(content.Id); ok {
					if completed, ok := existing.(bool); ok && completed {
						return true
					}
				}
				d.handles.Store(content.Id, false)
			}
			return true
		})
		d.inRecovery.Store(false)
	}

	for {
		select {
		case <-cleanupTicker.C:
			d.cleanup()
		case <-logDeletionTicker.C:
			d.performLogDeletion()
		case <-d.manualShutdown:
			d.cleanup()
			return
		}
	}
}

func (d *DurableBuffer) GetCompletedHandleForRetransmission() []Handle {
	completedHandles := make([]Handle, 0)

	d.handles.Range(func(key, value any) bool {
		handle, ok := key.(Handle)
		if !ok {
			return true
		}

		isCompleted, ok := value.(bool)
		if !ok || !isCompleted {
			return true
		}

		completedHandles = append(completedHandles, handle)
		return true
	})

	sort.Slice(completedHandles, func(i, j int) bool {
		if completedHandles[i].Id == completedHandles[j].Id {
			return completedHandles[i].TimeStamp < completedHandles[j].TimeStamp
		}
		return completedHandles[i].Id < completedHandles[j].Id
	})

	return completedHandles
}

func (d *DurableBuffer) MarkHandlesUploaded(handles []Handle) {
	now := time.Now().UnixNano()
	ForEach(handles, func(_ int, handle Handle) {
		d.uploadedHandles.Store(handle, now)
		d.handles.Delete(handle)
	})
	d.saveUploadedHandles()
	ForEach(handles, func(_ int, handle Handle) {
		logFile := fmt.Sprintf(downloadLogFileNameFormat, handle.Id, handle.TimeStamp)
		logPath := filepath.Join(d.logDir, logFile)
		if err := os.Remove(logPath); err != nil {
			d.logger.Warn().Err(err).Str("log_file", logFile).Msg(
				"failed to delete log file after upload")
		}
	})
}

func (d *DurableBuffer) GetUploadedHandles() []Handle {
	processingHandles := make([]Handle, 0)
	now := time.Now().UnixNano()
	ttlNs := d.processingHandleTTL.Nanoseconds()

	d.uploadedHandles.Range(func(key, value any) bool {
		handle, ok := key.(Handle)
		if !ok {
			return true
		}

		if _, ok := value.(int64); !ok {
			return true
		}

		stored, alreadyMoved := d.processingHandles.LoadOrStore(handle, now)
		if alreadyMoved {
			// If the entry has exceeded its TTL, allow re-dispatch.
			if ts, ok := stored.(int64); ok && now-ts > ttlNs {
				d.processingHandles.Store(handle, now)
			} else {
				return true
			}
		}

		processingHandles = append(processingHandles, handle)
		return true
	})

	sort.Slice(processingHandles, func(i, j int) bool {
		if processingHandles[i].Id == processingHandles[j].Id {
			return processingHandles[i].TimeStamp < processingHandles[j].TimeStamp
		}
		return processingHandles[i].Id < processingHandles[j].Id
	})

	return processingHandles
}

// ReleaseProcessingHandle removes a handle from the processingHandles registry
// so it can be retried by GetUploadedHandles on a subsequent poll tick.
func (d *DurableBuffer) ReleaseProcessingHandle(handle Handle) {
	d.processingHandles.Delete(handle)
}

// SetUploadedHandleTTL overrides the duration after which persisted uploaded
// handles are eligible for pruning. Must be called before the first write.
// The value is clamped to max(ttl, logDeletionDuration) to preserve the
// invariant that handle registry entries outlive their associated log files.
func (d *DurableBuffer) SetUploadedHandleTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultUploadedHandleTTL
	}
	if ttl < d.logDeletionDuration {
		ttl = d.logDeletionDuration
	}
	d.uploadedHandleTTL = ttl
}

// loadUploadedHandles reads the persisted uploaded-handles file on startup and
// populates uploadedHandles, skipping any entries that have already exceeded
// their TTL.
func (d *DurableBuffer) loadUploadedHandles() {
	filePath := filepath.Join(d.logDir, uploadedHandlesFileName)
	f, err := os.Open(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			d.logger.Warn().Err(err).Msg("failed to open uploaded handles file")
		}
		return
	}
	defer f.Close()

	now := time.Now().UnixNano()
	ttlNs := d.uploadedHandleTTL.Nanoseconds()

	for {
		var id uint64
		var timestamp int64
		var uploadedAt int64
		if binary.Read(f, binary.LittleEndian, &id) != nil {
			break
		}
		if binary.Read(f, binary.LittleEndian, &timestamp) != nil {
			break
		}
		if binary.Read(f, binary.LittleEndian, &uploadedAt) != nil {
			break
		}
		if now-uploadedAt > ttlNs {
			continue
		}
		d.uploadedHandles.Store(Handle{Id: id, TimeStamp: timestamp}, uploadedAt)
	}
}

// saveUploadedHandles atomically rewrites the uploaded-handles file with all
// in-memory entries that have not yet exceeded their TTL. A temp file + rename
// pattern is used so a crash mid-write leaves the previous file intact.
func (d *DurableBuffer) saveUploadedHandles() {
	if err := os.MkdirAll(d.logDir, 0755); err != nil {
		d.logger.Warn().Err(err).Msg("failed to create log dir for saving uploaded handles")
		return
	}

	filePath := filepath.Join(d.logDir, uploadedHandlesFileName)
	tmpPath := filePath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		d.logger.Warn().Err(err).Msg("failed to create uploaded handles temp file")
		return
	}

	now := time.Now().UnixNano()
	ttlNs := d.uploadedHandleTTL.Nanoseconds()

	d.uploadedHandles.Range(func(key, value any) bool {
		handle, ok := key.(Handle)
		if !ok {
			return true
		}
		uploadedAt, ok := value.(int64)
		if !ok {
			return true
		}
		if now-uploadedAt > ttlNs {
			return true
		}
		binary.Write(f, binary.LittleEndian, handle.Id)
		binary.Write(f, binary.LittleEndian, handle.TimeStamp)
		binary.Write(f, binary.LittleEndian, uploadedAt)
		return true
	})

	if err := f.Sync(); err != nil {
		f.Close()
		d.logger.Warn().Err(err).Msg("failed to sync uploaded handles temp file")
		return
	}
	f.Close()

	if err := os.Rename(tmpPath, filePath); err != nil {
		d.logger.Warn().Err(err).Msg("failed to commit uploaded handles file")
	}
}

// pruneUploadedHandles removes in-memory entries whose TTL has elapsed, then
// rewrites the on-disk file to match. Called during the periodic log-deletion
// cycle.
func (d *DurableBuffer) pruneUploadedHandles() {
	now := time.Now().UnixNano()
	ttlNs := d.uploadedHandleTTL.Nanoseconds()

	d.uploadedHandles.Range(func(key, value any) bool {
		handle, ok := key.(Handle)
		if !ok {
			return true
		}
		uploadedAt, ok := value.(int64)
		if !ok {
			d.uploadedHandles.Delete(handle)
			return true
		}
		if now-uploadedAt > ttlNs {
			d.uploadedHandles.Delete(handle)
			d.processingHandles.Delete(handle)
		}
		return true
	})

	d.saveUploadedHandles()
}

func (d *DurableBuffer) isHandleUploaded(handle Handle) bool {
	_, ok := d.uploadedHandles.Load(handle)
	return ok
}

func parseHandleFromDownloadLogFile(fileName string) (Handle, error) {
	if !isDownloadLogFile(fileName) {
		return Handle{}, fmt.Errorf("invalid download log extension: %s", fileName)
	}

	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	const prefix = "download_log_"
	if !strings.HasPrefix(base, prefix) {
		return Handle{}, fmt.Errorf("invalid download log prefix: %s", fileName)
	}

	handlePart := strings.TrimPrefix(base, prefix)
	var (
		handleId  uint64
		timestamp int64
	)
	parsed, err := fmt.Sscanf(handlePart, "%d-%d", &handleId, &timestamp)
	if err != nil || parsed != 2 {
		return Handle{}, fmt.Errorf("invalid download log handle format: %s", fileName)
	}

	return Handle{Id: handleId, TimeStamp: timestamp}, nil
}
