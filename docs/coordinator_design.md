# Coordinator — Design Architecture

## Overview

The `Coordinator` is the entry-point RPC server for the map-reduce pipeline. It
accepts chunked file uploads from clients (via the uploader CLI), persists them
through a `DurableBuffer` write-ahead log, uploads completed file content and
metadata to a distributed file system (DFS), and triggers map-reduce jobs on
the `Master` node.

The component lives in the `mrp` package and orchestrates the lifecycle between
upload ingestion, durable persistence, DFS upload, and map-reduce dispatch.

## Component Diagram

```
  ┌──────────────┐          RPC            ┌─────────────────────────────────────────────┐
  │   Uploader   │ ─────────────────────►  │              Coordinator                    │
  │   (client)   │  RPCGenerateHandle      │                                             │
  │              │  RPCForwardDownload     │  ┌──────────────────────────────────────┐   │
  └──────────────┘                         │  │           DurableBuffer              │   │
                                           │  │  (in-mem ring buffer + .dlog WAL)    │   │
                                           │  └──────────────┬───────────────────────┘   │
                                           │                 │                           │
                                           │     ┌───────────┴───────────┐               │
                                           │     │  Background ticker    │               │
                                           │     │  (uploadPollInterval) │               │
                                           │     └───────┬───────┬───────┘               │
                                           │             │       │                       │
                                           │             ▼       ▼                       │
                                           │     uploadFile   startMap                   │
                                           │     ToStorage()  Reduce()                   │
                                           └─────────┬────────────┬──────────────────────┘
                                                      │            │
                                          ┌───────────▼──┐   ┌────▼──────────┐
                                          │  DFS (via    │   │  Master       │
                                          │  Hercules)   │   │  (RPC)        │
                                          └──────────────┘   └───────────────┘
```

## Data Flow

### 1. Upload Ingestion

```
Client                          Coordinator                     DurableBuffer
  │                                  │                               │
  ├─ RPCGenerateDownloadHandle ────► │                               │
  │  ◄──── HandleReply {handle} ──── │ ── GenerateHandle() ────────► │
  │                                  │                               │
  ├─ RPCForwardDownload(chunk1) ───► │ ── Write(content) ──────────► │
  ├─ RPCForwardDownload(chunk2) ───► │ ── Write(content) ──────────► │
  ├─ RPCForwardDownload(final,eof) ► │ ── Write(content,eof=true) ─► │
  │                                  │                               │
  │                                  │     handles.Store(h, true)    │
```

### 2. Upload to DFS (ticker)

```
Coordinator                     DurableBuffer              DFS (Hercules)
  │                                  │                          │
  ├─ GetCompletedHandleFor           │                          │
  │  Retransmission() ─────────────► │                          │
  │  ◄── []Handle (completed) ────── │                          │
  │                                  │                          │
  ├─ buildHandleMetadataPayload ───► │                          │
  │  (reads .dlog file content)      │                          │
  │                                  │                          │
  ├─ CreateFile + Write ─────────────┼────────────────────────► │
  │  (metadata JSON w/ RawContent)   │                          │
  │                                  │                          │
  ├─ MarkHandlesUploaded ──────────► │                          │
  │  (moves to uploadedHandles,      │                          │
  │   deletes .dlog file)            │                          │
```

### 3. Map-Reduce Dispatch (ticker)

```
Coordinator                     DurableBuffer              Master
  │                                  │                       │
  ├─ GetUploadedHandles() ─────────► │                       │
  │  ◄── []Handle (new uploads) ──── │                       │
  │                                  │                       │
  ├─ For each handle:                │                       │
  │  ├─ rpc.Dial("Master") ─────────┼─────────────────────► │
  │  ├─ Master.RPCStartMapReduce ───┼─────────────────────► │
  │  │  ◄── StartMapReduceReply ────┼─────────────────────── │
  │  │                               │                       │
  │  └─ On failure:                  │                       │
  │     ReleaseProcessingHandle ───► │                       │
  │     (re-enqueues for next tick)  │                       │
```

## Key Types

| Type                         | Description                                                  |
|------------------------------|--------------------------------------------------------------|
| `Coordinator`                | RPC server, upload orchestrator, DFS uploader, MR dispatcher |
| `completedHandleMetadata`    | JSON payload uploaded to DFS (includes `RawContent`)         |
| `DurableBuffer`              | Write-ahead log managing handle lifecycle                    |
| `Handle`                     | `{Id uint64, TimeStamp int64}` — unique content identity     |
| `DownloadRequest/Reply`      | RPC types for chunked upload                                 |
| `HandleRequest/Reply`        | RPC types for handle generation                              |
| `StartMapReduceRequest/Reply`| RPC types for master dispatch                                |

## Coordinator Struct

```go
type Coordinator struct {
    serverAddress   string                  // bind address for RPC server
    fsServerAddress string                  // DFS (Hercules) server address
    masterAddress   string                  // Master RPC address

    wg              sync.WaitGroup          // tracks background goroutines
    startOnce       sync.Once               // ensures Start() runs once
    stopOnce        sync.Once               // ensures Close() runs once
    startErr        error                   // captures Start() error
    stopCh          chan struct{}            // signals goroutine shutdown

    uploadMu        sync.Mutex              // serializes uploadFileToStorage calls
    logger          zerolog.Logger
    listener        Listener                // RPC listener (RPCListener)
    dfsClient       *hercules.HerculesClient // lazily initialized, reused

    durableLog      *DurableBuffer          // durable write-ahead log
}
```

## Lifecycle

### Startup

```
NewCoordinator(serverAddr, fsAddr)
  │
  ├─ Create DurableBuffer (with recovery mode)
  ├─ Create rpc.Server, register Coordinator
  ├─ Create RPCListener with connection handler
  │
  └─ Return *Coordinator

coordinator.Start()
  │
  ├─ listener.Listen()  (binds TCP)
  ├─ Spawn ticker goroutine:
  │    every 15s: uploadFileToStorage() → startMapReduce()
  └─ Spawn error listener goroutine
```

### Shutdown

```
coordinator.Close()
  │
  ├─ close(stopCh)                    // signal goroutines to exit
  ├─ wg.Wait()                        // wait for goroutines to finish
  ├─ uploadFileToStorage(30s timeout)  // one final upload, race-free
  ├─ listener.Close()                 // stop accepting connections
  └─ durableLog.Close()               // final flush + stop monitor
```

**Key property**: `wg.Wait()` runs *before* the final upload to prevent
concurrent `uploadFileToStorage` calls between the ticker goroutine and the
shutdown path.

## Concurrency Model

| Resource                | Guard                  | Notes                                    |
|------------------------|------------------------|------------------------------------------|
| `uploadFileToStorage`  | `uploadMu` (Mutex)     | Prevents ticker + Close race             |
| `DurableBuffer` maps   | `sync.Map`             | Lock-free concurrent access              |
| `DurableBuffer` ring   | `logMu` (Mutex)        | Serializes Push/Pop/Drain                |
| `dfsClient`            | `uploadMu` (inherited) | Lazily created, reused within uploadMu   |
| `results` (Master)     | `resultMux` (Mutex)    | Serializes result collation              |

## Error Handling & Retry

### Upload to DFS

- Each handle is uploaded independently; a failure for one handle does not
  block others.
- On partial failure, only successfully uploaded handles are passed to
  `MarkHandlesUploaded`. Failed handles remain in the `handles` registry and
  are retried on the next ticker tick.
- `Close()` performs one final upload attempt with a 30-second timeout.

### Map-Reduce Dispatch

- `GetUploadedHandles()` uses `processingHandles.LoadOrStore` to gate handles:
  once returned, a handle is not returned again.
- If `runMapReduceForHandle` fails, the coordinator calls
  `ReleaseProcessingHandle(handle)` to remove the handle from the processing
  registry, allowing it to be re-dispatched on the next tick.
- If the master is unreachable, the RPC dial returns an error and the handle
  is released for retry.

### DFS Client

- A single `hercules.HerculesClient` is lazily created and reused across
  ticker ticks, eliminating per-tick connection overhead.
- If the DFS becomes unreachable, each upload attempt fails independently
  and is retried on the next tick.

## RPC Endpoints

| Method                         | Args                | Reply            | Description                    |
|--------------------------------|---------------------|------------------|--------------------------------|
| `Coordinator.RPCPing`          | `string`            | `*string`        | Health check                   |
| `Coordinator.RPCGenerateDownloadHandle` | `HandleRequest` | `*HandleReply` | Generate a new upload handle   |
| `Coordinator.RPCForwardDownload` | `DownloadRequest` | `*DownloadReply` | Accept a chunk of upload data  |

## Configuration (Environment Variables)

| Variable                          | Default                    | Description                              |
|-----------------------------------|----------------------------|------------------------------------------|
| `MAPREDUCE_DOWNLOAD_LOG_DIR`      | `./mapreduce_download_logs`| Directory for `.dlog` WAL files          |
| `MAPREDUCE_MASTER_SERVER_ADDRESS` | `localhost:1235`           | Master node RPC address                  |
| `MAPREDUCE_DFS_UPLOAD_PREFIX`     | `/mapreduce`               | Remote directory prefix in DFS           |

## Interaction with DurableBuffer

The coordinator is the primary consumer of the `DurableLog` interface:

```
Coordinator method           →  DurableLog method
─────────────────────────────────────────────────────────────
RPCGenerateDownloadHandle    →  GenerateHandle()
RPCForwardDownload           →  Write(content)
uploadFileToStorage          →  GetCompletedHandleForRetransmission()
                             →  MarkHandlesUploaded(handles)
startMapReduce               →  GetUploadedHandles()
startMapReduce (on failure)  →  ReleaseProcessingHandle(handle)
Close                        →  Close()
```

### Handle State Transitions (Coordinator perspective)

```
    RPCForwardDownload          uploadFileToStorage        startMapReduce
    (Eof=true)                  (DFS upload OK)            (Master dispatch)
         │                            │                         │
         ▼                            ▼                         ▼
  ┌────────────┐  GetCompleted  ┌───────────┐  GetUploaded  ┌───────────┐
  │  completed │ ─────────────► │ uploaded   │ ────────────► │processing │
  │  (handles) │                │(uploaded   │               │(processing│
  │            │                │  Handles)  │               │  Handles) │
  └────────────┘                └───────────┘               └─────┬─────┘
                                      │                           │
                                      │ MarkHandlesUploaded       │ On failure:
                                      │ (deletes .dlog)           │ ReleaseProcessingHandle
                                      ▼                           │ (re-enqueues)
                                 ┌──────────┐                     │
                                 │ pruned   │ ◄───────────────────┘
                                 │ (TTL     │    pruneUploadedHandles
                                 │  expiry) │
                                 └──────────┘
```
