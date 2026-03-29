BINARY := bin/goroscope
VERSION ?= dev

.PHONY: build run run-react run-react-complex run-react-rich dev-react ui fmt test test-race vet lint lint-fix bench web vscode pre-commit embed-web build-dist docker docker-push docker-compose-up docker-compose-down gen-client wasm e2e

build:
	mkdir -p bin
	go build -ldflags "-X github.com/Khachatur86/goroscope/internal/version.Version=$(VERSION)" -trimpath -o $(BINARY) ./cmd/goroscope

run:
	go run ./cmd/goroscope run ./app

run-react: build web
	./bin/goroscope run -ui=react -ui-path=web/dist -open-browser ./examples/trace_demo

run-react-complex: build web
	./bin/goroscope run -ui=react -ui-path=web/dist -open-browser ./examples/complex_demo

run-react-rich: build web
	./bin/goroscope run -ui=react -ui-path=web/dist -open-browser ./examples/rich_demo

# dev-react: Go API server (real static analysis on :7070) + Vite dev server
# (hot reload on :5173).  Vite proxies /api → :7070 automatically.
#
# Usage:
#   make dev-react          # open http://localhost:5173, click "Code" tab → Analyze
dev-react: build
	@./bin/goroscope ui & \
	BGPID=$$!; \
	trap "kill $$BGPID 2>/dev/null; exit" INT TERM EXIT; \
	echo "Waiting for Go server on :7070..."; \
	for i in $$(seq 1 20); do \
		if curl -sf http://127.0.0.1:7070/healthz >/dev/null 2>&1; then \
			echo "Go server ready."; break; \
		fi; \
		sleep 0.5; \
	done; \
	cd web && npm install --silent && npm run dev; \
	kill $$BGPID 2>/dev/null || true

ui:
	go run ./cmd/goroscope ui

fmt:
	go fmt ./...

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run --timeout=5m

lint-fix:
	gofmt -w .
	golangci-lint run --fix --timeout=5m

# Run before commit: fmt, vet, test-race, lint
pre-commit: fmt vet test-race lint
	@echo "All checks passed. Safe to commit."

bench:
	go test -bench=. -benchmem ./internal/tracebridge/... ./internal/analysis/...

web:
	cd web && npm install && npm run build

# e2e: build React UI and run Playwright E2E tests (headless Chromium).
# Usage:
#   make e2e
e2e: web
	cd web && npm run test:e2e

# embed-web: build the React UI and copy it into the Go embed directory so that
# the next `make build` (or `make build-dist`) produces a self-contained binary.
embed-web: web
	rm -rf internal/api/reactui/assets
	cp -r web/dist/. internal/api/reactui/

# build-dist: single-command release build — React bundle baked into the binary.
build-dist: embed-web build

ui-react: build web
	./bin/goroscope ui -ui=react -ui-path=web/dist -open-browser

vscode:
	cd vscode && npm install && npm run compile

# ── OpenAPI client generation (I-1) ───────────────────────────────────────────
# Generates a TypeScript type definition file from the embedded OpenAPI spec.
# Requires: npm install -g openapi-typescript  (or npx openapi-typescript)
#
# Usage:
#   make gen-client           # generate web/src/api/schema.d.ts
gen-client:
	npx openapi-typescript internal/api/openapi.yaml -o web/src/api/schema.d.ts

# ── WASM offline mode (I-10) ──────────────────────────────────────────────────
# Builds engine.wasm + copies wasm_exec.js into web/offline/ so the directory
# can be served (or opened from file://) without a Go server.
#
# Usage:
#   make wasm                     # build into web/offline/
#   open web/offline/index.html   # or: python3 -m http.server -d web/offline
wasm:
	mkdir -p web/offline
	GOOS=js GOARCH=wasm go build -o web/offline/engine.wasm ./cmd/wasm
	@WASM_JS="$$(go env GOROOT)/misc/wasm/wasm_exec.js"; \
	 [ -f "$$WASM_JS" ] || WASM_JS="$$(go env GOROOT)/lib/wasm/wasm_exec.js"; \
	 cp "$$WASM_JS" web/offline/wasm_exec.js

# ── Docker ────────────────────────────────────────────────────────────────────
IMAGE ?= ghcr.io/khachatur86/goroscope

# Build the Docker image locally (tag: latest + git sha).
docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):latest -t $(IMAGE):$(shell git rev-parse --short HEAD) .

# Push the locally built image to GHCR (requires docker login ghcr.io).
docker-push: docker
	docker push $(IMAGE):latest
	docker push $(IMAGE):$(shell git rev-parse --short HEAD)

# Start the full demo stack (goroscope + sample app).
docker-compose-up:
	docker compose up --build

# Tear down the demo stack.
docker-compose-down:
	docker compose down
