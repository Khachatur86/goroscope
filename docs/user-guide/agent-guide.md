# Agent Instrumentation Guide

The `agent` package embeds a lightweight HTTP server in your application that Goroscope's CLI and poller connect to. It requires no external process — just import the package and call one function at startup.

## Installation

```bash
go get github.com/Khachatur86/goroscope/agent
```

The agent package has **zero external dependencies** beyond the Go standard library.

## Basic Setup

```go
import "github.com/Khachatur86/goroscope/agent"

func main() {
    // Start the agent using GOROSCOPE_ADDR env var (default: 127.0.0.1:7072)
    stop, err := agent.StartFromEnv()
    if err != nil {
        log.Printf("goroscope agent: %v", err) // non-fatal, continue without agent
    } else {
        defer stop()
    }

    // ... your application startup ...
}
```

Set the listen address via environment variable:

```bash
GOROSCOPE_ADDR=127.0.0.1:7072 ./myapp
```

Then attach:

```bash
goroscope attach http://127.0.0.1:7072
```

## Request Correlation

Tag goroutines with a request ID so they are grouped correctly in the timeline:

```go
import "github.com/Khachatur86/goroscope/agent"

func handleRequest(w http.ResponseWriter, r *http.Request) {
    ctx := agent.WithRequestID(r.Context(), r.Header.Get("X-Request-ID"))
    // All goroutines spawned from this ctx inherit the label.
    doWork(ctx)
}
```

The label appears in the **Groups** view under `label_key=request_id` and as a tooltip in the timeline.

## Flight Recorder (Go 1.25+)

For always-on, low-overhead continuous tracing in production use the Flight Recorder integration:

```go
import (
    "context"
    "time"
    "github.com/Khachatur86/goroscope/agent"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    stop, err := agent.StartFlightRecorder(ctx, agent.FlightRecorderServerConfig{
        Addr:     "127.0.0.1:7072", // HTTP server address
        MinAge:   2 * time.Second,  // minimum snapshot age
        MaxBytes: 10 << 20,         // 10 MB ring buffer

        // Optional: auto-save a snapshot file when goroutine count spikes
        AnomalyThreshold: 500,                  // goroutine count trigger
        AnomalyDir:       "/var/log/myapp/traces",
    })
    if err != nil {
        log.Printf("flight recorder: %v", err)
    } else {
        defer stop()
    }

    // ... application startup ...
}
```

### Flight Recorder endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /debug/goroscope/snapshot` | Binary runtime trace snapshot (current ring buffer) |
| `GET /debug/goroscope/status` | JSON status: goroutine count, recorder enabled, snapshot age |

### Polling from the CLI

```bash
goroscope attach --flight-recorder http://127.0.0.1:7072
```

This polls the snapshot endpoint every 2 seconds (configurable with `--interval`) and feeds the data into the live timeline.

## Security Considerations

The agent HTTP server should only listen on loopback (`127.0.0.1`) in production unless you have explicit network controls. For remote access:

1. Use a reverse proxy with TLS termination.
2. Pass a bearer token via the `GOROSCOPE_TOKEN` environment variable; the agent will require `Authorization: Bearer <token>` on all requests.

```bash
GOROSCOPE_ADDR=0.0.0.0:7072 GOROSCOPE_TOKEN=mysecret ./myapp
```

```bash
goroscope attach --token=mysecret http://10.0.1.5:7072
```

## Goroutine Labels

Runtime labels set via `runtime/pprof.Do` or `runtime.SetGoroutineLabels` are captured by the agent and displayed in the Inspector. Use them to annotate critical goroutines:

```go
import "runtime/pprof"

func processJob(ctx context.Context, jobID string) {
    labels := pprof.Labels("job_id", jobID, "component", "processor")
    pprof.Do(ctx, labels, func(ctx context.Context) {
        // goroutine activity here is annotated with job_id + component
        doHeavyWork(ctx)
    })
}
```

In the Groups view, switch **Group by** to `label` and set **Label key** to `job_id` to see per-job goroutine aggregates.

## Minimal Overhead

The agent is designed to be safe in production:

- The pprof snapshot endpoint is read-only and does not pause the program.
- The Flight Recorder uses a kernel ring buffer; `WriteTo` copies the buffer without stopping goroutines.
- Both endpoints have a 30-second read timeout and a 256-byte error body limit to prevent abuse.
