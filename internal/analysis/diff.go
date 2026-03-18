package analysis

import (
	"github.com/Khachatur86/goroscope/internal/model"
)

// CaptureDiff holds the result of comparing two captures.
type CaptureDiff struct {
	// GoroutineDeltas keyed by goroutine ID. Positive WaitDeltaNS = more blocked in compare (regression).
	GoroutineDeltas map[int64]GoroutineDelta `json:"goroutine_deltas"`
	// OnlyInBaseline are goroutine IDs that exist in baseline but not in compare.
	OnlyInBaseline []int64 `json:"only_in_baseline"`
	// OnlyInCompare are goroutine IDs that exist in compare but not in baseline.
	OnlyInCompare []int64 `json:"only_in_compare"`
}

// GoroutineDelta describes the change for a goroutine present in both captures.
type GoroutineDelta struct {
	// WaitDeltaNS is (compare.WaitNS - baseline.WaitNS). Negative = improvement.
	WaitDeltaNS int64 `json:"wait_delta_ns"`
	// BlockedDeltaNS is the change in total BLOCKED segment duration. Negative = improvement.
	BlockedDeltaNS int64 `json:"blocked_delta_ns"`
	// Status is "improved", "regressed", or "unchanged".
	Status string `json:"status"`
}

// ComputeCaptureDiff compares baseline and compare captures. Baseline is the "before" (e.g. with
// contention); compare is the "after" (e.g. after fix). Improvements (reduced wait/blocked time)
// are indicated by negative deltas.
func ComputeCaptureDiff(baselineGoroutines []model.Goroutine, baselineSegments []model.TimelineSegment,
	compareGoroutines []model.Goroutine, compareSegments []model.TimelineSegment) CaptureDiff {
	baselineByID := goroutineMap(baselineGoroutines)
	compareByID := goroutineMap(compareGoroutines)
	baselineBlocked := blockedTimeByGoroutine(baselineSegments)
	compareBlocked := blockedTimeByGoroutine(compareSegments)

	deltas := make(map[int64]GoroutineDelta)
	var onlyBaseline, onlyCompare []int64

	seen := make(map[int64]bool)
	for id := range baselineByID {
		seen[id] = true
		if _, ok := compareByID[id]; !ok {
			onlyBaseline = append(onlyBaseline, id)
			continue
		}
		b := baselineByID[id]
		c := compareByID[id]
		waitDelta := (c.WaitNS - b.WaitNS)
		blockedDelta := compareBlocked[id] - baselineBlocked[id]
		status := "unchanged"
		if waitDelta < 0 || blockedDelta < 0 {
			status = "improved"
		} else if waitDelta > 0 || blockedDelta > 0 {
			status = "regressed"
		}
		deltas[id] = GoroutineDelta{
			WaitDeltaNS:    waitDelta,
			BlockedDeltaNS: blockedDelta,
			Status:         status,
		}
	}
	for id := range compareByID {
		if !seen[id] {
			onlyCompare = append(onlyCompare, id)
		}
	}
	return CaptureDiff{
		GoroutineDeltas: deltas,
		OnlyInBaseline:  onlyBaseline,
		OnlyInCompare:   onlyCompare,
	}
}

func goroutineMap(goroutines []model.Goroutine) map[int64]model.Goroutine {
	m := make(map[int64]model.Goroutine, len(goroutines))
	for _, g := range goroutines {
		m[g.ID] = g
	}
	return m
}

func blockedTimeByGoroutine(segments []model.TimelineSegment) map[int64]int64 {
	out := make(map[int64]int64)
	for _, s := range segments {
		if s.State != model.StateBlocked {
			continue
		}
		dur := s.EndNS - s.StartNS
		out[s.GoroutineID] += dur
	}
	return out
}
