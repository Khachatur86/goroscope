# Interpreting Results

## Smart Insights

The Smart Insights panel (below the header) synthesises all analysis results into a ranked list of findings. Each finding has:

- **Severity** — `critical`, `warning`, or `info`
- **Score** — 0–100 ranking (higher = more urgent)
- **Goroutine IDs** — click any badge to jump to that goroutine in the Inspector
- **Recommendation** — actionable next step

Smart Insights run automatically on every capture. You do not need to configure anything.

## Deadlock Hints

### What triggers a deadlock hint?

Goroscope builds a resource graph: goroutines are nodes, and a directed edge `A → B` means "goroutine A holds resource R and goroutine B is waiting for R". A cycle in this graph is a potential deadlock.

The hint appears in Smart Insights with `critical` severity and lists the goroutines involved in the cycle.

### What to do

1. Click the goroutine ID badge to open the Inspector.
2. In the Inspector, look at the **Stack** tab — the top frame shows where the goroutine is blocked.
3. In the **Spawn Tree** tab, follow the chain to find which goroutine holds the lock.
4. Common causes:
   - Lock ordering inconsistency (A locks M1 then M2, B locks M2 then M1).
   - Calling a method that acquires a lock from inside a callback that already holds it.
   - Channel send/receive pairs with no other goroutine on the other end.

### False positives

The graph analysis works on a snapshot. A "cycle" that resolves in under a millisecond will not appear; cycles that persist across the entire capture window are most reliable.

## Leak Detection

### What triggers a leak warning?

A goroutine is flagged as a potential leak when it remains in `WAITING` or `BLOCKED` state for longer than the leak threshold (default: 30 s). The threshold is configurable:

```go
// In your agent startup:
agent.StartFlightRecorder(ctx, agent.FlightRecorderServerConfig{
    // ...
})
```

Or via the CLI:
```bash
goroscope run --leak-threshold=60s ./cmd/myapp
```

### Reading the leak panel

The Leaks section in Smart Insights shows:
- Total leaked goroutine count
- Duration each has been stuck
- Stack frame at the point of blocking

### Common leak patterns

| Pattern | Typical cause |
|---------|---------------|
| Goroutine stuck on `<-ch` forever | Channel never receives a value; sender exited or panicked |
| Goroutine stuck on `sync.WaitGroup.Wait()` | `Done()` called fewer times than `Add()` |
| Goroutine stuck on `select {}` | Intentional park — check if it is an orphaned worker |
| HTTP handler goroutine stuck in SYSCALL | Client disconnected but handler never checks `ctx.Done()` |

## Contention Analysis

The Contention panel lists synchronisation primitives ranked by:

- **Peak waiters** — maximum concurrent goroutines blocked on this primitive
- **Total wait** — sum of all goroutine wait durations
- **Average wait** — total wait / number of acquisitions

### What to do for high-contention locks

1. **Reduce lock scope** — move work outside the critical section.
2. **Shard the lock** — partition data so different goroutines use different locks.
3. **Switch to `sync/atomic`** — for simple counters or flags.
4. **Use `sync.RWMutex`** — if reads dominate writes.
5. **Redesign with channels** — pass ownership of data rather than sharing it.

## Capture Compare

Use `GET /api/v1/compare?a=<id>&b=<id>` or the Compare tab in the UI to diff two captures.

The diff shows:
- Goroutines present in A but not B (potential cleanups)
- Goroutines present in B but not A (potential leaks)
- Goroutines present in both, with state change summary

This is particularly useful for:
- Before/after a code change (did the fix actually remove the blocked goroutines?)
- Same binary under different load levels
- Detecting goroutine accumulation between two snapshots taken minutes apart
