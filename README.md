# goroscope

Goroscope is a local Go concurrency debugger that captures runtime trace events and visualizes goroutines, blocking, channels, and mutex interactions on an interactive timeline.

## Current Status

This repository contains a working local MVP built around `runtime/trace`:

- Go CLI with `run`, `collect`, `ui`, and `replay` commands
- cooperative trace capture via the `agent` package
- trace parsing and normalization in `internal/tracebridge`
- in-memory analysis engine and session manager
- local REST + SSE API with an embedded browser UI under `internal/api/ui`
- future React workspace scaffold under `web/`
- product specification and architecture notes under `docs/`

## Quick Start

```bash
make build
go run ./cmd/goroscope ui --open-browser
```

Or without the flag: open `http://127.0.0.1:7070` manually.

Build with version: `make build VERSION=1.0.0`

## Runtime Trace Demo

The first real `run` pipeline is cooperative: the target app must import the Goroscope agent and call `agent.StartFromEnv()` in `main`.

An example target is included:

```bash
go run ./cmd/goroscope run ./examples/trace_demo
```

This starts the local UI immediately, runs the target, and refreshes the timeline from the growing `runtime/trace` while the process is still running. Live updates are pushed to the browser over Server-Sent Events, with a periodic fallback refresh in the UI.

## Other Entry Points

```bash
go run ./cmd/goroscope ui
go run ./cmd/goroscope collect
go run ./cmd/goroscope replay ./captures/sample.gtrace
```

`ui` and `collect` currently load bundled demo data. `replay` loads a capture file from disk. The current runnable UI is the embedded asset bundle in `internal/api/ui`; the `web/` directory is a future standalone frontend workspace.

## API

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/goroutines` | List goroutines. Query: `state`, `reason`, `search`, `min_wait_ns`, `limit`, `offset` |
| `GET /api/v1/goroutines/{id}` | Goroutine details |
| `GET /api/v1/goroutines/{id}/children` | Child goroutines (spawned by this one) |
| `GET /api/v1/insights` | Long-blocked goroutines. Query: `min_wait_ns` (default 1s) |
| `GET /api/v1/timeline` | Timeline segments. Query: `state`, `reason`, `search` |

Open the UI with `?goroutine=123` to auto-select that goroutine. The URL updates when you select a different one (shareable links).

## Layout

```text
agent/               Opt-in trace bootstrap for target programs
cmd/goroscope        CLI entrypoint
examples/trace_demo  Example target instrumented with the agent
internal/api         Local REST API, SSE stream, and embedded UI assets
internal/analysis    Goroutine state engine and timeline construction
internal/model       Core domain types
internal/session     Session lifecycle
internal/tracebridge Runtime trace execution, parsing, and replay
web/                 Future React frontend scaffold
docs/                Product and architecture docs
```
