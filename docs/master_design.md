# Master — Design Architecture

## Overview

The `Master` is the central orchestration node of the map-reduce pipeline. It
receives job requests from the `Coordinator`, fetches file metadata from the
distributed file system (DFS), dispatches chunk-level map-reduce tasks to
`Worker` nodes in parallel, collects results, and writes the final collated
output to disk.

The component lives in the `mrp` package and bridges the gap between durable
storage (DFS) and compute (Workers).

## Component Diagram

```
                                 ┌──────────────────────────────────────────────────┐
  ┌─────────────┐   RPC         │                   Master                         │
  │ Coordinator │ ────────────► │                                                  │
  │             │ RPCStartMap   │  ┌────────────┐     ┌──────────────────────────┐ │
  └─────────────┘  Reduce       │  │  jobs      │     │  results                 │ │
                                │  │  (tracking)│     │  map[handleId:chunk] →   │ │
                                │  └────────────┘     │  MapReduceResultRequest  │ │
                                │                     └──────────────────────────┘ │
                                │         │                       ▲                │
                                │         │ dispatch              │ callback       │
                                │         ▼                       │                │
                                │  ┌──────────────┐    ┌─────────┴──────────┐      │
                                │  │ Worker Pool  │    │ RPCSubmitMapReduce │      │
                                │  │ (concurrent) │    │ Result             │      │
                                │  └──────┬───────┘    └────────────────────┘      │
                                └─────────┼────────────────────────────────────────┘
                                          │
                           ┌──────────────┼──────────────┐
                           ▼              ▼              ▼
                      ┌─────────┐   ┌─────────┐   ┌─────────┐
                      │Worker 0 │   │Worker 1 │   │Worker N │
                      └─────────┘   └─────────┘   └─────────┘
                                          │
                                          ▼
                                  ┌───────────────┐
                                  │ DFS (Hercules) │
                                  └───────────────┘
```

## Data Flow

### 1. Job Dispatch (Coordinator → Master → Workers)

```
Coordinator                     Master                          DFS           Workers
  │                               │                              │               │
  ├─ RPCStartMapReduce ────────► │                              │               │
  │  {Handle}                     │                              │               │
  │                               ├─ fetchFileMetadataFromDFS ─► │               │
  │                               │  ◄── FileMetadata{Chunks} ── │               │
  │                               │                              │               │
  │                               ├─ jobs[handle.Id] = jobMeta   │               │
  │                               │  {expectedChunks, time}      │               │
  │                               │                              │               │
  │                               ├─ goroutine per chunk ────────┼────────────► │
  │                               │  Worker.RPCMapReduce         │  chunk 0     │
  │                               │  Worker.RPCMapReduce         │  chunk 1     │
  │                               │  Worker.RPCMapReduce         │  chunk N     │
  │                               │                              │               │
  │                               ├─ collect dispatch results    │               │
  │                               │                              │               │
  │  ◄── "accepted"|"partial"|    │                              │               │
  │      "error"                  │                              │               │
```

### 2. Result Collection (Workers → Master → Disk)

```
Workers                         Master                          Disk
  │                               │                              │
  ├─ RPCSubmitMapReduceResult ──► │                              │
  │  {Handle, ChunkIndex, Data}   │                              │
  │                               ├─ results[key] = request      │
  │                               │                              │
  │                               ├─ received < expected?        │
  │                               │  YES → reply "success"       │
  │                               │  (wait for more chunks)      │
  │                               │                              │
  │  ... more chunks arrive ...   │                              │
  │                               │                              │
  ├─ RPCSubmitMapReduceResult ──► │                              │
  │  (final chunk)                │                              │
  │                               ├─ received == expected         │
  │                               │                              │
  │                               ├─ collate all chunks          │
  │                               │  (sort by ChunkIndex)        │
  │                               │                              │
  │                               ├─ write tmp file ────────────► │
  │                               ├─ rename tmp → final ────────► │
  │                               │                              │
  │                               ├─ delete(jobs[handle.Id])     │
  │                               │                              │
  │  ◄── "all N chunks collated"  │                              │
```

## Key Types

| Type                      | Description                                              |
|---------------------------|----------------------------------------------------------|
| `Master`                  | RPC server, job orchestrator, result collator             |
| `jobMeta`                 | Tracks expected chunk count and dispatch time per handle  |
| `FileMetadata`            | DFS file info including chunk list and raw content        |
| `ChunkInfo`               | Per-chunk offset, size, DFS path, and chunk handle        |
| `StartMapReduceRequest`   | Coordinator → Master: handle to process                   |
| `StartMapReduceReply`     | Master → Coordinator: status (`accepted`/`partial`/`error`) |
| `MapReduceRequest`        | Master → Worker: file metadata + specific chunk           |
| `MapReduceResultRequest`  | Worker → Master: chunk output data and metadata           |

## Master Struct

```go
type Master struct {
    serverAddress   string                  // bind address for RPC server
    fsServerAddress string                  // DFS (Hercules) server address
    workerAddresses []string                // configured worker RPC addresses

    resultMux       sync.Mutex              // guards results map
    results         map[string]MapReduceResultRequest  // "handleId:chunkIdx" → result

    jobsMu          sync.Mutex              // guards jobs map
    jobs            map[uint64]jobMeta      // handle ID → {expectedChunks, dispatchedAt}

    wg              sync.WaitGroup          // tracks background goroutines
    startOnce       sync.Once               // ensures Start() runs once
    stopOnce        sync.Once               // ensures Close() runs once
    startErr        error                   // captures Start() error
    stopCh          chan struct{}            // signals goroutine shutdown

    logger          zerolog.Logger
    listener        Listener                // RPC listener (RPCListener)
    dfsClient       *hercules.HerculesClient // lazily initialized, reused
}
```

## Lifecycle

### Startup

```
NewMaster(serverAddr)
  │
  ├─ Parse MAPREDUCE_WORKER_ADDRESSES (comma-separated, deduped)
  ├─ Create rpc.Server, register Master
  ├─ Create RPCListener with connection handler
  │
  └─ Return *Master

master.Start()
  │
  ├─ listener.Listen()  (binds TCP)
  └─ Spawn error listener goroutine
```

### Shutdown

```
master.Close()
  │
  ├─ close(stopCh)        // signal goroutines to exit
  ├─ listener.Close()     // stop accepting connections
  └─ wg.Wait()            // wait for goroutines to finish
```

## Concurrency Model

| Resource          | Guard              | Notes                                         |
|-------------------|--------------------|-----------------------------------------------|
| `results` map     | `resultMux`        | Stores all incoming chunk results              |
| `jobs` map        | `jobsMu`           | Tracks expected chunk counts per handle        |
| `dfsClient`       | Created once only  | Lazily initialized via `getOrCreateDFSClient`  |
| Chunk dispatch    | Per-goroutine      | One goroutine per chunk, `WaitGroup` barrier   |
| Result collation  | `resultMux` held   | Scan + collect happens under single lock hold  |

### Dispatch Concurrency

Chunks are dispatched to workers in parallel using one goroutine per chunk.
A `sync.WaitGroup` gates the dispatch phase; all results are collected into a
buffered channel and tallied after the barrier.

```
RPCStartMapReduce
  │
  ├─ for each chunk → go dispatch(chunk)
  │    ├─ check ctx.Done()
  │    ├─ rpc.Dial(worker)
  │    ├─ Worker.RPCMapReduce
  │    └─ send result to channel
  │
  ├─ wg.Wait()
  ├─ close(resultsCh)
  └─ tally dispatched vs failed
```

## Dispatch Status Semantics

| Reply Status | Meaning                                        | Coordinator Action          |
|-------------|------------------------------------------------|-----------------------------|
| `accepted`  | All chunks dispatched successfully              | Handle stays in processing  |
| `partial`   | Some chunks dispatched, some failed             | Logs warning, keeps handle  |
| `error`     | All dispatches failed or precondition not met   | Releases handle for retry   |

## Job Completion Tracking

The `jobs` map records how many chunks were dispatched per handle ID:

```
RPCStartMapReduce  →  jobs[handle.Id] = {expectedChunks: N, dispatchedAt: now}
                           │
RPCSubmitResult(chunk 1)   │  received < expected → accept, wait
RPCSubmitResult(chunk 2)   │  received < expected → accept, wait
  ...                      │
RPCSubmitResult(chunk N)   │  received == expected → collate + write + cleanup
                           │
                           └─ delete(jobs[handle.Id])
```

For untracked handles (late duplicates, results arriving after job cleanup),
results are stored but collation is skipped.

## Result Collation

When the final chunk arrives:

1. All results for the handle are collected under `resultMux` lock
2. Chunks are sorted by `ChunkIndex`
3. `OutputData` is concatenated with newline separators
4. JSON payload is written to a **temp file** (`.tmp` suffix)
5. `os.Rename` atomically promotes the temp file to the final path
6. Job entry is removed from the `jobs` tracker

### Collated Result Structure

```json
{
  "handle": {"Id": 42, "TimeStamp": 1234567890},
  "chunk_count": 3,
  "chunks": [
    {"chunk_index": 0, "worker_addr": "...", "output_data": "...", ...},
    {"chunk_index": 1, ...},
    {"chunk_index": 2, ...}
  ],
  "combined_output": "...all chunk outputs concatenated...",
  "last_updated_at": "2026-04-09T..."
}
```

## DFS Interaction

### Metadata Fetch

`fetchFileMetadataFromDFS` resolves the file uploaded by the Coordinator:

1. Constructs the expected filename: `download_log_{handleId}-{timestamp}.json`
2. Tries multiple DFS prefix paths (configured + fallbacks)
3. Fetches `fileInfo` (length, chunk count) from the first matching path
4. For each DFS chunk: retrieves the chunk handle and computes offset/size
5. Returns `FileMetadata` with the full chunk list

The DFS client is lazily created and reused across calls via
`getOrCreateDFSClient(ctx)`.

### Upload Prefix Resolution

```
resolveUploadPrefixes()
  │
  ├─ MAPREDUCE_DFS_UPLOAD_PREFIX (env, default: /mapreduce)
  ├─ /mapreduce                  (fallback)
  └─ /mapreduce/download_logs    (fallback)
  │
  └─ Deduplicated, cleaned, returned as []string
```

## RPC Endpoints

| Method                           | Caller      | Args                      | Reply                  | Description                        |
|----------------------------------|-------------|---------------------------|------------------------|------------------------------------|
| `Master.Heartbeat`               | Any         | `HeartbeatRequest`        | `*HeartbeatReply`      | Health check                       |
| `Master.RPCStartMapReduce`       | Coordinator | `StartMapReduceRequest`   | `*StartMapReduceReply` | Fetch metadata, dispatch to workers|
| `Master.RPCSubmitMapReduceResult`| Worker      | `MapReduceResultRequest`  | `*MapReduceResultReply`| Accept chunk result, collate when complete|

## Error Handling & Retry

### Dispatch Failures

- Each chunk is dispatched independently in its own goroutine.
- A 30-second `context.WithTimeout` bounds the entire dispatch phase.
- If all chunks fail, the job entry is removed from `jobs` and `"error"` is
  returned to the coordinator, which calls `ReleaseProcessingHandle` to allow
  retry on the next tick.
- If some chunks fail (`"partial"`), the coordinator logs a warning. Partially
  dispatched chunks that succeed will still submit results.

### DFS Fetch Failures

- Multiple DFS prefix paths are tried in sequence.
- All errors are accumulated via `errors.Join` and returned as a single error.
- The coordinator treats `"error"` replies as dispatch failures and retries.

### Late / Duplicate Results

- Results for handles not in the `jobs` tracker are accepted into `results`
  but do not trigger collation or file writes.

## Configuration (Environment Variables)

| Variable                          | Default           | Description                              |
|-----------------------------------|-------------------|------------------------------------------|
| `MAPREDUCE_DFS_SERVER_ADDRESS`    | `localhost:8089`  | Hercules DFS server address              |
| `MAPREDUCE_WORKER_ADDRESSES`      | `""` (empty)      | Comma-separated worker RPC addresses     |
| `MAPREDUCE_DFS_UPLOAD_PREFIX`     | `/mapreduce`      | Remote DFS directory prefix              |
| `MAPREDUCE_MASTER_RESULT_DIR`     | `./results`       | Local directory for collated result files |

## Interaction with Coordinator

```
Coordinator method             →  Master RPC
──────────────────────────────────────────────────────
runMapReduceForHandle          →  Master.RPCStartMapReduce
  (on "error" reply)           →  ReleaseProcessingHandle (retry)
  (on "partial" reply)         →  logs warning, handle stays processing
  (on "accepted" reply)        →  handle stays processing
```

## Interaction with Workers

```
Master method                  →  Worker RPC
──────────────────────────────────────────────────────
RPCStartMapReduce (dispatch)   →  Worker.RPCMapReduce (per chunk)
                               ◄  Worker.RPCSubmitMapReduceResult (callback)
```

### Worker Assignment

Chunks are assigned to workers using round-robin on the chunk index:

```
workerAddr = workerAddresses[chunkIndex % len(workerAddresses)]
```

## Handle Lifecycle (Master Perspective)

```
    RPCStartMapReduce           RPCSubmitMapReduceResult
    (from Coordinator)          (from Workers, final chunk)
         │                            │
         ▼                            ▼
  ┌────────────┐  dispatch    ┌────────────┐  collate   ┌───────────┐
  │  received  │ ───────────► │ dispatched │ ─────────► │ completed │
  │            │              │ (in jobs)  │            │ (written) │
  └────────────┘              └────────────┘            └───────────┘
         │                          │                         │
         │ all fail                 │ partial results         │ delete(jobs[id])
         ▼                          │ still accumulate        ▼
  ┌────────────┐                    │                  ┌───────────┐
  │  rejected  │ ◄─────────────────┘ (on total fail)  │  cleaned  │
  │  (retry)   │                                       │  up       │
  └────────────┘                                       └───────────┘
```
