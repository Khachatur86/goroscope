# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2025-03-18

### Added

- **Replay via UI**: "Open capture" button, drag-and-drop `.gtrace`, `POST /api/v1/replay/load`
- **Segment Inspector**: Click on segment shows state/reason/resource; stack at segment via `GET /api/v1/goroutines/:id/stack-at?ns=...`
- **ETag / 304**: Conditional responses for `/goroutines` and `/timeline`
- **govulncheck in CI**: GitHub Actions and GitLab CI run vulnerability checks
- **Stack at segment**: Inspector shows stack snapshot at segment timestamp
- **`goroscope check`**: Analyze capture for deadlock hints; exit 1 if found (for CI)
- **Chrome Trace export**: "Export Trace" button for Chrome DevTools tracing
- **Metrics over time**: `MetricsChart` component (active/blocked goroutines over time)
- **Web build in CI**: GitHub Actions and GitLab CI build React UI
- **`make pre-commit`**: Runs fmt, vet, test-race, lint before commit

### Changed

- Go 1.23 → 1.25
- golangci-lint v2.1.6 → v2.4.0
- Replaced deprecated `runtime.GOROOT()` with `go env GOROOT`
- Vite 5 → 6 (fixes npm audit vulnerabilities)
- README: full API docs, `check` command, layout updates

### Fixed

- Linter: gofmt in engine.go, unused `ctx` in checkCommand

## [Unreleased]

### Added

- **`goroscope test`**: Run `go test` with runtime tracing in a single command — no agent instrumentation required. All `go test` flags and packages are forwarded verbatim. If tests fail, the trace is still loaded for post-mortem inspection. Flags: `-addr`, `-open-browser`, `-ui`, `-ui-path`, `-save`.
- **`goroscope export`**: Export timeline segments to CSV or JSON for analysis (pandas, Perfetto)
- **Compare captures**: `POST /api/v1/compare` — compare two .gtrace files; UI split-panel (baseline vs compare) with diff overlay (improved/regressed/unchanged), unified goroutine rows, sync scroll, filter by status
- **Goroutine groups view**: `GET /api/v1/goroutines/groups?by=function|package|parent_id|label[&label_key=<key>]` — aggregates goroutines by shared dimension with per-group state counts, avg/max/total wait time, and accumulated CPU time. New "Groups" inspector tab in the UI with collapsible rows, group-by switcher, and goroutine ID badges that jump to the inspector.
- **Smart Insights**: `GET /api/v1/smart-insights` — synthesises deadlock, goroutine-leak, contention, long-blocking, and goroutine-count signals into a ranked list of findings (score 0–100, severity critical/warning/info) with human-readable descriptions and actionable recommendations. UI shows a persistent banner below the header with collapsible insight cards, severity badges, and goroutine ID links.
- **Brush time-range selection**: "⌖ Select range" toggle in the timeline legend activates brush mode — drag on the timeline canvas creates a cyan selection rect. Goroutines with no segments in the selected [startNS, endNS] window are removed from the goroutine list. MetricsChart shows a matching highlight rect over the range. "✕ Clear range" resets the filter. Zoom/pan work independently and are preserved.
- **Recursive spawn-tree with branch highlighting**: Inspector "Spawn Tree" section replaced with a fully recursive collapsible tree (`SpawnTree.tsx`). Shows ancestor chain (root → … → parent → selected) and descendant tree (expand/collapse per node, child count badges, state-dot chips). "Highlight branch" button dims all goroutines outside the selected branch in the timeline canvas. Highlight is cleared when another goroutine is selected.
- **Homebrew tap + goreleaser distribution**: `.goreleaser.yaml` (v2) builds multi-platform binaries (linux/darwin/windows × amd64/arm64), bundles `web/dist/` in release archives, and auto-publishes a Homebrew formula to the `Khachatur86/homebrew-goroscope` tap on every tagged release. `go install github.com/Khachatur86/goroscope/cmd/goroscope@latest` continues to work standalone (vanilla UI embedded). Release workflow updated to `goreleaser/goreleaser-action@v6`.
- **Direct trace binary reader** (F-1): `BuildCaptureFromRawTrace` now uses `golang.org/x/exp/trace.NewReader` + `ReadEvent` for direct in-process parsing of Go runtime trace v2 files — no `go tool trace -d=parsed` subprocess. Eliminates the external process round-trip, removes the need for a `go` binary on the path at runtime, and prepares the groundwork for streaming (A-1) via the exported `buildCaptureFromReader(ctx, io.Reader)` function. First external Go dependency (`golang.org/x/exp`); `ParseParsedTrace` preserved for backward compatibility.
- **Streaming live-trace path** (A-1): `watchLiveTrace` (O(n²) full-file re-read every poll tick) replaced by `streamLiveTrace` using `TailReader` + `StreamBinaryTrace`. Events flow directly from the growing binary trace file into the engine via the new `EngineWriter` interface — O(1) per batch, O(n) total. New symbols: `EngineWriter` (interface), `TailReader`, `NewTailReader`, `WaitForTraceFile`, `StreamBinaryTrace`, `StreamBinaryTraceInput` in `internal/tracebridge/stream.go`; `AddProcessorSegments`, `SetParentIDs`, `SetLabelOverrides`, `Flush` added to `*analysis.Engine`; `TracePath()` added to `*tracebridge.LiveTraceRun`.

### Fixed

- **Live demo blocked-filter regression**: After A-1, clicking "blocked" in the React UI showed no goroutines. Root cause: the previous demo completed in ~240 ms (< `pollDelay` of 500 ms), so all events were processed in one shot after the program exited, leaving every goroutine in `StateDone`. Two-part fix: (1) extended `examples/trace_demo` — workers now hold the mutex for 100 ms (was 10 ms) and process 60 jobs (was 24), making the demo run ~6 s with 7/8 workers visibly `BLOCKED` at any moment; (2) added a 200 ms time-based flush ticker to `StreamBinaryTrace` so the UI receives SSE updates even when fewer than 64 events arrive in a poll window.
