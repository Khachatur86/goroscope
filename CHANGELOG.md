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

- (none yet)
