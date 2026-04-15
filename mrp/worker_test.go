package mrp

import (
	"fmt"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerHeartbeatRPCIntegration(t *testing.T) {
	worker, err := NewWorker("127.0.0.1:0")
	require.NoError(t, err)

	require.NoError(t, worker.Start())
	defer func() {
		require.NoError(t, worker.Close())
	}()

	rpcListener, ok := worker.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, rpcListener.listener)

	client, err := rpc.Dial("tcp", rpcListener.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	request := HeartbeatRequest{WorkerID: "master-1", Message: "ping"}
	var reply HeartbeatReply
	err = client.Call("Worker.Heartbeat", request, &reply)
	require.NoError(t, err)

	assert.Equal(t, "ok", reply.Status)
	assert.Equal(t, "worker heartbeat received from master-1: ping", reply.Message)
	assert.NotEmpty(t, reply.ServerUTC)
}

func TestWorkerRPCMapReduceEndToEndWithPluginAndMasterCallback(t *testing.T) {
	master, err := NewMaster("127.0.0.1:0")
	require.NoError(t, err)
	master.workerAddresses = []string{}
	require.NoError(t, master.Start())
	defer func() {
		require.NoError(t, master.Close())
	}()

	masterListener, ok := master.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, masterListener.listener)

	pluginPath := buildTempPlugin(t)
	t.Setenv("MAPREDUCE_PLUGIN_PATH", pluginPath)

	intermediateDir := t.TempDir()
	outputDir := t.TempDir()
	t.Setenv("MAPREDUCE_WORKER_INTERMEDIATE_DIR", intermediateDir)
	t.Setenv("MAPREDUCE_WORKER_OUTPUT_DIR", outputDir)
	t.Setenv("MAPREDUCE_N_REDUCE", "2")
	t.Setenv("MAPREDUCE_DFS_SERVER_ADDRESS", "")

	worker, err := NewWorker("127.0.0.1:0")
	require.NoError(t, err)
	worker.masterAddress = masterListener.listener.Addr().String()

	err = worker.Start()
	if err != nil {
		if strings.Contains(err.Error(), "plugin was built with a different version") {
			t.Skipf("skipping plugin integration due Go plugin toolchain mismatch: %v", err)
		}
		require.NoError(t, err)
	}
	defer func() {
		require.NoError(t, worker.Close())
	}()

	workerListener, ok := worker.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, workerListener.listener)

	client, err := rpc.Dial("tcp", workerListener.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	rawContent := []byte("hello world hello map reduce")
	request := MapReduceRequest{
		Handle: Handle{Id: 77, TimeStamp: 1001},
		File: FileMetadata{
			SourcePath: "/inline/chunk",
			SourceLog:  "inline",
			SizeBytes:  int64(len(rawContent)),
			RawContent: rawContent,
		},
		ChunkIndex: 0,
		ChunkInfo: ChunkInfo{
			Index:  0,
			Offset: 0,
			Size:   int64(len(rawContent)),
			Path:   "/inline/chunk",
		},
	}

	var reply MapReduceReply
	err = client.Call("Worker.RPCMapReduce", request, &reply)
	require.NoError(t, err)
	assert.Equal(t, "accepted", reply.Status)

	// Poll for the async result callback from worker.
	result := waitForResult(t, master, "77:0", 10*time.Second)
	assert.Contains(t, result.OutputData, "hello 2")
	assert.Contains(t, result.OutputData, "world 1")
	assert.NotEmpty(t, result.OutputFile)

	data, err := os.ReadFile(result.OutputFile)
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(result.OutputData), strings.TrimSpace(string(data)))
}

func TestWorkerRPCMapReduceEndToEndWithoutPlugin(t *testing.T) {
	master, err := NewMaster("127.0.0.1:0")
	require.NoError(t, err)
	master.workerAddresses = []string{}
	require.NoError(t, master.Start())
	defer func() {
		require.NoError(t, master.Close())
	}()

	masterListener, ok := master.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, masterListener.listener)

	t.Setenv("MAPREDUCE_PLUGIN_PATH", "")
	t.Setenv("MAPREDUCE_PLUGIN_NAME", "")
	t.Setenv("MAPREDUCE_PLUGIN_DIR", t.TempDir())

	intermediateDir := t.TempDir()
	outputDir := t.TempDir()
	t.Setenv("MAPREDUCE_WORKER_INTERMEDIATE_DIR", intermediateDir)
	t.Setenv("MAPREDUCE_WORKER_OUTPUT_DIR", outputDir)
	t.Setenv("MAPREDUCE_N_REDUCE", "2")
	t.Setenv("MAPREDUCE_DFS_SERVER_ADDRESS", "")

	worker, err := NewWorker("127.0.0.1:0")
	require.NoError(t, err)
	worker.masterAddress = masterListener.listener.Addr().String()

	require.NoError(t, worker.Start())
	defer func() {
		require.NoError(t, worker.Close())
	}()

	workerListener, ok := worker.listener.(*RPCListener)
	require.True(t, ok)
	require.NotNil(t, workerListener.listener)

	client, err := rpc.Dial("tcp", workerListener.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	rawContent := []byte("alpha beta alpha")
	request := MapReduceRequest{
		Handle: Handle{Id: 88, TimeStamp: 2002},
		File: FileMetadata{
			SourcePath: "/inline/default",
			SourceLog:  "inline-default",
			SizeBytes:  int64(len(rawContent)),
			RawContent: rawContent,
		},
		ChunkIndex: 0,
		ChunkInfo: ChunkInfo{
			Index:  0,
			Offset: 0,
			Size:   int64(len(rawContent)),
			Path:   "/inline/default",
		},
	}

	var reply MapReduceReply
	err = client.Call("Worker.RPCMapReduce", request, &reply)
	require.NoError(t, err)
	assert.Equal(t, "accepted", reply.Status)

	result := waitForResult(t, master, "88:0", 10*time.Second)
	assert.Contains(t, result.OutputData, "alpha 2")
	assert.Contains(t, result.OutputData, "beta 1")

	data, err := os.ReadFile(result.OutputFile)
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(result.OutputData), strings.TrimSpace(string(data)))
}

func waitForResult(t *testing.T, master *Master, key string, timeout time.Duration) MapReduceResultRequest {
	t.Helper()
	deadline := time.After(timeout)
	for {
		master.resultMux.Lock()
		result, exists := master.results[key]
		master.resultMux.Unlock()
		if exists {
			return result
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for result %s", key)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func buildTempPlugin(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	pluginSrc := filepath.Join(tmpDir, "temp_plugin.go")
	pluginSo := filepath.Join(tmpDir, "temp_plugin.so")

	source := `package main

import (
	"strconv"
	"strings"

	"github.com/caleberi/map_reduce_rpc/mr"
)

func Map(_ string, contents string) []mr.KeyValue {
	words := strings.Fields(contents)
	out := make([]mr.KeyValue, 0, len(words))
	for _, word := range words {
		out = append(out, mr.KeyValue{Key: word, Value: "1"})
	}
	return out
}

func Reduce(_ string, values []string) string {
	return strconv.Itoa(len(values))
}
`

	require.NoError(t, os.WriteFile(pluginSrc, []byte(source), 0o644))

	wd, err := os.Getwd()
	require.NoError(t, err)
	moduleRoot := filepath.Dir(wd)

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginSo, pluginSrc)
	cmd.Dir = moduleRoot
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, fmt.Sprintf("plugin build failed: %s", string(output)))

	return pluginSo
}
