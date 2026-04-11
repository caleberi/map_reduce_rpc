# DurableBuffer — Design Architecture

## Overview

`DurableBuffer` is a **write-ahead log (WAL)** that accepts chunked content via
RPC, buffers it in a lock-free ring buffer, and periodically flushes to `.dlog`
files on disk. It supports crash recovery through replay, tracks upload
acknowledgements, and manages its own lifecycle (flush, deletion, pruning)
through a background monitor goroutine.

The component lives in the `mrp` package and implements the `DurableLog`
interface, making it injectable and testable.

## Component Diagram

```
                        ┌─────────────────────────────────────────┐
                        │              Coordinator                │
                        │                                         │
                        │   RPCDownloadFile ──► durableLog.Write  │
                        │   RPCGenerateHandle ► durableLog.Gen…   │
                        │   uploadFileToStorage ► GetCompleted…   │
                        │   startMapReduce ────► GetUploaded…     │
                        └────────────┬────────────────────────────┘
                                     │ *DurableLog interface*
                        ┌────────────▼────────────────────────────┐
                        │            DurableBuffer                │
                        │                                         │
                        │  ┌──────────┐   ┌────────────────────┐  │
                        │  │RingBuffer│──►│  .dlog files (WAL) │  │
                        │  │(in-mem)  │   │  on disk           │  │
                        │  └──────────┘   └────────────────────┘  │
                        │                                         │
                        │  ┌──────────────────────────────────┐   │
                        │  │ Handle Registries (sync.Map)     │   │
                        │  │  • handles         (pending)     │   │
                        │  │  • uploadedHandles (ack'd)       │   │
                        │  │  • processingHandles (in-flight) │   │
                        │  └──────────────────────────────────┘   │
                        │                                         │
                        │  ┌──────────────────────────────────┐   │
                        │  │ uploaded_handles.dat (on disk)    │   │
                        │  │ crash-safe via tmp+rename         │   │
                        │  └──────────────────────────────────┘   │
                        └─────────────────────────────────────────┘
```

## Data Flow

```
  RPC caller
     │
     ▼
  Write(content)
     │
     ├─ validate handle (not expired, id in range)
     ├─ deep-copy data into ring buffer slot
     ├─ register handle in `handles` map
     │
     ▼
  monitor goroutine (background)
     │
     ├─ cleanupTicker ──► cleanup()
     │                       ├─ syncLogToFile()
     │                       │    ├─ Lock logMu
     │                       │    ├─ Drain ring buffer
     │                       │    ├─ Unlock logMu
     │                       │    └─ syncContentsToFile() (no lock held)
     │                       │         └─ group by handle → append to .dlog
     │                       └─ saveUploadedHandles()
     │
     ├─ logDeletionTicker ──► performLogDeletion()
     │                           ├─ scan .dlog files
     │                           ├─ skip non-uploaded handles
     │                           ├─ delete files past logDeletionDuration
     │                           └─ pruneUploadedHandles()
     │
     └─ manualShutdown ──► cleanup() → return (closes `done` chan)
```

## Key Types

| Type             | Description                                              |
|------------------|----------------------------------------------------------|
| `Handle`         | `{Id uint64, TimeStamp int64}` — unique content identity |
| `Content`        | `{Id Handle, Eof bool, Data []byte}` — a data chunk      |
| `OverflowPolicy` | `DropOldest` (pop head) or `Reject` (return error)       |
| `DurableLog`     | Interface for the durable log (implemented by `DurableBuffer`) |
| `DurableBuffer`  | Concrete WAL implementation                              |

## Handle Lifecycle

```
  GenerateHandle()          MarkHandlesUploaded()        pruneUploadedHandles()
        │                          │                           │
        ▼                          ▼                           ▼
   ┌─────────┐    Write(Eof)  ┌──────────┐    TTL expires  ┌─────────┐
   │ pending │ ─────────────► │ uploaded  │ ──────────────► │ evicted │
   │(handles)│                │(uploaded  │                 │(deleted │
   │         │                │ Handles)  │                 │ from    │
   └─────────┘                └──────────┘                 │ memory) │
                                   │                        └─────────┘
                                   │ GetUploadedHandlesForProcessing()
                                   ▼
                              ┌──────────────┐
                              │ processing   │
                              │(processing   │
                              │ Handles)     │
                              └──────────────┘
```

**Three registries** (`sync.Map`):

- **`handles`** — tracks all known handles; value is `bool` (`true` = Eof
  received, i.e. complete).
- **`uploadedHandles`** — handles acknowledged by the caller via
  `MarkHandlesUploaded`; value is `int64` (upload timestamp in nanoseconds),
  used for TTL-based pruning.
- **`processingHandles`** — subset of uploaded handles that have been handed
  off to a downstream consumer via `GetUploadedHandlesForProcessing`; prevents
  double-dispatch.

## Persistence Model

### WAL Files (`.dlog`)

Each handle gets exactly one file:

```
download_log_{HandleId}-{TimeStamp}.dlog
```

Binary format per record (little-endian):

| Field       | Type     | Size     |
|-------------|----------|----------|
| Handle ID   | uint64   | 8 bytes  |
| Timestamp   | int64    | 8 bytes  |
| Data length | uint32   | 4 bytes  |
| Data        | []byte   | variable |
| Eof flag    | bool     | 1 byte   |

Files are created via `syncContentsToFile` (append mode) and deleted via two
paths:

1. **Eager** — `MarkHandlesUploaded` removes the file immediately after
   persisting the handle to the uploaded registry.
2. **Deferred** — `performLogDeletion` removes files whose mod-time exceeds
   `logDeletionDuration`, as a fallback for files that survived the eager path.

### Uploaded Handles Registry (`uploaded_handles.dat`)

Binary format per entry (little-endian):

| Field      | Type   | Size    |
|------------|--------|---------|
| Handle ID  | uint64 | 8 bytes |
| Timestamp  | int64  | 8 bytes |
| Uploaded At| int64  | 8 bytes |

Written atomically via temp-file + `os.Rename` to survive crashes. The
`uploadedAt` field (not the handle's creation timestamp) drives TTL expiry,
ensuring the pruning window starts from when the upload was acknowledged.

## Timing Invariants

### SafeLogDeletionDuration

```
logDeletionDuration = (handleExpiryDuration + cleanupTimeInterval + expectedRecoveryTime) × safetyFactor
```

The constructor enforces `logDeletionDuration ≥ SafeLogDeletionDuration(…, 2)`
to prevent deletion before replay can complete.

### uploadedHandleTTL ≥ logDeletionDuration

The uploaded-handle registry must outlive the log files it guards. If TTL were
shorter, `pruneUploadedHandles` could evict a handle entry before
`performLogDeletion` removes the file, causing `Replay` to re-process it
(duplicate delivery).

### cleanupTimeInterval > 0

Guarded in the constructor with a default of 30 seconds to prevent a
`time.NewTicker(0)` panic.

## Concurrency Model

| Mechanism            | Protects                                 |
|----------------------|------------------------------------------|
| `RingBuffer` (CAS)  | In-memory content slots (MPMC, lock-free)|
| `logMu` (Mutex)     | Drain + Write atomicity for ring buffer  |
| `sync.Map`          | Handle registries (concurrent R/W)       |
| `atomic.Bool`       | `closed`, `inRecovery` flags             |
| `atomic.Uint64`     | `nextHandle` monotonic counter           |
| `atomic.Int64`      | `lastTimestamp` monotonic clock           |
| `atomic.Uint32`     | `overflowPolicy` hot-swappable setting   |

The `logMu` mutex is held only during the ring buffer drain (a fast in-memory
operation). All disk I/O (`syncContentsToFile`, `saveUploadedHandles`) runs
outside the lock.

## Crash Recovery

When `recoveryMode = true`, the `monitor` goroutine replays all `.dlog` files
on startup before entering its normal tick loop:

1. Set `inRecovery = true` (suppresses handle expiry checks).
2. Call `Replay(visitor)` — reads every `.dlog` in sorted order, pushes
   content back into the ring buffer, and rebuilds the `handles` registry.
3. Advance `nextHandle` to `max(replayed ID) + 1`.
4. Set `inRecovery = false`.

The `uploaded_handles.dat` file is loaded in `NewDurableBuffer`, so
`isHandleUploaded` correctly skips already-processed content during replay.

## Shutdown

`Close()` is idempotent (guarded by `closed` CAS). It:

1. Closes the `manualShutdown` channel, signalling the monitor goroutine.
2. Blocks on `<-done` until the monitor has run its final `cleanup()` and
   returned.

The caller is responsible for signal handling (SIGINT/SIGTERM). The library
does not install process-global signal handlers.

## Interface

```go
type DurableLog interface {
    GenerateHandle() Handle
    Write(content Content) error
    Replay(visitor func(Content) bool)
    Close() error
    SetOverflowPolicy(policy OverflowPolicy)
    MarkHandlesUploaded(handles []Handle)
    GetCompletedHandleForRetransimision() []Handle
    GetUploadedHandlesForProcessing() []Handle
    LogDir() string
}
```

This interface enables the coordinator (and tests) to swap in alternative
implementations without depending on the concrete `DurableBuffer` type.
