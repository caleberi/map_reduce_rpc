package mrp

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func setupTestDir(t *testing.T) string {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("downloader_test_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	return dir
}

func cleanupTestDir(dir string) {
	os.RemoveAll(dir)
}

func TestNewDownloader(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(
		1024,
		false,
		time.Minute,
		time.Second*10,
		time.Hour,
		testDir,
	)

	assert.NotNil(t, d, "NewDownloader should not return nil")
	assert.Equal(t, testDir, d.logDir, "Expected logDir %s, got %s", testDir, d.logDir)
	assert.Equal(t, time.Minute, d.handleExpiryDuration, "Expected handleExpiryDuration 1m, got %v", d.handleExpiryDuration)
	d.Close()
}

func TestNewDownloaderDefaults(t *testing.T) {
	d := NewDurableBuffer(1024, false, 0, time.Second, time.Hour, "")

	assert.Equalf(
		t, d.handleExpiryDuration, time.Minute,
		"Expected default handleExpiryDuration 1m, got %v", d.handleExpiryDuration,
	)
	assert.Equalf(t, d.logDir, "./download_logs/",
		"Expected default logDir ./download_logs/, got %s", d.logDir,
	)

	d.Close()
	cleanupTestDir(d.logDir)
}

func TestGenerateHandle(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	handle1 := d.GenerateHandle()
	handle2 := d.GenerateHandle()

	assert.NotZero(t, handle1.Id, "Expected non-zero handle ID")
	assert.Greater(t, handle2.Id, handle1.Id, "Expected monotonically increasing handle IDs")
	assert.NotZero(t, handle1.TimeStamp, "Expected non-zero timestamp")
	assert.Greater(t, handle2.TimeStamp, handle1.TimeStamp, "Expected increasing timestamps")
}

func TestWriteValidHandle(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	handle := d.GenerateHandle()
	content := Content{
		Id:   handle,
		Data: []byte("test data"),
		Eof:  false,
	}

	err := d.Write(content)
	assert.NoError(t, err, "Write should not fail")

	assert.Equal(t, uint64(1), d.log.Len(), "Expected 1 entry in ring buffer")
}

func TestWriteExpiredHandle(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Millisecond*100, time.Second*10, time.Hour, testDir)
	defer d.Close()

	handle := d.GenerateHandle()

	time.Sleep(time.Millisecond * 150)

	content := Content{
		Id:   handle,
		Data: []byte("test data"),
		Eof:  false,
	}

	err := d.Write(content)
	assert.Equalf(t, ErrInvalidHandle, err, "Expected ErrInvalidHandle, got %v", err)
}

func TestWriteInvalidHandleId(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	d.GenerateHandle()
	content := Content{
		Id: Handle{
			Id:        999999,
			TimeStamp: time.Now().UnixNano(),
		},
		Data: []byte("test data"),
		Eof:  false,
	}

	err := d.Write(content)
	assert.ErrorIs(t, err, ErrInvalidHandle, "Expected ErrInvalidHandle, got %v", err)
}

func TestWriteMultipleEntries(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	count := 10
	for i := 0; i < count; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "data %d", i),
			Eof:  i == count-1,
		}

		err := d.Write(content)
		assert.NoError(t, err, "Write %d should not fail", i)
	}

	assert.Equal(t, uint64(count), d.log.Len(), "Expected %d entries", count)
}

func TestWriteBufferFull(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)
	defer d.Close()

	capacity := int(d.log.Cap())

	writeCount := capacity + 5
	for i := 0; i < writeCount; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "data %d", i),
			Eof:  false,
		}

		err := d.Write(content)
		assert.NoError(t, err, "Write %d should not fail", i)
	}

	currentSize := d.log.Len()

	assert.LessOrEqual(t, currentSize, d.log.Cap(), "Buffer size should not exceed capacity")
}

func TestWriteBufferFullRejectPolicy(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(2, false, time.Hour, time.Second*10, time.Hour, testDir)
	d.SetOverflowPolicy(OverflowReject)
	defer d.Close()

	h1 := d.GenerateHandle()
	h2 := d.GenerateHandle()
	h3 := d.GenerateHandle()

	assert.NoError(t, d.Write(Content{Id: h1, Data: []byte("a")}))
	assert.NoError(t, d.Write(Content{Id: h2, Data: []byte("b")}))
	err := d.Write(Content{Id: h3, Data: []byte("c")})
	assert.ErrorIs(t, err, ErrBufferFull)
	assert.Equal(t, uint64(2), d.log.Len())
}

func TestWriteBufferFullDropOldestPolicy(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(2, false, time.Hour, time.Second*10, time.Hour, testDir)
	d.SetOverflowPolicy(OverflowDropOldest)
	defer d.Close()

	h1 := d.GenerateHandle()
	h2 := d.GenerateHandle()
	h3 := d.GenerateHandle()

	assert.NoError(t, d.Write(Content{Id: h1, Data: []byte("first")}))
	assert.NoError(t, d.Write(Content{Id: h2, Data: []byte("second")}))
	assert.NoError(t, d.Write(Content{Id: h3, Data: []byte("third")}))

	v1, ok := d.log.Pop()
	assert.True(t, ok)
	v2, ok := d.log.Pop()
	assert.True(t, ok)

	assert.Equal(t, "second", string(v1.Data))
	assert.Equal(t, "third", string(v2.Data))
}

func TestSyncLogToFile(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	handle := d.GenerateHandle()
	content := Content{
		Id:   handle,
		Data: fmt.Appendf(nil, "test data for sync"),
		Eof:  true,
	}

	err := d.Write(content)
	assert.NoError(t, err, "Write should not fail")

	err = d.syncLogToFile()
	assert.NoError(t, err, "syncLogToFile should not fail")

	expectedFile := fmt.Sprintf(downloadLogFileNameFormat, handle.Id, handle.TimeStamp)
	filePath := filepath.Join(testDir, expectedFile)

	_, err = os.Stat(filePath)
	assert.NoError(t, err, "Expected file %s to exist", filePath)
}

func TestSyncLogToFileDoesNotDuplicateOnRepeatedSync(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)

	handle := d.GenerateHandle()
	content := Content{
		Id:   handle,
		Data: []byte("idempotent sync payload"),
		Eof:  true,
	}

	err := d.Write(content)
	assert.NoError(t, err, "Write should not fail")

	err = d.syncLogToFile()
	assert.NoError(t, err, "first syncLogToFile should not fail")

	err = d.syncLogToFile()
	assert.NoError(t, err, "second syncLogToFile should not fail")

	d.Close()

	d2 := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d2.Close()

	replayedCount := 0
	d2.Replay(func(c Content) bool {
		replayedCount++
		return true
	})

	assert.Equal(t, 1, replayedCount, "Expected replay to read a single entry after repeated sync")
}

func TestCleanup(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Millisecond*100, time.Second*10, time.Hour, testDir)
	defer d.Close()

	handle1 := d.GenerateHandle()
	content1 := Content{Id: handle1, Data: []byte("old data"), Eof: false}
	d.Write(content1)

	time.Sleep(time.Millisecond * 150)

	handle2 := d.GenerateHandle()
	content2 := Content{Id: handle2, Data: []byte("new data"), Eof: false}
	d.Write(content2)

	entriesBeforeCount := d.log.Len()

	d.cleanup()
	entriesAfterCount := d.log.Len()
	assert.Less(t, entriesAfterCount, entriesBeforeCount, "Expected cleanup to remove expired entries")
}

func TestCleanupDoesNotDuplicateLogEntries(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)

	for i := 0; i < 5; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "entry %d", i),
			Eof:  false,
		}
		d.Write(content)
	}

	// First cleanup: syncs + compacts
	d.cleanup()

	// Second cleanup: should NOT re-flush the same entries
	d.cleanup()

	// Third cleanup: really stress the watermark reset
	d.cleanup()

	files, _ := os.ReadDir(testDir)
	t.Logf("Files after 3 cleanups:")
	for _, f := range files {
		t.Logf("  %s", f.Name())
	}

	d.Close()

	// Replay and count: should see exactly 5 entries, not 15
	d2 := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)
	defer d2.Close()

	replayedCount := 0
	d2.Replay(func(c Content) bool {
		replayedCount++
		t.Logf("Replayed: %s", string(c.Data))
		return true
	})

	assert.Equal(t, 5, replayedCount, "Expected 5 unique entries after multiple cleanups, got %d (duplicate flush bug)", replayedCount)
}

func TestReplay(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)

	handles := make([]Handle, 3)
	for i := 0; i < 3; i++ {
		handles[i] = d.GenerateHandle()
		content := Content{
			Id:   handles[i],
			Data: fmt.Appendf(nil, "data %d", i),
			Eof:  i == 2,
		}
		d.Write(content)
	}

	d.Close()

	time.Sleep(time.Millisecond * 100)

	d2 := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d2.Close()

	replayedCount := 0
	d2.Replay(func(c Content) bool {
		replayedCount++
		return true
	})

	assert.Equal(t, 3, replayedCount, "Expected 3 replayed entries, got %d", replayedCount)
	cleanupTestDir(testDir)
}

func TestRecoveryMode(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)

	for i := 0; i < 5; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "recovery data %d", i),
			Eof:  i == 4,
		}
		d.Write(content)
	}

	d.Close()
	time.Sleep(time.Millisecond * 100)

	d2 := NewDurableBuffer(1024, true, time.Minute, time.Second*10, time.Hour, testDir)
	defer d2.Close()

	time.Sleep(time.Millisecond * 500)

	entriesCount := d2.log.Len()

	assert.Equal(t, uint64(5), entriesCount, "Expected 5 recovered entries, got %d", entriesCount)
	cleanupTestDir(testDir)
}

func TestReplayCallback(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)

	handle := d.GenerateHandle()
	expectedData := fmt.Appendf(nil, "replay test data")
	content := Content{
		Id:   handle,
		Data: expectedData,
		Eof:  true,
	}
	d.Write(content)
	d.Close()

	time.Sleep(time.Millisecond * 100)

	d2 := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d2.Close()

	replayed := false
	d2.Replay(func(c Content) bool {
		replayed = true
		assert.Equal(t, string(expectedData), string(c.Data), "Expected data %s, got %s", expectedData, c.Data)
		assert.True(t, c.Eof, "Expected Eof to be true")
		return true
	})

	assert.True(t, replayed, "Replay callback was not called")

	cleanupTestDir(testDir)
}

func TestConcurrentWrites(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	numGoroutines := 10
	writesPerGoroutine := 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				handle := d.GenerateHandle()
				content := Content{
					Id:   handle,
					Data: fmt.Appendf(nil, "goroutine %d write %d", id, i),
					Eof:  false,
				}
				err := d.Write(content)
				assert.NoError(t, err, "Concurrent write failed: %v", err)
			}
		}(g)
	}

	wg.Wait()

	entriesCount := d.log.Len()
	expectedCount := uint64(numGoroutines * writesPerGoroutine)
	assert.Equal(t, expectedCount, entriesCount, "Expected %d entries after concurrent writes, got %d", expectedCount, entriesCount)
}

func TestClose(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)

	handle := d.GenerateHandle()
	content := Content{Id: handle, Data: []byte("test"), Eof: false}
	d.Write(content)

	err := d.Close()
	assert.NoError(t, err, "Close returned error: %v", err)

	time.Sleep(time.Millisecond * 200)
}

func TestPerformLogDeletion(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Millisecond*100, testDir)
	defer d.Close()

	handle := d.GenerateHandle()
	content := Content{Id: handle, Data: []byte("old data"), Eof: false}
	d.Write(content)
	d.syncLogToFile()

	// Mark handle as uploaded so performLogDeletion considers it for removal.
	// We store directly rather than calling MarkHandlesUploaded to avoid the
	// eager file deletion that would remove the file before we can test the
	// deferred deletion path.
	d.uploadedHandles.Store(handle, time.Now().UnixNano())

	files, _ := os.ReadDir(testDir)
	assert.NotZero(t, len(files), "Expected log file to be created")

	fileName := ""
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".dlog" {
			fileName = file.Name()
			break
		}
	}
	assert.NotEmpty(t, fileName, "Expected at least one .dlog file")

	oldTime := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(testDir, fileName), oldTime, oldTime)

	d.performLogDeletion()

	filesAfter, _ := os.ReadDir(testDir)
	remainingDlogCount := 0
	for _, file := range filesAfter {
		if filepath.Ext(file.Name()) == ".dlog" {
			remainingDlogCount++
		}
	}
	assert.Zero(t, remainingDlogCount, "Expected .dlog files to be deleted")
}

func TestGetCompletedHandleForRetransmission(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	h1 := d.GenerateHandle()
	h2 := d.GenerateHandle()

	assert.NoError(t, d.Write(Content{Id: h1, Data: []byte("h1-part"), Eof: false}))
	assert.NoError(t, d.Write(Content{Id: h2, Data: []byte("h2-complete"), Eof: true}))
	assert.NoError(t, d.Write(Content{Id: h1, Data: []byte("h1-end"), Eof: true}))

	completed := d.GetCompletedHandleForRetransmission()
	assert.Len(t, completed, 2)
	assert.Equal(t, h1, completed[0])
	assert.Equal(t, h2, completed[1])

	completedAgain := d.GetCompletedHandleForRetransmission()
	assert.Len(t, completedAgain, 2, "Completed handles remain pending until upload is acknowledged")

	d.MarkHandlesUploaded([]Handle{h1, h2})
	completedAfterAck := d.GetCompletedHandleForRetransmission()
	assert.Empty(t, completedAfterAck, "Completed handles should be removed after upload acknowledgement")
}

func TestGetCompletedHandleForRetransmission_IgnoresIncompleteHandles(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	h := d.GenerateHandle()
	assert.NoError(t, d.Write(Content{Id: h, Data: []byte("only-part"), Eof: false}))

	completed := d.GetCompletedHandleForRetransmission()
	assert.Empty(t, completed)

	assert.NoError(t, d.Write(Content{Id: h, Data: []byte("end"), Eof: true}))
	completed = d.GetCompletedHandleForRetransmission()
	assert.Equal(t, []Handle{h}, completed)

	d.MarkHandlesUploaded([]Handle{h})
	completed = d.GetCompletedHandleForRetransmission()
	assert.Empty(t, completed)
}

func TestPerformLogDeletionRequiresUploadAck(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Millisecond*100, testDir)
	defer d.Close()

	h := d.GenerateHandle()
	assert.NoError(t, d.Write(Content{Id: h, Data: []byte("completed"), Eof: true}))
	assert.NoError(t, d.syncLogToFile())

	fileName := fmt.Sprintf(downloadLogFileNameFormat, h.Id, h.TimeStamp)
	filePath := filepath.Join(testDir, fileName)
	oldTime := time.Now().Add(-time.Hour)
	assert.NoError(t, os.Chtimes(filePath, oldTime, oldTime))

	d.performLogDeletion()
	_, err := os.Stat(filePath)
	assert.NoError(t, err, "Log should not be deleted before upload acknowledgement")

	d.MarkHandlesUploaded([]Handle{h})
	d.performLogDeletion()
	_, err = os.Stat(filePath)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err), "Log should be deleted after upload acknowledgement")
}

func TestGetUploadedHandlesForProcessing(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	h1 := d.GenerateHandle()
	h2 := d.GenerateHandle()
	h3 := d.GenerateHandle()

	d.MarkHandlesUploaded([]Handle{h2, h1})

	first := d.GetUploadedHandles()
	assert.Equal(t, []Handle{h1, h2}, first)

	second := d.GetUploadedHandles()
	assert.Empty(t, second, "Handles already moved for processing should not be returned again")

	d.MarkHandlesUploaded([]Handle{h3})
	third := d.GetUploadedHandles()
	assert.Equal(t, []Handle{h3}, third)
}

func TestGetUploadedHandlesForProcessing_IgnoresNotUploaded(t *testing.T) {
	testDir := setupTestDir(t)
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	h := d.GenerateHandle()
	assert.NoError(t, d.Write(Content{Id: h, Data: []byte("complete"), Eof: true}))

	processing := d.GetUploadedHandles()
	assert.Empty(t, processing, "Only uploaded handles should be exposed for processing")
}

func BenchmarkGenerateHandle(b *testing.B) {
	testDir := setupTestDir(&testing.T{})
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Minute, time.Second*10, time.Hour, testDir)
	defer d.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.GenerateHandle()
	}
}

func BenchmarkWriteSingleThread(b *testing.B) {
	testDir := setupTestDir(&testing.T{})
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)
	defer d.Close()

	data := []byte("benchmark data for write test")
	handles := make([]Handle, b.N)
	for i := 0; i < b.N; i++ {
		handles[i] = d.GenerateHandle()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := Content{
			Id:   handles[i],
			Data: data,
			Eof:  false,
		}
		d.Write(content)
	}
}

func BenchmarkWriteConcurrent(b *testing.B) {
	testDir := setupTestDir(&testing.T{})
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)
	defer d.Close()

	data := []byte("benchmark data for concurrent write test")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			handle := d.GenerateHandle()
			content := Content{
				Id:   handle,
				Data: data,
				Eof:  false,
			}
			d.Write(content)
		}
	})
}

func BenchmarkSyncLogToFile(b *testing.B) {
	testDir := setupTestDir(&testing.T{})
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)
	defer d.Close()

	for i := 0; i < 100; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "data %d", i),
			Eof:  false,
		}
		d.Write(content)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.syncLogToFile()
	}
}

func BenchmarkCleanup(b *testing.B) {
	testDir := setupTestDir(&testing.T{})
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Millisecond*100, time.Second*10, time.Hour, testDir)
	defer d.Close()

	for i := 0; i < 50; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "data %d", i),
			Eof:  false,
		}
		d.Write(content)
	}

	time.Sleep(time.Millisecond * 150)

	for i := 0; i < 50; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "data %d", i+50),
			Eof:  false,
		}
		d.Write(content)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.cleanup()
	}
}

func BenchmarkReplay(b *testing.B) {
	testDir := setupTestDir(&testing.T{})
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)

	for i := 0; i < 100; i++ {
		handle := d.GenerateHandle()
		content := Content{
			Id:   handle,
			Data: fmt.Appendf(nil, "data %d", i),
			Eof:  false,
		}
		d.Write(content)
	}
	d.syncLogToFile()
	d.Close()

	d2 := NewDurableBuffer(1024, false, time.Hour, time.Second*10, time.Hour, testDir)
	defer d2.Close()

	counter := 0
	visitor := func(c Content) bool {
		counter++
		return true
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter = 0
		d2.Replay(visitor)
	}

	cleanupTestDir(testDir)
}

func BenchmarkConcurrentWriteAndCleanup(b *testing.B) {
	testDir := setupTestDir(&testing.T{})
	defer cleanupTestDir(testDir)

	d := NewDurableBuffer(1024, false, time.Millisecond*50, time.Millisecond*100, time.Hour, testDir)
	defer d.Close()

	b.ResetTimer()

	var wg sync.WaitGroup

	wg.Add(4)
	for w := 0; w < 4; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < b.N/4; i++ {
				handle := d.GenerateHandle()
				content := Content{
					Id:   handle,
					Data: []byte("concurrent benchmark data"),
					Eof:  false,
				}
				d.Write(content)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N/100; i++ {
			d.cleanup()
			time.Sleep(time.Microsecond * 100)
		}
	}()

	wg.Wait()
}
