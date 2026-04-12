APP := map_reduce_rpc
GO ?= go

COORD_ADDR ?= localhost:1234
MASTER_ADDR ?= localhost:1235
WORKER_ADDR ?= localhost:1236
DFS_ADDR ?= localhost:8089
PLUGIN_PATH ?= ./plugins/wc/wc.so
UPLOAD_FOLDER ?= ./inputs

.PHONY: help build run-coordinator run-master run-worker run upload-folder test test-mrp fmt clean plugin-wc plugin-indexer plugin-jobcount plugin-mtiming plugin-rtiming plugin-crash plugin-nocrash plugin-early-exit plugin-bigram plugin-charfreq plugin-emailextract plugin-linestats plugin-sentiment plugin-topwords plugin-urlextractor build-all-plugins docker-test docker-test-plugin

help:
	@echo "Targets:"
	@echo "  make build              - Build binary"
	@echo "  make run-coordinator    - Run coordinator"
	@echo "  make run-master         - Run master"
	@echo "  make run-worker         - Run worker"
	@echo "  make run ROLE=worker    - Run selected role (coordinator|master|worker)"
	@echo "  make upload-folder      - Upload files in folder to coordinator"
	@echo "  make test               - Run all tests"
	@echo "  make test-mrp           - Run mrp package tests"
	@echo "  make fmt                - Format code"
	@echo "  make plugin-wc          - Build wc plugin"
	@echo "  make build-all-plugins  - Build all 15 plugins"
	@echo "  make dashboard-api      - Run simulation API (port 4400)"
	@echo "  make dashboard-web-dev  - Run web dashboard dev (port 5173)"
	@echo "  make clean              - Remove generated artifacts"

build:
	$(GO) build -o $(APP) ./

run-coordinator:
	MAPREDUCE_DFS_SERVER_ADDRESS=$(DFS_ADDR) \
	MAPREDUCE_MASTER_SERVER_ADDRESS=$(MASTER_ADDR) \
	$(GO) run . -role=coordinator -addr=$(COORD_ADDR) -dfs-address=$(DFS_ADDR) -master-address=$(MASTER_ADDR)

run-master:
	MAPREDUCE_DFS_SERVER_ADDRESS=$(DFS_ADDR) \
	$(GO) run . -role=master -addr=$(MASTER_ADDR) -dfs-address=$(DFS_ADDR)

run-worker:
	MAPREDUCE_DFS_SERVER_ADDRESS=$(DFS_ADDR) \
	MAPREDUCE_MASTER_SERVER_ADDRESS=$(MASTER_ADDR) \
	MAPREDUCE_PLUGIN_PATH=$(PLUGIN_PATH) \
	$(GO) run . -role=worker -addr=$(WORKER_ADDR) -master-address=$(MASTER_ADDR) -dfs-address=$(DFS_ADDR) -plugin-path=$(PLUGIN_PATH)

run:
	@case "$(ROLE)" in \
		coordinator) $(MAKE) run-coordinator ;; \
		master) $(MAKE) run-master ;; \
		worker) $(MAKE) run-worker ;; \
		*) echo "Usage: make run ROLE=coordinator|master|worker"; exit 1 ;; \
	esac

upload-folder:
	$(GO) run ./cmd/uploader -coordinator=$(COORD_ADDR) -folder=$(UPLOAD_FOLDER)

test:
	$(GO) test ./...

test-mrp:
	$(GO) test ./mrp/...

fmt:
	$(GO) fmt ./...

plugin-wc:
	$(GO) build -buildmode=plugin -o plugins/wc/wc.so ./plugins/wc/wc.go

plugin-indexer:
	$(GO) build -buildmode=plugin -o plugins/indexer/indexer.so ./plugins/indexer/indexer.go

plugin-jobcount:
	$(GO) build -buildmode=plugin -o plugins/jobcount/jobcount.so ./plugins/jobcount/jobcount.go

plugin-mtiming:
	$(GO) build -buildmode=plugin -o plugins/mtiming/mtiming.so ./plugins/mtiming/mtiming.go

plugin-rtiming:
	$(GO) build -buildmode=plugin -o plugins/rtiming/rtiming.so ./plugins/rtiming/rtiming.go

plugin-crash:
	$(GO) build -buildmode=plugin -o plugins/crash/crash.so ./plugins/crash/crash.go

plugin-nocrash:
	$(GO) build -buildmode=plugin -o plugins/nocrash/nocrash.so ./plugins/nocrash/nocrash.go

plugin-early-exit:
	$(GO) build -buildmode=plugin -o plugins/early_exit/early_exit.so ./plugins/early_exit/early_exit.go

plugin-bigram:
	$(GO) build -buildmode=plugin -o plugins/bigram/bigram.so ./plugins/bigram/bigram.go

plugin-charfreq:
	$(GO) build -buildmode=plugin -o plugins/charfreq/charfreq.so ./plugins/charfreq/charfreq.go

plugin-emailextract:
	$(GO) build -buildmode=plugin -o plugins/emailextract/emailextract.so ./plugins/emailextract/emailextract.go

plugin-linestats:
	$(GO) build -buildmode=plugin -o plugins/linestats/linestats.so ./plugins/linestats/linestats.go

plugin-sentiment:
	$(GO) build -buildmode=plugin -o plugins/sentiment/sentiment.so ./plugins/sentiment/sentiment.go

plugin-topwords:
	$(GO) build -buildmode=plugin -o plugins/topwords/topwords.so ./plugins/topwords/topwords.go

plugin-urlextractor:
	$(GO) build -buildmode=plugin -o plugins/urlextractor/urlextractor.so ./plugins/urlextractor/urlextractor.go

build-all-plugins: plugin-wc plugin-indexer plugin-jobcount plugin-mtiming plugin-rtiming plugin-crash plugin-nocrash plugin-early-exit plugin-bigram plugin-charfreq plugin-emailextract plugin-linestats plugin-sentiment plugin-topwords plugin-urlextractor

docker-test: ## Run all plugins via Docker (build once, test each plugin)
	docker compose build
	@for plugin in wc indexer jobcount nocrash bigram charfreq emailextract linestats sentiment topwords urlextractor; do \
		echo "=== Testing plugin: $$plugin ==="; \
		rm -rf ./output/results/*; \
		MAPREDUCE_PLUGIN=$$plugin docker compose up -d; \
		sleep 5; \
		$(GO) run ./cmd/uploader -coordinator=localhost:1234 -folder=$(UPLOAD_FOLDER); \
		echo "Waiting for results..."; \
		sleep 30; \
		echo "--- Results for $$plugin ---"; \
		cat ./output/results/*.json 2>/dev/null || echo "(no results)"; \
		docker compose down; \
		echo ""; \
	done

docker-test-plugin: ## Test a single plugin via Docker: make docker-test-plugin PLUGIN=indexer
	rm -rf ./output/results/*
	MAPREDUCE_PLUGIN=$(or $(PLUGIN),wc) docker compose up --build -d
	sleep 5
	$(GO) run ./cmd/uploader -coordinator=localhost:1234 -folder=$(UPLOAD_FOLDER)
	@echo "Waiting for MapReduce to complete..."
	@sleep 30
	@echo "--- Results ---"
	@cat ./output/results/*.json 2>/dev/null || echo "(no results)"

## ── Dashboard ───────────────────────────────────────────────

dashboard-api: ## Build & run the simulation API server (port 4400)
	cd dashboard/api && $(GO) run .

dashboard-web-install: ## Install web dashboard dependencies
	cd dashboard/web && npm install

dashboard-web-dev: ## Start the web dashboard in dev mode (port 5173)
	cd dashboard/web && npm run dev

dashboard-web-build: ## Production build the web dashboard
	cd dashboard/web && npm run build

dashboard: ## Run both API server + web dev server (requires two terminals)
	@echo "Start each in a separate terminal:"
	@echo "  make dashboard-api      (Go API on :4400)"
	@echo "  make dashboard-web-dev  (Vite dev on :5173)"

clean:
	rm -f $(APP)
	rm -f mr-out-* mr-*-*.json
	rm -rf mrp_intermediate mrp_output
	rm -f plugins/*/*.so
	rm -rf dashboard/web/dist dashboard/web/node_modules
