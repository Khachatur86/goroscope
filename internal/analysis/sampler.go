package analysis

import (
	"fmt"
	"sort"

	"github.com/Khachatur86/goroscope/internal/model"
)

// SamplingPolicy controls display-level goroutine sampling when the total
// count exceeds a budget. This is a presentation concern, distinct from
// RetentionPolicy which bounds raw in-memory data volume.
type SamplingPolicy struct {
	// MaxDisplay caps the number of goroutines returned by the API.
	// 0 means no limit (all goroutines are returned).
	MaxDisplay int
}

// DefaultSamplingPolicy returns a SamplingPolicy that limits display to
// 15 000 goroutines — sufficient for smooth UI rendering while keeping
// JSON responses under ~10 MB.
func DefaultSamplingPolicy() SamplingPolicy {
	return SamplingPolicy{MaxDisplay: 15_000}
}

// SampleResult is the output of SampleGoroutines.
type SampleResult struct {
	// Goroutines is the sampled (or full) list, ordered by anomaly score desc.
	Goroutines []model.Goroutine `json:"goroutines"`
	// Sampled is true when the list was truncated due to the MaxDisplay cap.
	Sampled bool `json:"sampled"`
	// TotalCount is the total number of goroutines before sampling.
	TotalCount int `json:"total_count"`
	// DisplayCount is len(Goroutines).
	DisplayCount int `json:"display_count"`
	// Warning is a human-readable message set when Sampled is true.
	Warning string `json:"warning,omitempty"`
}

// SampleGoroutines returns at most policy.MaxDisplay goroutines, prioritised by
// anomaly score (blocked/waiting goroutines with long waits rank first).
//
// If policy.MaxDisplay is 0 or len(goroutines) ≤ MaxDisplay the full slice is
// returned unchanged with Sampled = false.
func SampleGoroutines(goroutines []model.Goroutine, policy SamplingPolicy) SampleResult {
	total := len(goroutines)
	if policy.MaxDisplay <= 0 || total <= policy.MaxDisplay {
		return SampleResult{
			Goroutines:   goroutines,
			TotalCount:   total,
			DisplayCount: total,
		}
	}

	type scored struct {
		g     model.Goroutine
		score float64
	}
	candidates := make([]scored, len(goroutines))
	for i, g := range goroutines {
		candidates[i] = scored{g: g, score: goroutineAnomalyScore(g)}
	}
	// Stable sort: highest score first; ties broken by goroutine ID for determinism.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].g.ID < candidates[j].g.ID
	})

	out := make([]model.Goroutine, policy.MaxDisplay)
	for i := range out {
		out[i] = candidates[i].g
	}

	return SampleResult{
		Goroutines:   out,
		Sampled:      true,
		TotalCount:   total,
		DisplayCount: policy.MaxDisplay,
		Warning: fmt.Sprintf(
			"sampled view: showing %d of %d goroutines (prioritized by anomaly score)",
			policy.MaxDisplay, total,
		),
	}
}

// goroutineAnomalyScore returns a priority score for g.
// Higher scores mean the goroutine is more likely to be anomalous and should
// be preserved when sampling reduces the visible set.
//
// Scoring table:
//
//	Base: BLOCKED=60, WAITING=50, SYSCALL=30, RUNNING=10, other=5
//	Wait boost: >30 s → +40, >1 s → +20, >100 ms → +10
func goroutineAnomalyScore(g model.Goroutine) float64 {
	var score float64
	switch g.State {
	case model.StateBlocked:
		score = 60
	case model.StateWaiting:
		score = 50
	case model.StateSyscall:
		score = 30
	case model.StateRunning:
		score = 10
	default:
		score = 5
	}

	switch {
	case g.WaitNS > 30_000_000_000: // > 30 s — likely leak
		score += 40
	case g.WaitNS > 1_000_000_000: // > 1 s
		score += 20
	case g.WaitNS > 100_000_000: // > 100 ms
		score += 10
	}

	return score
}
