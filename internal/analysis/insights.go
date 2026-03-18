package analysis

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// InsightSeverity ranks how urgent a finding is.
type InsightSeverity string

const (
	// SeverityCritical requires immediate attention (deadlock, large leak).
	SeverityCritical InsightSeverity = "critical"
	// SeverityWarning indicates a problem that degrades performance or correctness.
	SeverityWarning InsightSeverity = "warning"
	// SeverityInfo highlights a pattern worth monitoring.
	SeverityInfo InsightSeverity = "info"
)

// InsightKind identifies the class of finding.
type InsightKind string

const (
	InsightKindDeadlock   InsightKind = "deadlock"
	InsightKindLeak       InsightKind = "leak"
	InsightKindContention InsightKind = "contention"
	InsightKindBlocking   InsightKind = "blocking"
	InsightKindGoroutines InsightKind = "goroutine_count"
)

// Insight is a single ranked, actionable finding produced by GenerateInsights.
type Insight struct {
	// ID is a stable, deterministic identifier within a single snapshot.
	ID string `json:"id"`
	// Kind classifies the finding.
	Kind InsightKind `json:"kind"`
	// Severity indicates urgency.
	Severity InsightSeverity `json:"severity"`
	// Score is 0–100; higher means more urgent. Used for ordering.
	Score float64 `json:"score"`
	// Title is a short (≤80 chars) human-readable summary.
	Title string `json:"title"`
	// Description provides context and relevant metrics.
	Description string `json:"description"`
	// Recommendation is the primary actionable suggestion.
	Recommendation string `json:"recommendation"`
	// GoroutineIDs lists goroutines involved in the finding (may be empty).
	GoroutineIDs []int64 `json:"goroutine_ids,omitempty"`
	// ResourceIDs lists resources involved (may be empty).
	ResourceIDs []string `json:"resource_ids,omitempty"`
}

// defaultLeakThresholdNS is 30 seconds.
const defaultLeakThresholdNS = int64(30_000_000_000)

// defaultLongBlockThresholdNS is 1 second.
const defaultLongBlockThresholdNS = int64(1_000_000_000)

// defaultContentionPeakWaiters triggers a contention insight.
const defaultContentionPeakWaiters = 4

// defaultGoroutineCountThreshold triggers a goroutine-count insight.
const defaultGoroutineCountThreshold = 1000

// GenerateInsightsInput holds all data required to produce insights.
// ctx is not in the struct per CTX-1; callers should not need cancellation here.
type GenerateInsightsInput struct {
	Goroutines []model.Goroutine
	Segments   []model.TimelineSegment
	Edges      []model.ResourceEdge

	// Optional thresholds — zero means use the defaults above.
	LeakThresholdNS      int64
	LongBlockThresholdNS int64
	ContentionMinPeak    int
	GoroutineCountMin    int
}

// GenerateInsights synthesises all existing analysis primitives into a ranked
// list of findings, sorted by Score descending (most urgent first).
func GenerateInsights(in GenerateInsightsInput) []Insight {
	leakNS := in.LeakThresholdNS
	if leakNS <= 0 {
		leakNS = defaultLeakThresholdNS
	}
	blockNS := in.LongBlockThresholdNS
	if blockNS <= 0 {
		blockNS = defaultLongBlockThresholdNS
	}
	contentionMin := in.ContentionMinPeak
	if contentionMin <= 0 {
		contentionMin = defaultContentionPeakWaiters
	}
	goroutineMin := in.GoroutineCountMin
	if goroutineMin <= 0 {
		goroutineMin = defaultGoroutineCountThreshold
	}

	var insights []Insight

	insights = append(insights, deadlockInsights(in.Edges, in.Goroutines)...)
	insights = append(insights, leakInsights(in.Goroutines, leakNS)...)
	insights = append(insights, contentionInsights(in.Segments, contentionMin)...)
	insights = append(insights, blockingInsights(in.Goroutines, blockNS, leakNS)...)
	insights = append(insights, goroutineCountInsight(in.Goroutines, goroutineMin)...)

	sort.Slice(insights, func(i, j int) bool {
		if insights[i].Score != insights[j].Score {
			return insights[i].Score > insights[j].Score
		}
		return insights[i].ID < insights[j].ID
	})

	return insights
}

// ── Deadlock ─────────────────────────────────────────────────────────────────

func deadlockInsights(edges []model.ResourceEdge, goroutines []model.Goroutine) []Insight {
	hints := FindDeadlockHints(edges, goroutines)
	out := make([]Insight, 0, len(hints))
	for i, h := range hints {
		ids := formatGoroutineList(h.GoroutineIDs, 5)
		desc := fmt.Sprintf(
			"%d goroutines form a blocked cycle (%s). Resources involved: %s.",
			len(h.GoroutineIDs),
			ids,
			formatResourceList(h.ResourceIDs, 3),
		)
		if h.BlameChain != "" {
			desc += " Chain: " + h.BlameChain + "."
		}
		out = append(out, Insight{
			ID:             fmt.Sprintf("deadlock-%d", i),
			Kind:           InsightKindDeadlock,
			Severity:       SeverityCritical,
			Score:          100,
			Title:          fmt.Sprintf("Potential deadlock: %d goroutines in blocked cycle", len(h.GoroutineIDs)),
			Description:    desc,
			Recommendation: "Check resource acquisition order. Ensure locks are always acquired in the same order. Use `go vet -race` and the deadlock detector.",
			GoroutineIDs:   h.GoroutineIDs,
			ResourceIDs:    h.ResourceIDs,
		})
	}
	return out
}

// ── Goroutine leak ────────────────────────────────────────────────────────────

func leakInsights(goroutines []model.Goroutine, thresholdNS int64) []Insight {
	leaks := LeakCandidates(goroutines, thresholdNS)
	if len(leaks) == 0 {
		return nil
	}

	// Group leaked goroutines by top user-space function for actionable output.
	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: leaks,
		By:         GroupByFunction,
	})
	if err != nil || len(groups) == 0 {
		// Fallback: single aggregate insight.
		ids := extractIDs(leaks)
		return []Insight{{
			ID:             "leak-0",
			Kind:           InsightKindLeak,
			Severity:       SeverityCritical,
			Score:          90,
			Title:          fmt.Sprintf("Goroutine leak: %d goroutines stuck >%s", len(leaks), fmtDuration(thresholdNS)),
			Description:    fmt.Sprintf("%d goroutines have been in WAITING or BLOCKED state for more than %s without a state change.", len(leaks), fmtDuration(thresholdNS)),
			Recommendation: "Check goroutine lifecycle and context propagation. Ensure all goroutines respect context cancellation.",
			GoroutineIDs:   ids,
		}}
	}

	var out []Insight
	for i, g := range groups {
		if i >= 5 {
			break // Limit to top 5 groups to avoid noise
		}
		score := 90.0 - float64(i)*3
		out = append(out, Insight{
			ID:             fmt.Sprintf("leak-%d", i),
			Kind:           InsightKindLeak,
			Severity:       SeverityCritical,
			Score:          score,
			Title:          fmt.Sprintf("Goroutine leak: %d goroutines stuck in %s (>%s)", g.Count, g.Key, fmtDuration(thresholdNS)),
			Description:    fmt.Sprintf("%d goroutines in %s have been WAITING/BLOCKED for more than %s. Max individual wait: %s.", g.Count, g.Key, fmtDuration(thresholdNS), fmtDuration(g.MaxWaitNS)),
			Recommendation: fmt.Sprintf("Inspect %s: ensure it respects ctx.Done(), drains channels, and doesn't block on unresponsive I/O.", g.Key),
			GoroutineIDs:   g.GoroutineIDs,
		})
	}
	return out
}

// ── Resource contention ───────────────────────────────────────────────────────

func contentionInsights(segments []model.TimelineSegment, minPeakWaiters int) []Insight {
	contention := ComputeResourceContention(segments)

	// Sort by peak waiters descending.
	sort.Slice(contention, func(i, j int) bool {
		return contention[i].PeakWaiters > contention[j].PeakWaiters
	})

	var out []Insight
	for i, c := range contention {
		if c.PeakWaiters < minPeakWaiters {
			break // sorted, so all remaining are below threshold
		}
		if i >= 5 {
			break
		}
		score := 75.0 - float64(i)*5
		out = append(out, Insight{
			ID:             fmt.Sprintf("contention-%d", i),
			Kind:           InsightKindContention,
			Severity:       SeverityWarning,
			Score:          score,
			Title:          fmt.Sprintf("High contention on %s (%d concurrent waiters)", c.ResourceID, c.PeakWaiters),
			Description:    fmt.Sprintf("Resource %s has a peak of %d concurrent waiters. Average wait: %s, total wait: %s across %d segments.", c.ResourceID, c.PeakWaiters, fmtDuration(int64(c.AvgWaitNS)), fmtDuration(c.TotalWaitNS), c.SegmentCount),
			Recommendation: "Reduce lock scope, use sync.RWMutex for read-heavy workloads, or shard the resource to reduce contention.",
			ResourceIDs:    []string{c.ResourceID},
		})
	}
	return out
}

// ── Long blocking ─────────────────────────────────────────────────────────────

func blockingInsights(goroutines []model.Goroutine, blockNS, leakNS int64) []Insight {
	var blocked []model.Goroutine
	for _, g := range goroutines {
		// Exclude leaks (they have their own insight).
		if g.WaitNS >= leakNS {
			continue
		}
		if isWaitStateForInsights(g.State) && g.WaitNS >= blockNS {
			blocked = append(blocked, g)
		}
	}
	if len(blocked) == 0 {
		return nil
	}

	// Compute aggregate stats.
	var maxWait int64
	for _, g := range blocked {
		if g.WaitNS > maxWait {
			maxWait = g.WaitNS
		}
	}
	score := 60.0
	if len(blocked) > 50 {
		score = 70
	}
	ids := extractIDs(blocked)
	if len(ids) > 20 {
		ids = ids[:20]
	}
	return []Insight{{
		ID:             "blocking-0",
		Kind:           InsightKindBlocking,
		Severity:       SeverityWarning,
		Score:          score,
		Title:          fmt.Sprintf("%d goroutines blocked for >%s", len(blocked), fmtDuration(blockNS)),
		Description:    fmt.Sprintf("%d goroutines are in WAITING, BLOCKED, or SYSCALL state for more than %s. Longest individual wait: %s.", len(blocked), fmtDuration(blockNS), fmtDuration(maxWait)),
		Recommendation: "Investigate slow dependencies (DB, network, I/O). Ensure context cancellation is propagated. Consider timeouts.",
		GoroutineIDs:   ids,
	}}
}

// ── Goroutine count ───────────────────────────────────────────────────────────

func goroutineCountInsight(goroutines []model.Goroutine, minCount int) []Insight {
	n := len(goroutines)
	if n < minCount {
		return nil
	}
	var blocked int
	for _, g := range goroutines {
		if isWaitStateForInsights(g.State) {
			blocked++
		}
	}
	score := 30.0
	if n > 5000 {
		score = 40
	}
	return []Insight{{
		ID:             "goroutine-count-0",
		Kind:           InsightKindGoroutines,
		Severity:       SeverityInfo,
		Score:          score,
		Title:          fmt.Sprintf("High goroutine count: %d active goroutines", n),
		Description:    fmt.Sprintf("%d goroutines are active; %d (%d%%) are in a blocked/waiting state.", n, blocked, pct(blocked, n)),
		Recommendation: "Consider bounding concurrency with worker pools, semaphores (golang.org/x/sync/semaphore), or errgroup with limits.",
	}}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func isWaitStateForInsights(s model.GoroutineState) bool {
	switch s {
	case model.StateBlocked, model.StateWaiting, model.StateSyscall:
		return true
	default:
		return false
	}
}

// fmtDuration converts nanoseconds to a human-readable string.
func fmtDuration(ns int64) string {
	if ns <= 0 {
		return "0ns"
	}
	if ns < 1_000 {
		return fmt.Sprintf("%dns", ns)
	}
	if ns < 1_000_000 {
		return fmt.Sprintf("%.1fµs", float64(ns)/1_000)
	}
	if ns < 1_000_000_000 {
		return fmt.Sprintf("%.1fms", float64(ns)/1_000_000)
	}
	return fmt.Sprintf("%.2fs", float64(ns)/1_000_000_000)
}

func pct(part, total int) int {
	if total == 0 {
		return 0
	}
	return (part * 100) / total
}

func extractIDs(goroutines []model.Goroutine) []int64 {
	ids := make([]int64, len(goroutines))
	for i, g := range goroutines {
		ids[i] = g.ID
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func formatGoroutineList(ids []int64, max int) string {
	var parts []string
	for i, id := range ids {
		if i >= max {
			parts = append(parts, fmt.Sprintf("…+%d more", len(ids)-max))
			break
		}
		parts = append(parts, fmt.Sprintf("G%d", id))
	}
	return strings.Join(parts, ", ")
}

func formatResourceList(ids []string, max int) string {
	if len(ids) == 0 {
		return "(unknown)"
	}
	var parts []string
	for i, id := range ids {
		if i >= max {
			parts = append(parts, fmt.Sprintf("+%d more", len(ids)-max))
			break
		}
		parts = append(parts, id)
	}
	return strings.Join(parts, ", ")
}
