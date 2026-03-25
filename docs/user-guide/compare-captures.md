# Comparing Captures for Regression Detection

Comparing two captures is the primary technique for verifying that a code change improved (or did not worsen) concurrency behaviour.

## When to Compare

- **Before/after a bug fix** — confirm that blocked goroutines from the original report no longer appear.
- **Before/after a performance optimisation** — verify that lock contention decreased.
- **Same code, different load** — understand how the program scales under higher concurrency.
- **Two deploys in production** — detect goroutine accumulation between snapshots taken minutes apart using the Flight Recorder.

## Saving Captures

Captures are automatically saved to `~/.goroscope/captures/` after every `goroscope run` or `goroscope attach` session.

List saved captures:

```bash
goroscope history
```

Output:

```
ID                                    Target                    Goroutines  Captured
3f2e1a0b-...                          ./cmd/myapp               142         2026-03-25 10:14:22
1d4c5e6f-...                          ./cmd/myapp               189         2026-03-25 10:08:01
```

Replay a specific capture:

```bash
goroscope replay ~/.goroscope/captures/3f2e1a0b-....gtrace
```

## Comparing in the UI

1. Open a session in the UI.
2. Click **Compare** in the top navigation.
3. Select a second capture from the dropdown.
4. The diff view shows:
   - **New goroutines** (present in B, not in A) — highlighted green.
   - **Gone goroutines** (present in A, not in B) — highlighted grey.
   - **Changed goroutines** — state change summary (e.g., was RUNNING, now BLOCKED).

## Comparing via the API

```bash
# Get the IDs of two captures from the history endpoint
curl http://localhost:7071/api/v1/history

# Compare them
curl "http://localhost:7071/api/v1/compare?a=<id-a>&b=<id-b>"
```

Response structure:

```json
{
  "added": [{ "id": 42, "state": "WAITING", "topFrame": "net.(*Resolver).lookup" }],
  "removed": [{ "id": 17, "state": "DEAD" }],
  "changed": [
    {
      "id": 8,
      "before": { "state": "BLOCKED", "waitDuration": "1.2s" },
      "after":  { "state": "RUNNING", "waitDuration": "0s" }
    }
  ],
  "summary": {
    "totalBefore": 142,
    "totalAfter": 189,
    "delta": 47,
    "newBlocked": 0,
    "resolvedBlocked": 3
  }
}
```

## Regression Detection in CI

Use the capture diff to build an automated regression gate:

```bash
# Capture before the change (baseline)
go test -trace=baseline.trace ./...
goroscope export --format=json baseline.trace > baseline.json

# Capture after the change (PR branch)
go test -trace=pr.trace ./...
goroscope export --format=json pr.trace > pr.json

# Compare: fail if more goroutines are blocked in the PR branch
goroscope diff --fail-on=new-blocked baseline.json pr.json
```

`goroscope diff` exits with code 1 if `new-blocked` count in the PR capture is greater than in the baseline.

## Interpreting the Summary

| Field | What it means |
|-------|---------------|
| `delta` | Net change in goroutine count. Positive = more goroutines. |
| `newBlocked` | Goroutines that became BLOCKED between the two captures. High values suggest new lock contention. |
| `resolvedBlocked` | Goroutines that were BLOCKED and are now gone or RUNNING. Indicates a fix worked. |
| `added` with state WAITING | Possible goroutine leak — new goroutines stuck waiting. |

## Capture Diff vs Live Compare

| Feature | Capture Diff | Live Compare (SSE) |
|---------|--------------|--------------------|
| Works offline | Yes | No |
| Shows historical state | Yes | No (only current) |
| Compares arbitrary points in time | Yes | Only "now" vs "a second ago" |
| Available in CLI | Yes | No |
| Available in UI | Yes (Compare tab) | Yes (live view header) |
