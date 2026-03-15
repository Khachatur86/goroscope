# Non-Functional Requirements — Verification

This document tracks verification of NFRs from [MVP_SPEC.md](MVP_SPEC.md) §12.

---

## Performance

| Requirement | Target | Verification |
|-------------|--------|---------------|
| Collection overhead | <10% CPU on representative workloads | Manual profiling; `go tool pprof` on live run |
| Timeline update latency | <500ms from event ingestion to UI update | SSE push + poll interval; measured in `watchLiveTrace` |
| Browser interactivity | 10k visible goroutines with virtualization | `BenchmarkEngineLoadCapture` and `BenchmarkEngineListGoroutines` for 10k goroutines |

### Benchmarks

Run:

```bash
go test -bench=. -benchmem ./internal/tracebridge/... ./internal/analysis/...
```

Track regressions in CI (optional): add `go test -bench=... -count=5` and compare with `benchstat`.

---

## Memory

| Requirement | Target | Verification |
|-------------|--------|---------------|
| Default in-memory budget | 256MB | No explicit cap in MVP; stacks deduplicated in capture |
| Configurable upper bound | 1GB | Deferred to post-MVP |
| Stack deduplication | Aggressive | Stacks stored per goroutine; no cross-goroutine dedup in MVP |

---

## Reliability

| Requirement | Verification |
|-------------|---------------|
| Collector must not crash on malformed event | `TestParseParsedTrace_MalformedLines` — parser skips invalid transitions |
| Session completion on abrupt target exit | `watchLiveTrace` handles `LiveRun.Done()`, calls `FailCurrent` on error |
| UI tolerates stream reconnects | SSE client reconnects; no server-side state for connection |

### Tests

- `internal/tracebridge/parsedtrace_test.go`: malformed lines, missing sync, empty output
- `internal/tracebridge/replay_test.go`: malformed JSON, empty events
- `internal/analysis/engine_test.go`: invalid events (GoroutineID=0), stack for unknown goroutine

---

## Security

| Requirement | Verification |
|-------------|---------------|
| Bind to localhost by default | `--addr=127.0.0.1:7070` |
| No remote access in MVP | No TLS, no auth |
| No telemetry in MVP | No outbound calls |

---

## Phase 7 Checklist

- [x] Parser skips malformed transition lines (no crash)
- [x] `decodeCapture` validates nil/empty events
- [x] Engine ignores GoroutineID=0 events
- [x] Engine handles stack snapshot for goroutine with no prior events
- [x] Benchmarks for ParseParsedTrace, LoadCapture, ListGoroutines
- [x] NFR documentation
