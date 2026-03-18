package analysis

import (
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestComputeCaptureDiff(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)

	// Baseline: G1 and G2 both blocked 100ms
	baselineGoroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning, WaitNS: 0, CreatedAt: base, LastSeenAt: base},
		{ID: 2, State: model.StateBlocked, WaitNS: int64(100 * time.Millisecond), CreatedAt: base, LastSeenAt: base},
	}
	baselineSegments := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 100, State: model.StateRunning},
		{GoroutineID: 2, StartNS: 50, EndNS: 150, State: model.StateBlocked},
	}

	// Compare: G2 blocked only 30ms (improvement)
	compareGoroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning, WaitNS: 0, CreatedAt: base, LastSeenAt: base},
		{ID: 2, State: model.StateBlocked, WaitNS: int64(30 * time.Millisecond), CreatedAt: base, LastSeenAt: base},
	}
	compareSegments := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 100, State: model.StateRunning},
		{GoroutineID: 2, StartNS: 50, EndNS: 80, State: model.StateBlocked},
	}

	diff := ComputeCaptureDiff(baselineGoroutines, baselineSegments, compareGoroutines, compareSegments)

	if d, ok := diff.GoroutineDeltas[2]; !ok {
		t.Fatal("expected delta for G2")
	} else {
		if d.Status != "improved" {
			t.Errorf("G2 status = %q, want improved", d.Status)
		}
		if d.WaitDeltaNS >= 0 {
			t.Errorf("G2 WaitDeltaNS = %d, want negative", d.WaitDeltaNS)
		}
		if d.BlockedDeltaNS >= 0 {
			t.Errorf("G2 BlockedDeltaNS = %d, want negative", d.BlockedDeltaNS)
		}
	}
}

func TestComputeCaptureDiff_OnlyInOne(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)

	baselineGoroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning, CreatedAt: base, LastSeenAt: base},
		{ID: 2, State: model.StateRunning, CreatedAt: base, LastSeenAt: base},
	}
	compareGoroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning, CreatedAt: base, LastSeenAt: base},
		{ID: 3, State: model.StateRunning, CreatedAt: base, LastSeenAt: base},
	}

	diff := ComputeCaptureDiff(baselineGoroutines, nil, compareGoroutines, nil)

	if len(diff.OnlyInBaseline) != 1 || diff.OnlyInBaseline[0] != 2 {
		t.Errorf("OnlyInBaseline = %v, want [2]", diff.OnlyInBaseline)
	}
	if len(diff.OnlyInCompare) != 1 || diff.OnlyInCompare[0] != 3 {
		t.Errorf("OnlyInCompare = %v, want [3]", diff.OnlyInCompare)
	}
}
