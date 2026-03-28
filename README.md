# Goroscope

[![CI](https://github.com/Khachatur86/goroscope/actions/workflows/ci.yml/badge.svg)](https://github.com/Khachatur86/goroscope/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Khachatur86/goroscope)](https://goreportcard.com/report/github.com/Khachatur86/goroscope)
[![Go Reference](https://pkg.go.dev/badge/github.com/Khachatur86/goroscope.svg)](https://pkg.go.dev/github.com/Khachatur86/goroscope)
[![VS Code Marketplace](https://img.shields.io/visual-studio-marketplace/v/goroscope.goroscope?label=VS%20Code)](https://marketplace.visualstudio.com/items?itemName=goroscope.goroscope)
[![Open VSX](https://img.shields.io/open-vsx/v/goroscope/goroscope?label=Open%20VSX)](https://open-vsx.org/extension/goroscope/goroscope)

**Goroscope** is a local Go concurrency debugger. It visualizes goroutines, blocking, channels, and mutex interactions on an interactive timeline — with live updates while your process runs and zero data leaving your machine.

## 2-minute quickstart

### Attach to any running Go process (zero code changes)

Most Go services already expose `/debug/pprof`. If yours does not, add one import:

```go
import _ "net/http/pprof"
```

Then attach:

```bash
go install github.com/Khachatur86/goroscope/cmd/goroscope@latest
goroscope attach -addr http://localhost:6060 -open-browser
```

Goroscope polls `/debug/pprof/goroutine?debug=2` every 2 s, accumulates the full goroutine history, and serves the UI at **http://localhost:7070**.

### Instrument with the agent library

```go
import "github.com/Khachatur86/goroscope/agent"

func main() {
    stop := agent.Start(agent.Config{Addr: ":7070", OpenBrowser: true})
    defer stop()
    // … rest of main
}
```

```bash
go run ./yourprogram   # UI opens automatically
```

## Install

**go install** (single self-contained binary with React UI baked in):

```bash
go install github.com/Khachatur86/goroscope/cmd/goroscope@latest
```

**Build from source:**

```bash
git clone https://github.com/Khachatur86/goroscope
cd goroscope
make build-dist     # builds React UI, embeds it, compiles Go → bin/goroscope
# Or for development:
make build          # Go only (shows vanilla UI until make embed-web is run)
```

Build with version: `make build-dist VERSION=1.2.0`

## Current Status

This repository contains a working local MVP built around `runtime/trace`:

- Go CLI with `run`, `test`, `collect`, `ui`, `replay`, `check`, `export`, and `version` commands
- cooperative trace capture via the `agent` package
- trace parsing and normalization in `internal/tracebridge`
- in-memory analysis engine and session manager
- local REST + SSE API with an embedded browser UI under `internal/api/ui`
- VS Code extension with Session panel and open-in-editor from stack frames
- React UI in `web/` (Vite + TypeScript) — run `make web` to build
- product specification and architecture notes under `docs/`

## Runtime Trace Demo

The `run` pipeline is cooperative: the target app must import the Goroscope agent and call `agent.StartFromEnv()` in `main`.

Examples:

```bash
goroscope run ./examples/trace_demo --open-browser
goroscope run ./examples/worker_pool --open-browser
```

**React UI** for live run (flags must come before the target):

```bash
make web
goroscope run -ui=react -open-browser ./examples/trace_demo
```

Or `make run-react`.

This starts the local UI immediately, runs the target, and refreshes the timeline from the growing `runtime/trace` while the process is still running. Live updates are pushed to the browser over Server-Sent Events.

## Commands

| Command   | Description                                      |
|-----------|--------------------------------------------------|
| `attach`  | Attach to any live Go process via `/debug/pprof` |
| `run`     | Run a Go program with live trace capture         |
| `test`    | Run `go test` with tracing, open UI with result  |
| `collect` | Load demo data and serve UI                      |
| `ui`      | Open the UI (no trace loaded)                    |
| `replay`  | Load .gtrace or raw Go trace (e.g. go test -trace) and serve UI |
| `check`   | Analyze capture for deadlock hints; exit 1 if found (for CI) |
| `export`  | Export timeline segments to CSV or JSON (for pandas, analysis) |
| `version` | Print version                                    |
| `help`    | Show usage                                       |

```bash
goroscope help
goroscope run -h
goroscope export --format=csv capture.gtrace   # CSV for pandas
goroscope export --format=json capture.gtrace  # JSON with segments
```

## Troubleshooting

**"target did not emit a runtime trace"** — The target must import `github.com/Khachatur86/goroscope/agent` and call `agent.StartFromEnv()` in `main`. See `examples/trace_demo` and `examples/worker_pool`.

**Without agent** — Use `goroscope test ./pkg/...` to produce and visualize a trace in one command. Or manually: `go test -trace=out ./pkg` then `goroscope replay out`.

## Test Command

`goroscope test` runs `go test` with runtime tracing injected, then opens the UI with the resulting trace — no agent instrumentation required.

```bash
# Trace a single package
goroscope test ./pkg/worker -open-browser

# Filter to one test and save the capture
goroscope test ./pkg/worker -run TestWorkerPool -save=debug.gtrace -open-browser

# Trace all packages (may produce a large trace)
goroscope test ./... -count=1

# Use the React UI
make web
goroscope test ./pkg/worker -ui=react -open-browser
```

All arguments after goroscope's own flags (`-addr`, `-open-browser`, `-ui`, `-ui-path`, `-save`) are forwarded verbatim to `go test`. If tests fail, goroscope still loads and serves the trace so you can inspect goroutine state at the time of failure.

**"Cannot connect to Goroscope"** (VS Code) — Ensure goroscope is running (`goroscope run ...` or `goroscope ui`). Check `goroscope.addr` in VS Code settings.

## API

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/goroutines` | List goroutines. Query: `state`, `reason`, `search`, `min_wait_ns`, `limit`, `offset` |
| `GET /api/v1/goroutines/{id}` | Goroutine details |
| `GET /api/v1/goroutines/{id}/children` | Child goroutines (spawned by this one) |
| `GET /api/v1/goroutines/{id}/stack-at?ns=...` | Stack snapshot at given nanosecond (for segment inspection) |
| `GET /api/v1/insights` | Long-blocked goroutines. Query: `min_wait_ns` (default 1s) |
| `GET /api/v1/timeline` | Timeline segments. Query: `state`, `reason`, `search` |
| `GET /api/v1/resources/graph` | Resource dependency graph |
| `GET /api/v1/deadlock-hints` | Deadlock analysis hints |
| `GET /api/v1/processor-timeline` | GMP processor timeline (for scheduler view) |
| `POST /api/v1/replay/load` | Load .gtrace file (multipart form field `file`) |
| `POST /api/v1/compare` | Compare two .gtrace files (multipart `file_a`, `file_b`); returns baseline, compare, and diff |
| `GET /api/v1/goroutines/groups` | Goroutine groups. Query: `by` (function\|package\|parent_id\|label), `label_key` |
| `GET /api/v1/smart-insights` | Ranked actionable findings (deadlock, leak, contention, blocking, count) |
| `GET /api/v1/stream` | Server-Sent Events for live updates |

Open the UI with `?goroutine=123` to auto-select that goroutine. The URL updates when you select a different one (shareable links).

**Compare captures**: Click "Compare" in the header, select two .gtrace files (baseline and compare), then view the split-panel diff (improved / regressed / unchanged).

## Attach flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `http://localhost:6060` | Target pprof base URL |
| `-interval` | `2s` | Poll interval |
| `-session-name` | `attach` | Session label shown in the UI |
| `-open-browser` | `false` | Open browser automatically |

## Development

```bash
# Backend only (vanilla embedded UI)
make build && make run

# Full release build (React UI baked in)
make build-dist

# React dev server (hot-reload on :5173, proxied to :7070)
cd web && npm install && npm run dev

# React UI dev mode via goroscope
make run-react          # builds React + starts goroscope with -ui-path=web/dist

# Tests and lint
make test-race          # go test -race ./...
make lint               # golangci-lint
make pre-commit         # fmt + vet + test-race + lint

# Install git hooks (gofmt + go vet + golangci-lint on every commit)
git config core.hooksPath .githooks
```

Use `make lint-fix` to auto-fix what lint can fix.

## Layout

```text
agent/                  Opt-in trace bootstrap for target programs
cmd/goroscope           CLI entrypoint
examples/trace_demo     Example: channels + mutex
examples/worker_pool    Example: worker pool pattern
examples/http_demo      Example: agent.WithRequestID for HTTP handlers
internal/api            Local REST API, SSE stream, and embedded UI assets
internal/api/reactui/   Embedded React bundle (generated by make embed-web)
internal/api/ui/        Vanilla embedded UI (fallback when React not built)
internal/analysis       Goroutine state engine and timeline construction
internal/model          Core domain types
internal/pprofpoll      pprof attach mode poller and parser
internal/session        Session lifecycle
internal/tracebridge    Runtime trace execution, parsing, and replay
vscode/                 VS Code extension
web/                    React frontend (Vite + TypeScript)
docs/                   Product and architecture docs
```
