# Omni-reducer

<img width="2032" height="1161" alt="Screenshot 2026-04-11 at 22 15 40" src="https://github.com/user-attachments/assets/c8a08ba4-ef9b-4463-93a2-f2feeeb549d3" />

A distributed MapReduce implementation in Go using RPC, a write-ahead durable log, and a pluggable distributed file system (Hercules DFS).

## Architecture

The system is composed of three service roles that communicate over Go's `net/rpc`:

```
  ┌──────────────┐          ┌──────────────────┐
  │  Uploader    │          │  Dashboard Web   │
  │  (CLI tool)  │          │  :8080 (nginx)   │
  └──────┬───────┘          └──────┬───────────┘
         │ RPC: Download            │ HTTP
         │ (chunked upload)         ▼
         │                  ┌──────────────────┐
         │                  │  Dashboard API   │
         │                  │  :4400 (Go)      │
         │                  └──────┬───────────┘
         │                         │ RPC (upload → trigger → poll)
         ▼                         ▼
  ┌──────────────────────────────────────┐
  │           Coordinator  :1234        │
  │  Buffers data → uploads to DFS      │
  │  Triggers Master with plugin name   │
  └──────────────────┬──────────────────┘
                     │ RPC: StartMapReduce(Handle, Plugin)
                     ▼
  ┌──────────────────────────────────────┐
  │             Master  :1235           │
  │  Discovers worker plugins via RPC   │
  │  Routes chunks to matching workers  │
  │  Collects results, writes output    │
  └──────────┬───────────┬──────────────┘
             │           │
     ┌───────┘           └────────┐
     ▼                            ▼
 ┌──────────┐  ┌──────────┐  ┌──────────┐
 │worker-wc │  │worker-idx│  │worker-*  │
 │  :1236   │  │  :1237   │  │ :1238-43 │
 └──────────┘  └──────────┘  └──────────┘
  Each worker loads ONE .so plugin
  and advertises it via RPCGetPluginInfo
```
<img width="2032" height="1161" alt="Screenshot 2026-04-11 at 22 13 55" src="https://github.com/user-attachments/assets/f626dcf8-334f-4167-8301-b30a8f0472d6" />

### Components

| Component | Package | Description |
|-----------|---------|-------------|
| **Coordinator** | `mrp/coordinator.go` | Accepts file uploads via RPC, persists them through a durable write-ahead log (`DurableBuffer`), uploads completed files to Hercules DFS, and triggers the Master to begin MapReduce jobs. Passes the requested plugin name through to the Master. |
| **DurableBuffer** | `mrp/durable_buffer.go` | Write-ahead log backed by a ring buffer with on-disk `.dlog` persistence. Tracks handle lifecycle across three registries (pending, uploaded, processing) with TTL-based expiry. |
| **Master** | `mrp/master.go` | Fetches file metadata from DFS, discovers each worker's loaded plugin via `RPCGetPluginInfo`, routes chunk tasks only to workers matching the requested plugin, collects and collates results, writes final output atomically, and notifies the Coordinator on completion. |
| **Worker** | `mrp/worker.go` | Loads a single Map/Reduce plugin (`.so`) at startup, advertises its plugin name via `RPCGetPluginInfo`, receives chunk assignments from the Master, reads chunk data from DFS, executes the map→partition→reduce pipeline, and submits results back asynchronously. |
| **RPC Listener** | `mrp/rpc_listener.go` | Shared RPC server setup used by all three roles. |
| **Uploader** | `cmd/uploader/` | CLI tool to stream files from a local folder to the Coordinator in configurable chunks. |
| **Dashboard API** | `dashboard/api/` | Go HTTP server that drives real MapReduce pipelines (upload → trigger → poll) or falls back to math-based simulation when the cluster is unavailable. |
| **Dashboard Web** | `dashboard/web/` | React 19 + Vite + TypeScript + Tailwind v4 frontend with plugin selection, real-time topology view, and comparison charts. |

### Data Flow

1. **Ingest** — The uploader CLI (or the Dashboard API) sends file data in chunks to the Coordinator via `RPC: Download`.
2. **Buffer** — The Coordinator writes chunks into the `DurableBuffer` (WAL). On completion it uploads the full file content to Hercules DFS.
3. **Dispatch** — The Coordinator calls `Master.RPCStartMapReduce` with a `Plugin` field. The Master discovers worker plugins via `RPCGetPluginInfo`, routes chunks only to workers that match the requested plugin, and fans out chunk tasks concurrently.
4. **Execute** — Each Worker reads its chunk from DFS, runs the loaded Map function, partitions intermediate key/value pairs, runs the Reduce function, and writes output.
5. **Collect** — Workers submit results back to the Master via `RPC: MapReduceResult`. The Master collates all chunk outputs into a final result file.
6. **Complete** — The Master notifies the Coordinator via `RPC: NotifyJobComplete`, which releases the processing handle.
7. **Dashboard** *(optional)* — The Dashboard API polls `Master.RPCGetJobResult` for completion, then builds real metrics (throughput, latency, phase timing) for the frontend.

## Project Structure

```
.
├── main.go                  # Entrypoint — selects role via -role flag
├── cmd/uploader/main.go     # CLI uploader tool
├── mrp/                     # Core MapReduce packages
│   ├── coordinator.go       # Coordinator service
│   ├── coordinator_test.go  # Coordinator tests
│   ├── durable_buffer.go    # Write-ahead log (WAL)
│   ├── durable_buffer_test.go
│   ├── master.go            # Master service
│   ├── master_test.go       # Master tests
│   ├── worker.go            # Worker service
│   ├── worker_test.go       # Worker tests
│   ├── ringbuffer.go        # Generic ring buffer
│   ├── ringbuffer_test.go
│   ├── rpc_definitions.go   # All RPC request/reply types
│   ├── rpc_listener.go      # Shared RPC server setup
│   ├── types.go             # Shared types (KeyValue, etc.)
│   └── utils.go             # Generic utility functions
├── plugins/                 # MapReduce plugins (each in its own folder)
│   ├── wc/wc.go             # Word count
│   ├── indexer/indexer.go    # Inverted index
│   ├── crash/crash.go       # Crash/delay stress test
│   ├── nocrash/nocrash.go   # Deterministic (no-crash) variant
│   ├── early_exit/early_exit.go  # Early exit test
│   ├── jobcount/jobcount.go # Job invocation counter
│   ├── mtiming/mtiming.go   # Map parallelism test
│   ├── rtiming/rtiming.go   # Reduce parallelism test
│   ├── bigram/bigram.go     # Bigram frequency analysis
│   ├── charfreq/charfreq.go # Character frequency distribution
│   ├── emailextract/emailextract.go  # E-mail address extraction
│   ├── linestats/linestats.go  # Line/word/char counting (wc -lwm)
│   ├── sentiment/sentiment.go  # Sentiment classification
│   ├── topwords/topwords.go    # Top-N word frequency
│   └── urlextractor/urlextractor.go  # URL extraction
├── inputs/                  # Sample input texts (Project Gutenberg)
├── docs/                    # Design documents
├── Makefile                 # Build, run, and test targets
├── dashboard/               # Simulation & monitoring dashboard
│   ├── api/                 # Go HTTP API (port 4400)
│   │   ├── main.go          # API server, real pipeline + sim fallback
│   │   └── Dockerfile
│   └── web/                 # React 19 + Vite + TypeScript frontend
│       ├── src/
│       ├── Dockerfile
│       └── nginx.conf
├── Dockerfile               # Multi-stage Docker build (mrp binary + plugins)
├── docker-compose.yml       # Full stack (coordinator + master + 15 plugin workers + dashboard)
├── test-mr.sh               # Integration test suite
└── test-mr-many.sh          # Repeated trial runner
```

## Prerequisites

- **Go 1.24+** (with plugin support — Linux or macOS, CGO enabled)
- **Hercules DFS** — a running instance of the [distributed-system](https://github.com/caleberi/distributed-system) Hercules file server
- **Redis** — required by Hercules DFS

## Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/caleberi/distributed-system` | Hercules DFS client (`hercules` package) and common types |
| `github.com/rs/zerolog` | Structured JSON logging |
| `github.com/stretchr/testify` | Test assertions |
| `github.com/google/uuid` | Unique ID generation |
| `github.com/redis/go-redis/v9` | Redis client (transitive, via Hercules) |

## Setup

```bash
# Clone the repository
git clone https://github.com/caleberi/map_reduce_rpc.git
cd map_reduce_rpc

# Download Go dependencies
go mod download

# Build the main binary
make build

# Build all plugins
make build-all-plugins
```

### Build a Single Plugin

```bash
make plugin-wc          # Word count
make plugin-indexer     # Inverted index
make plugin-crash       # Crash test
make plugin-nocrash     # No-crash variant
make plugin-early-exit  # Early exit test
make plugin-jobcount    # Job count test
make plugin-mtiming     # Map parallelism test
make plugin-rtiming     # Reduce parallelism test
make plugin-bigram      # Bigram frequency
make plugin-charfreq    # Character frequency
make plugin-emailextract # E-mail extraction
make plugin-linestats   # Line/word/char stats
make plugin-sentiment   # Sentiment analysis
make plugin-topwords    # Top-N words
make plugin-urlextractor # URL extraction
```

## Running

The system is a single binary that switches behavior based on `-role`:

```bash
# Terminal 1 — Start the Coordinator
make run-coordinator
# or: go run . -role=coordinator -addr=localhost:1234 -dfs-address=localhost:8089 -master-address=localhost:1235

# Terminal 2 — Start the Master
make run-master
# or: go run . -role=master -addr=localhost:1235 -dfs-address=localhost:8089

# Terminal 3 — Start a Worker (with the word count plugin)
make run-worker PLUGIN_PATH=./plugins/wc/wc.so
# or: go run . -role=worker -addr=localhost:1236 -master-address=localhost:1235 -dfs-address=localhost:8089 -plugin-path=./plugins/wc/wc.so

# Terminal 4 — Upload input files
make upload-folder UPLOAD_FOLDER=./inputs
# or: go run ./cmd/uploader -coordinator=localhost:1234 -folder=./inputs
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MAPREDUCE_DFS_SERVER_ADDRESS` | `localhost:8089` | Hercules DFS server address |
| `MAPREDUCE_MASTER_SERVER_ADDRESS` | `localhost:1235` | Master RPC address |
| `MAPREDUCE_PLUGIN_PATH` | *(empty)* | Absolute or relative path to the `.so` plugin file |
| `MAPREDUCE_PLUGIN_NAME` | *(empty)* | Plugin name (resolved to `plugins/{name}/{name}.so`). Workers advertise this via `RPCGetPluginInfo`. |
| `MAPREDUCE_PLUGIN_DIR` | `./plugins` | Base directory for plugin resolution |
| `MAPREDUCE_WORKER_ADDRESSES` | *(empty)* | Comma-separated list of worker addresses for the Master |
| `MAPREDUCE_N_REDUCE` | `2` | Number of reduce partitions |
| `MAPREDUCE_COORDINATOR_ADDRESS` | *(empty)* | Coordinator RPC address (used by Dashboard API) |
| `MAPREDUCE_MASTER_ADDRESS` | *(empty)* | Master RPC address (used by Dashboard API) |
| `MAPREDUCE_INPUT_DIR` | `./inputs` | Input file directory (used by Dashboard API) |

### CLI Flags

```
-role          Service role: coordinator | master | worker  (default: coordinator)
-addr          RPC listen address for the selected role     (default: localhost:1234)
-dfs-address   Hercules DFS server address                  (default: localhost:8089)
-master-address  Master RPC address (coordinator/worker)    (default: localhost:1235)
-plugin-path   Path to a worker plugin .so file             (default: "")
```

## Running with Docker

```bash
# Ensure the Hercules DFS network exists:
docker network create hercules-net   # skip if already created

# Start the full stack (coordinator + master + 15 plugin workers + dashboard):
docker compose up --build

# Or in detached mode:
docker compose up --build -d
```

The `docker-compose.yml` starts:

| Service | Port | Description |
|---------|------|-------------|
| `mrp-coordinator` | 1234 | Coordinator RPC |
| `mrp-master` | 1235 | Master RPC |
| `worker-wc` | 1236 | Word count plugin |
| `worker-indexer` | 1237 | Inverted index plugin |
| `worker-jobcount` | 1238 | Job count plugin |
| `worker-mtiming` | 1239 | Map parallelism plugin |
| `worker-rtiming` | 1240 | Reduce parallelism plugin |
| `worker-nocrash` | 1241 | No-crash baseline plugin |
| `worker-crash` | 1242 | Crash stress-test plugin |
| `worker-early-exit` | 1243 | Early exit test plugin |
| `worker-bigram` | 1244 | Bigram frequency plugin |
| `worker-charfreq` | 1245 | Character frequency plugin |
| `worker-emailextract` | 1246 | E-mail extraction plugin |
| `worker-linestats` | 1247 | Line stats plugin |
| `worker-sentiment` | 1248 | Sentiment analysis plugin |
| `worker-topwords` | 1249 | Top-N words plugin |
| `worker-urlextractor` | 1250 | URL extraction plugin |
| `dashboard-api` | 4400 | Dashboard HTTP API |
| `dashboard-web` | 8080 | Dashboard frontend (nginx) |

All services connect to Hercules DFS via the shared `hercules-net` Docker network. The Master auto-discovers each worker's loaded plugin and routes jobs accordingly.

## Testing

```bash
# Run unit tests
make test

# Run only mrp package tests
make test-mrp

# Run the full integration test suite (builds plugins, runs all scenarios)
./test-mr.sh

# Run integration tests N times
./test-mr-many.sh 5
```

### Integration Test Scenarios (`test-mr.sh`)

| Test | Plugin | What it Verifies |
|------|--------|-----------------|
| **wc** | `wc` | Basic word count correctness |
| **indexer** | `indexer` | Inverted index correctness |
| **map parallelism** | `mtiming` | Multiple map tasks run concurrently |
| **reduce parallelism** | `rtiming` | Multiple reduce tasks run concurrently |
| **job count** | `jobcount` | No duplicate task assignments |
| **early exit** | `early_exit` | Output is finalized before any process exits |
| **crash** | `crash` / `nocrash` | Recovery from worker crashes and delays |

## Writing a Custom Plugin

Each plugin is a Go `main` package that exports two functions:

```go
package main

import "github.com/caleberi/map_reduce_rpc/mr"

func Map(filename string, contents string) []mr.KeyValue {
    // Emit key/value pairs from the input
    return []mr.KeyValue{{Key: "word", Value: "1"}}
}

func Reduce(key string, values []string) string {
    // Aggregate values for a given key
    return strconv.Itoa(len(values))
}
```

Place it in `plugins/<name>/<name>.go` and build:

```bash
go build -buildmode=plugin -o plugins/<name>/<name>.so ./plugins/<name>/<name>.go
```

Then run a worker with it:

```bash
make run-worker PLUGIN_PATH=./plugins/<name>/<name>.so
```

## Dashboard

An interactive web dashboard for running real MapReduce jobs and comparing plugin performance.

### Stack

- **API**: Go HTTP server that executes real MapReduce pipelines via the cluster, with math-simulation fallback (`dashboard/api/`)
- **Frontend**: React 19 + Vite + TypeScript + Tailwind CSS v4 + TanStack Router/Query/Table + Recharts + shadcn/ui (`dashboard/web/`)

### Quick Start (Docker)

The dashboard is included in `docker compose up --build`. Open **http://localhost:8080**.

### Quick Start (Development)

```bash
# Terminal 1 — API server (port 4400)
make dashboard-api

# Terminal 2 — Dev server (port 5173, proxies to API)
make dashboard-web-install   # first time only
make dashboard-web-dev
```

Open http://localhost:5173 to access the dev dashboard.

### Features

- **Plugin Selection** — Choose any of the 15 plugins (wc, indexer, jobcount, mtiming, rtiming, nocrash, crash, early_exit, bigram, charfreq, emailextract, linestats, sentiment, topwords, urlextractor) from a dropdown. Jobs are routed to the correct worker automatically.
- **Simulate** — Configure plugin, input size, worker count, chunk size, and network latency. When the cluster is running, the dashboard uploads files, triggers real MapReduce execution, and reports actual metrics. Falls back to math-based simulation when the cluster is unavailable.
- **Compare** — Select multiple plugins and run real jobs for each sequentially. Results are displayed side-by-side with radar charts, stacked duration bars, and a sortable table.
- **Cluster Status** — Live topology view showing coordinator, master, and all worker nodes with online/offline status.
- **Dark/Light Theme** — Toggle between a dark industrial theme and a light theme.

## Credits

- [MapReduce: Simplified Data Processing on Large Clusters](https://research.google/pubs/pub62/) — the original Google paper (OSDI '04)
- [Distributed MapReduce Algorithm and Its Go Implementation](https://yunuskilicdev.medium.com/distributed-mapreduce-algorithm-and-its-go-implementation-12273720ff2f)
- MIT 6.5840 (formerly 6.824) Distributed Systems courseware
