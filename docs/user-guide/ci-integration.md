# Using Goroscope in CI

## Overview

Goroscope can be integrated into CI pipelines in two ways:

1. **Trace replay** — collect a `go test -trace` file, then replay it with `goroscope check` to fail the build on concurrency issues.
2. **Benchmark regression tracking** — automatically compare benchmark results across commits and post a report when a regression exceeds the threshold.

## Trace Replay in CI

### Step 1: Collect a trace in your test run

```yaml
# .github/workflows/ci.yml
- name: Run tests with trace
  run: go test -race -trace=testdata/out.trace ./...
```

### Step 2: Check the trace for issues

```yaml
- name: Install goroscope
  run: go install github.com/Khachatur86/goroscope/cmd/goroscope@latest

- name: Check trace for concurrency issues
  run: |
    goroscope check \
      --leak-threshold=5s \
      --fail-on=leak,deadlock \
      testdata/out.trace
```

`goroscope check` exits with code 1 if any finding at `critical` severity is found. Use `--fail-on` to control which finding types fail the build.

### Available flags

| Flag | Default | Description |
|------|---------|-------------|
| `--fail-on` | `deadlock,leak` | Comma-separated list of finding types that cause non-zero exit |
| `--leak-threshold` | `30s` | Minimum wait duration to classify a goroutine as leaked |
| `--max-goroutines` | `15000` | Goroutines above this threshold trigger sampled view |
| `--format` | `text` | Output format: `text`, `json`, `github` (annotations) |

### GitHub Actions annotations

Use `--format=github` to emit `::error` and `::warning` annotations directly in the PR diff view:

```yaml
- name: Check trace
  run: |
    goroscope check --format=github testdata/out.trace
```

## Benchmark Regression Tracking

Goroscope ships with `internal/ci/bench_regression.go` — a benchstat-based regression detector that posts a PR comment when any benchmark regresses by more than 10%.

### Setup

The regression tracker is already wired into the provided GitHub Actions workflow at `.github/workflows/ci.yml`. It runs on every push to a PR branch:

```yaml
- name: Benchmark regression check
  run: |
    go test -run='^$' -bench=. -benchmem -count=5 ./... | tee bench-new.txt
    benchstat bench-baseline.txt bench-new.txt > bench-diff.txt
    go run ./internal/ci/bench_regression.go bench-diff.txt
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    PR_NUMBER: ${{ github.event.pull_request.number }}
```

The script posts a comment like:

> **Benchmark regression detected (>10%):**
> ```
> BenchmarkEngine/Load-8   -15.3%   was 1.2ms, now 1.4ms
> ```

### Customising the threshold

Set the `BENCH_REGRESSION_THRESHOLD` environment variable (percentage, default `10`):

```yaml
env:
  BENCH_REGRESSION_THRESHOLD: "5"
```

## GitLab CI

The same workflow is available for GitLab in `.gitlab-ci.yml`:

```yaml
check-trace:
  stage: test
  script:
    - go install github.com/Khachatur86/goroscope/cmd/goroscope@latest
    - go test -trace=out.trace ./...
    - goroscope check --fail-on=deadlock,leak out.trace
  artifacts:
    paths:
      - out.trace
    when: always
```

## Docker

```dockerfile
FROM golang:1.25-alpine AS build
RUN go install github.com/Khachatur86/goroscope/cmd/goroscope@latest

FROM alpine:3.20
COPY --from=build /go/bin/goroscope /usr/local/bin/goroscope
```
