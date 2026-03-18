package analysis

import (
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestLeakCandidates(t *testing.T) {
	t.Parallel()

	threshold := 30 * int64(time.Second)
	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, WaitNS: 25 * int64(time.Second)},
		{ID: 2, State: model.StateWaiting, WaitNS: 35 * int64(time.Second)},
		{ID: 3, State: model.StateBlocked, WaitNS: 40 * int64(time.Second)},
		{ID: 4, State: model.StateRunning, WaitNS: 60 * int64(time.Second)},
		{ID: 5, State: model.StateSyscall, WaitNS: 35 * int64(time.Second)},
		{ID: 6, State: model.StateBlocked, WaitNS: 29 * int64(time.Second)},
	}

	out := LeakCandidates(goroutines, threshold)

	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (G2 and G3 only)", len(out))
	}
	ids := make(map[int64]bool)
	for _, g := range out {
		ids[g.ID] = true
	}
	if !ids[2] || !ids[3] {
		t.Errorf("expected G2 and G3, got %v", out)
	}
	// G1: blocked but only 25s < 30s
	// G4: running, not a leak candidate state
	// G5: syscall, not a leak candidate state (only WAITING/BLOCKED)
	// G6: blocked but 29s < 30s
}

func TestLeakCandidates_ZeroThreshold(t *testing.T) {
	t.Parallel()

	out := LeakCandidates([]model.Goroutine{
		{ID: 1, State: model.StateBlocked, WaitNS: 60 * int64(time.Second)},
	}, 0)

	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0 for zero threshold", len(out))
	}
}
