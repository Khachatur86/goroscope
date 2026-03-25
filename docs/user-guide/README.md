# Goroscope User Guide

Goroscope is a local Go concurrency debugger that captures and visualises goroutine activity from Go runtime traces.

## Contents

| Guide | What you'll learn |
|-------|-------------------|
| [Goroutine States](./goroutine-states.md) | What each state means and how to spot problems |
| [Interpreting Results](./interpreting-results.md) | Deadlock hints, leak detection, contention analysis, Smart Insights |
| [CI Integration](./ci-integration.md) | Running goroscope in GitHub Actions / GitLab CI |
| [Agent Instrumentation](./agent-guide.md) | Embedding the agent in your application |
| [Comparing Captures](./compare-captures.md) | Regression detection via capture diff |

## Quick Start

### Run a program and inspect it

```bash
goroscope run ./cmd/myapp -- --port=8080
```

Opens `http://localhost:7071` with a live timeline.

### Replay a saved trace

```bash
go test -trace=out.trace ./pkg/worker/...
goroscope replay out.trace
```

### Attach to a running process (Go 1.25+)

```bash
# In your app, import the agent package:
# import _ "github.com/Khachatur86/goroscope/agent"
# Call agent.StartFromEnv() at startup.

goroscope attach http://localhost:7072
```

### Export for Grafana / Jaeger

```bash
goroscope export --format=otlp --endpoint=localhost:4318 capture.gtrace
```

## Key Concepts

**Capture** — a snapshot of goroutine state at a point in time, built from a runtime trace file.

**Session** — a named group of captures from the same target process. Useful for comparing before/after a code change.

**Segment** — a continuous time interval during which a single goroutine was in one state (RUNNING, BLOCKED, WAITING, SYSCALL).

**Anomaly score** — a heuristic score (0–100+) that ranks goroutines by how unusual their behaviour is. Used for sampled views and Smart Insights.
