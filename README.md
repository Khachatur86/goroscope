# goroscope

Goroscope is a local Go concurrency debugger that captures runtime trace events and visualizes goroutines, blocking, channels, and mutex interactions on an interactive timeline.

## Current Status

This repository now contains a buildable starter scaffold for the MVP:

- Go module and CLI entrypoint
- internal packages for model, collector, analysis, API, session, and trace bridge
- local HTTP API, demo session data, and an interactive browser UI
- future frontend workspace scaffold under `web/`
- product specification under `docs/`

## Quick Start

```bash
make build
go run ./cmd/goroscope ui
```

Then open `http://127.0.0.1:7070`.

## Runtime Trace Demo

The first real `run` pipeline is cooperative: the target app must import the Goroscope agent and call `agent.StartFromEnv()` in `main`.

An example target is included:

```bash
go run ./cmd/goroscope run ./examples/trace_demo
```

This starts the local UI immediately, runs the target, and refreshes the timeline from the growing `runtime/trace` while the process is still running. The browser UI currently polls the backend once per second.

## Layout

```text
cmd/goroscope        CLI entrypoint
internal/api         Local HTTP API
internal/analysis    Timeline/state scaffolding
internal/collector   Event buffering
internal/model       Core domain types
internal/session     Session lifecycle
internal/tracebridge Trace ingestion stubs
web/                 Frontend scaffold
docs/                Product and architecture docs
```
