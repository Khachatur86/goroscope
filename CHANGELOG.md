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
- **Homebrew tap + goreleaser distribution**: `.goreleaser.yaml` (v2) builds multi-platform binaries (linux/darwin/windows × amd64/arm64), bundles `web/dist/` in release archives, and auto-publishes a Homebrew formula to the `Khachatur86/homebrew-goroscope` tap on every tagged release. `go install github.com/Khachatur86/goroscope/cmd/goroscope@latest` continues to work standalone (vanilla UI embedded). Release workflow updated to `goreleaser/goreleaser-action@v6`.
