# Worker Design Document

## Overview

The Worker is the execution node in the MapReduce pipeline. It receives chunk
assignments from the Master, runs user-defined map and reduce functions against
the data, writes output files, and reports results back to the Master. Workers
are stateless with respect to job orchestration — the Master owns scheduling and
completion tracking.

## Position in the Pipeline

```
Coordinator → Master → Worker(s) → Master (result callback)
```

1. The **Coordinator** ingests data into the DurableBuffer, uploads completed
   handles to DFS, then tells the Master to begin processing.
2. The **Master** fetches file metadata from DFS, splits the file into chunks,
   and dispatches each chunk to a Worker via `Worker.RPCMapReduce`.
3. Each **Worker** accepts the chunk, runs the map/reduce pipeline
   asynchronously, then calls `Master.RPCSubmitMapReduceResult` with the output.
4. When all chunks are received, the Master collates results and notifies the
   Coordinator via `Coordinator.RPCNotifyJobComplete`.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                      Worker                          │
│                                                      │
│  ┌──────────────┐                                    │
│  │ RPC Listener │◄── Master dispatches chunks        │
│  └──────┬───────┘                                    │
│         │                                            │
│         ▼                                            │
│  RPCMapReduce (accepts immediately, returns          │
│  "accepted", spawns background goroutine)            │
│         │                                            │
│         ▼                                            │
│  ┌────────────┐     ┌──────────────┐                 │
│  │ readChunk  │────►│  mapf()      │  (Map phase)    │
│  │ Data       │     └──────┬───────┘                 │
│  └────────────┘            │                         │
│                            ▼                         │
│                  ┌──────────────────┐                 │
│                  │ arrangeImmediate │  (Partition)    │
│                  │ (hash → bucket)  │                 │
│                  └────────┬─────────┘                 │
│                           │                          │
│              ┌────────────┼────────────┐             │
│              ▼            ▼            ▼             │
│         partition 0  partition 1  partition N         │
│         (write JSON) (write JSON) (write JSON)       │
│              │            │            │             │
│              └────────────┼────────────┘             │
│                           ▼                          │
│                  ┌──────────────────┐                 │
│                  │ runReduceFrom    │  (Reduce phase) │
│                  │ Channel          │                 │
│                  └────────┬─────────┘                 │
│                           │                          │
│                           ▼                          │
│                  ┌──────────────────┐                 │
│                  │ Atomic write     │                 │
│                  │ output file      │                 │
│                  └────────┬─────────┘                 │
│                           │                          │
│                           ▼                          │
│                  ┌──────────────────┐                 │
│                  │ submitResultTo   │────► Master     │
│                  │ Master (RPC)     │                 │
│                  └──────────────────┘                 │
│                                                      │
│  cleanup: intermediate files removed after pipeline  │
└──────────────────────────────────────────────────────┘
```

## Struct Definition

```go
type Worker struct {
    serverAddress string            // This worker's RPC listen address
    masterAddress string            // Master RPC address for result submission
    fsServerAddr  string            // DFS (Hercules) server address
    pluginName    string            // Name of the .so plugin
    pluginPath    string            // Resolved path to the .so plugin
    nReduce       int               // Number of reduce partitions
    intermediate  string            // Directory for intermediate partition files
    outputDir     string            // Directory for final output files

    mapf    func(string, string) []mr.KeyValue  // Map function (from plugin or default)
    reducef func(string, []string) string       // Reduce function (from plugin or default)

    wg        sync.WaitGroup        // Tracks in-flight async pipelines
    startOnce sync.Once
    stopOnce  sync.Once
    startErr  error
    stopCh    chan struct{}          // Closed on shutdown; cancels in-flight work

    dfsClient   *hercules.HerculesClient  // Lazily initialised, reused
    dfsClientMu sync.Mutex

    logger   zerolog.Logger
    listener Listener               // TCP RPC listener
}
```

## Configuration (Environment Variables)

| Variable | Default | Description |
|---|---|---|
| `MAPREDUCE_MASTER_SERVER_ADDRESS` | `localhost:1235` | Master RPC address for result callbacks |
| `MAPREDUCE_DFS_SERVER_ADDRESS` | `localhost:8089` | Hercules DFS server for chunk reads |
| `MAPREDUCE_PLUGIN_NAME` | (empty) | Plugin `.so` name (looked up in `MAPREDUCE_PLUGIN_DIR`) |
| `MAPREDUCE_PLUGIN_PATH` | (empty) | Explicit path to plugin `.so` (overrides name) |
| `MAPREDUCE_PLUGIN_DIR` | `./plugins` | Directory to search for plugin by name |
| `MAPREDUCE_N_REDUCE` | `1` | Number of reduce partitions per chunk |
| `MAPREDUCE_WORKER_INTERMEDIATE_DIR` | `./mrp_intermediate` | Intermediate partition file directory |
| `MAPREDUCE_WORKER_OUTPUT_DIR` | `./mrp_output` | Final output file directory |

## Lifecycle

### Startup

1. `NewWorker(address)` — creates the worker, registers it as an RPC service.
2. `Start()` — loads the map/reduce plugin (if configured), starts the TCP
   listener, begins draining listener errors.

### Shutdown

1. `Close()` — closes `stopCh` (signals all in-flight goroutines to cancel),
   stops the RPC listener, then `wg.Wait()` blocks until every background
   pipeline finishes or is cancelled.

## RPC Interface

### `Worker.RPCMapReduce(request, reply)`

The primary entry point. Called by the Master for each chunk.

**Request fields:**
- `Handle` — identifies the job
- `File` — full `FileMetadata` (may include `RawContent` for inline data)
- `ChunkIndex` — which chunk of the file this is
- `ChunkInfo` — offset, size, and DFS path for the chunk

**Behaviour:**
- Returns `"accepted"` immediately to unblock the Master dispatch goroutine.
- Spawns a background goroutine (tracked by `wg`) that:
  1. Cleans stale intermediate files from prior attempts (dedup guard)
  2. Reads chunk data (inline `RawContent` or DFS)
  3. Runs the map/reduce pipeline
  4. Cleans intermediate files
  5. Submits results to Master via `Master.RPCSubmitMapReduceResult`
- On shutdown (`stopCh` closed), the context is cancelled, aborting DFS reads.

### `Worker.Heartbeat(request, reply)`

Health check endpoint. Returns `"ok"` with server UTC time.

## Data Flow

### 1. Chunk Read (`readChunkData`)

Two paths:
- **Inline** — if `request.File.RawContent` is non-empty, slice out
  `[offset : offset+size]` directly. Zero-copy from the dispatched request.
- **DFS** — connect to Hercules via a lazily-created, reused client. Read
  `size` bytes at `offset` from the chunk's DFS path. Context-aware: checks
  for cancellation before starting the read.

### 2. Map Phase

Calls `mapf(chunkPath, chunkDataString)` → returns `[]mr.KeyValue`.

The map function is loaded from a Go plugin (`.so`) at startup. If no plugin
is configured, the built-in `defaultMap` tokenises by whitespace and emits
`{word, "1"}` pairs (word count).

### 3. Partition (`arrangeImmediate`)

Hashes each key (`ihash(key) % nReduce`) into one of `nReduce` buckets. Each
bucket is written to an intermediate JSON file:

```
{intermediate}/mr-{handleId}-{chunkIdx}-{reduceIdx}.json
```

Partition writes run concurrently (one goroutine per bucket).

### 4. Reduce Phase (`runReduceFromChannel`)

Reads intermediate files as they arrive via a channel, decodes all key-value
pairs, sorts by key, groups consecutive equal keys, and calls
`reducef(key, values)` for each group.

JSON decode errors are detected and surfaced (not silently swallowed).

### 5. Output Write

The reduce output is written atomically (temp file → `os.Rename`) to:

```
{outputDir}/mr-out-{handleId}-{chunkIdx}
```

### 6. Result Submission (`submitResultToMaster`)

Opens an RPC connection to the Master with a 15-second dial timeout. Sends:
- Handle, ChunkIndex, WorkerAddr
- OutputFile path and OutputData string
- GeneratedAt timestamp

The Master uses this to track chunk completion and collate final results.

### 7. Cleanup

After the pipeline completes (success or failure), intermediate partition files
matching `mr-{handleId}-{chunkIdx}-*.json` are glob-deleted. Stale files from
prior failed attempts are also cleaned before the pipeline starts.

## Plugin System

Workers load map/reduce functions from Go plugins (`.so` shared objects) at
startup:

```go
p, err := plugin.Open(resolvedPath)
mapSymbol, _ := p.Lookup("Map")    // func(string, string) []mr.KeyValue
reduceSymbol, _ := p.Lookup("Reduce") // func(string, []string) string
```

Plugin resolution order:
1. `MAPREDUCE_PLUGIN_PATH` (explicit path, highest priority)
2. `MAPREDUCE_PLUGIN_DIR` / `MAPREDUCE_PLUGIN_NAME` + `.so`
3. If neither is set, built-in defaults are used (word count)

## Concurrency Model

```
RPCMapReduce (RPC handler thread)
    │
    ├── returns "accepted" immediately
    │
    └── goroutine (tracked by wg)
         │
         ├── context derived from stopCh (cancelled on shutdown)
         │
         ├── readChunkData (inline or DFS, context-aware)
         │
         ├── runMapReducePipeline
         │    ├── mapf() (sequential)
         │    ├── partition writes (concurrent, one goroutine per bucket)
         │    └── reduce (sequential, reads from channel as partitions complete)
         │
         ├── cleanupIntermediateFiles
         │
         └── submitResultToMaster (RPC with 15s dial timeout)
```

Key properties:
- `wg.Add(1)` before goroutine launch; `wg.Done()` on completion.
- `Close()` waits for `wg.Wait()` — graceful drain of in-flight work.
- Context cancellation propagates to DFS reads on shutdown.
- Each pipeline goroutine is independent — multiple chunks process in parallel.

## Error Handling

| Stage | Failure mode | Behaviour |
|---|---|---|
| Plugin load | Missing/incompatible `.so` | `Start()` returns error, worker does not listen |
| Chunk read | DFS unreachable | Logged, goroutine exits (Master's job tracker will time out) |
| Map/Reduce | Panic in user function | Unrecovered (process-level crash) |
| JSON decode | Corrupt intermediate file | Error returned, pipeline fails, logged |
| Output write | Disk full / permission | Error returned, pipeline fails, logged |
| Result submit | Master unreachable | Logged, goroutine exits (Master's job tracker times out; Coordinator retries via processingHandles TTL) |

## File Layout

```
mrp_intermediate/
    mr-{handleId}-{chunkIdx}-0.json     # Partition files (temporary)
    mr-{handleId}-{chunkIdx}-1.json
    ...
mrp_output/
    mr-out-{handleId}-{chunkIdx}        # Final reduce output
    mr-out-{handleId}-{chunkIdx}.tmp    # Temp file during atomic write
```

## Relationship to Other Components

### Master → Worker

- Master calls `Worker.RPCMapReduce` with chunk assignment
- Master checks reply status (`"accepted"` / `"error"`)
- Workers are addressed by `MAPREDUCE_WORKER_ADDRESSES` on the Master side
- Chunks are round-robin distributed: `workerAddresses[chunkIdx % len(workers)]`

### Worker → Master

- Worker calls `Master.RPCSubmitMapReduceResult` asynchronously after pipeline
  completes
- Master tracks expected chunk count; when all received, writes collated result
- If Worker crashes after accepting, the Master's `jobs` entry remains until
  the Coordinator's processingHandles TTL expires and triggers a re-dispatch

### Worker → DFS

- Reads chunk data from Hercules DFS when `RawContent` is not available inline
- Single DFS client per worker (lazy init, mutex-protected)
- Context-aware: shutdown cancels in-flight reads
