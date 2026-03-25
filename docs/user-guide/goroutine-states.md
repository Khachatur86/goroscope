# Understanding Goroutine States

Every goroutine in a Go program transitions through a small set of runtime states. Goroscope records these transitions from a runtime trace and displays them as coloured segments on the timeline.

## State Reference

| State | Colour | Meaning |
|-------|--------|---------|
| `RUNNING` | Green | The goroutine is scheduled on an OS thread and executing Go code. |
| `RUNNABLE` | Yellow | The goroutine is ready to run but waiting for a free P (processor). |
| `WAITING` | Blue | Blocked on a channel, `sync.WaitGroup`, `sync.Cond`, `select`, or `time.Sleep`. |
| `BLOCKED` | Red | Blocked on a `sync.Mutex`, `sync.RWMutex`, or similar contention primitive. |
| `SYSCALL` | Orange | Inside a system call (I/O, `mmap`, `futex`, etc.). The P may or may not be retained. |
| `DEAD` | Grey | The goroutine has returned (or was not yet started). |

## How to Read the Timeline

Each row is one goroutine. Time flows left to right. Segments are drawn proportionally to their wall-clock duration. Hover over any segment to see:

- State name and duration
- Stack frame at the start of the segment
- Goroutine ID and label (if set via `runtime.SetGoroutineLabels`)

## Common Patterns

### Healthy goroutine pool

Short RUNNING segments interleaved with brief WAITING segments. Worker goroutines waiting for work should be in WAITING, not RUNNABLE — long RUNNABLE periods indicate CPU starvation (too few Ps).

### Lock contention

Many goroutines with long BLOCKED segments that all start at the same time indicate a hot mutex. Use the Contention panel to see which lock has the most waiters and the longest average wait.

### I/O-bound goroutines

Long SYSCALL segments are normal for goroutines doing network I/O. Watch for goroutines that are in SYSCALL for seconds — this can exhaust the thread pool and cause scheduling latency for other goroutines.

### Goroutine leaks

Goroutines that remain in WAITING or BLOCKED long after their expected lifetime are candidates for leaks. Goroscope's leak detector flags goroutines in these states beyond a configurable threshold (default: 30 s).

## Anomaly Score

Goroscope assigns an anomaly score to each goroutine, used to prioritise the sampled view:

| Condition | Score contribution |
|-----------|--------------------|
| State is BLOCKED | +60 |
| State is WAITING | +50 |
| State is SYSCALL | +30 |
| State is RUNNING | +10 |
| Longest wait > 30 s | +40 |
| Longest wait > 1 s | +20 |
| Longest wait > 100 ms | +10 |

When the goroutine count exceeds `--max-goroutines` (default 15 000), only the highest-scoring goroutines are shown and a warning banner appears in the UI.
