# goroscope

Goroscope is a local Go concurrency debugger that captures runtime trace events and visualizes goroutines, blocking, channels, and mutex interactions on an interactive timeline.

## Install

```bash
go install github.com/Khachatur86/goroscope/cmd/goroscope@latest
```

Or download a pre-built binary from [Releases](https://github.com/Khachatur86/goroscope/releases) (includes React UI).

Or build from source:

```bash
git clone https://github.com/Khachatur86/goroscope
cd goroscope
make build
# Binary: bin/goroscope
```

Build with version: `make build VERSION=1.0.0`

## Quick Start

```bash
goroscope ui --open-browser
```

Or without the flag: open `http://127.0.0.1:7070` manually.

**React UI** (same port 7070):

```bash
make web
goroscope ui -ui=react -open-browser
```

Or `make ui-react` (builds + web + runs).

## Current Status

This repository contains a working local MVP built around `runtime/trace`:

- Go CLI with `run`, `collect`, `ui`, `replay`, `check`, and `version` commands
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
| `run`     | Run a Go program with live trace capture         |
| `collect` | Load demo data and serve UI                      |
| `ui`      | Load demo data and serve UI                      |
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

**Without agent** — Use `go test -trace=out ./pkg` to produce a raw trace, then `goroscope replay out` or `goroscope export --format=csv out` to inspect it.

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
| `GET /api/v1/stream` | Server-Sent Events for live updates |

Open the UI with `?goroutine=123` to auto-select that goroutine. The URL updates when you select a different one (shareable links).

**Compare captures**: Click "Compare" in the header, select two .gtrace files (baseline and compare), then view the split-panel diff (improved / regressed / unchanged).

## Development

Before committing, run:

```bash
make pre-commit
```

This runs `go fmt`, `go vet`, `go test -race`, and `golangci-lint`. Use `make lint-fix` to auto-fix what lint can fix.

## Layout

```text
agent/                  Opt-in trace bootstrap for target programs
cmd/goroscope           CLI entrypoint
examples/trace_demo     Example: channels + mutex
examples/worker_pool    Example: worker pool pattern
examples/http_demo      Example: agent.WithRequestID for HTTP handlers
internal/api            Local REST API, SSE stream, and embedded UI assets
internal/analysis       Goroutine state engine and timeline construction
internal/model          Core domain types
internal/session        Session lifecycle
internal/tracebridge    Runtime trace execution, parsing, and replay
vscode/                 VS Code extension
web/                    React frontend (Vite + TypeScript)
docs/                   Product and architecture docs
```
