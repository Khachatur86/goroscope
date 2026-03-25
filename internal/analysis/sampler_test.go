package analysis

import (
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func makeGoroutine(id int64, state model.GoroutineState, waitNS int64) model.Goroutine {
	return model.Goroutine{
		ID:         id,
		State:      state,
		WaitNS:     waitNS,
		CreatedAt:  time.Now(),
		LastSeenAt: time.Now(),
	}
}

func TestSampleGoroutines_BelowThreshold(t *testing.T) {
	t.Parallel()
	goroutines := []model.Goroutine{
		makeGoroutine(1, model.StateRunning, 0),
		makeGoroutine(2, model.StateWaiting, 0),
	}
	policy := SamplingPolicy{MaxDisplay: 10}
	result := SampleGoroutines(goroutines, policy)

	if result.Sampled {
		t.Error("expected Sampled=false when count <= MaxDisplay")
	}
	if result.TotalCount != 2 || result.DisplayCount != 2 {
		t.Errorf("expected total=2 display=2, got total=%d display=%d", result.TotalCount, result.DisplayCount)
	}
	if result.Warning != "" {
		t.Errorf("expected no warning, got %q", result.Warning)
	}
}

func TestSampleGoroutines_NoLimit(t *testing.T) {
	t.Parallel()
	goroutines := make([]model.Goroutine, 50_000)
	for i := range goroutines {
		goroutines[i] = makeGoroutine(int64(i+1), model.StateRunning, 0)
	}
	result := SampleGoroutines(goroutines, SamplingPolicy{MaxDisplay: 0})
	if result.Sampled {
		t.Error("expected Sampled=false for unlimited policy")
	}
	if result.DisplayCount != 50_000 {
		t.Errorf("expected display=50000, got %d", result.DisplayCount)
	}
}

func TestSampleGoroutines_PrioritisesAnomalous(t *testing.T) {
	t.Parallel()
	goroutines := []model.Goroutine{
		makeGoroutine(1, model.StateRunning, 0),              // score 10
		makeGoroutine(2, model.StateBlocked, 60_000_000_000), // score 60+40=100 (>30s)
		makeGoroutine(3, model.StateWaiting, 2_000_000_000),  // score 50+20=70  (>1s)
		makeGoroutine(4, model.StateSyscall, 0),              // score 30
		makeGoroutine(5, model.StateWaiting, 0),              // score 50
		makeGoroutine(6, model.StateBlocked, 200_000_000),    // score 60+10=70  (>100ms)
	}
	policy := SamplingPolicy{MaxDisplay: 3}
	result := SampleGoroutines(goroutines, policy)

	if !result.Sampled {
		t.Fatal("expected Sampled=true")
	}
	if result.TotalCount != 6 {
		t.Errorf("TotalCount: want 6, got %d", result.TotalCount)
	}
	if result.DisplayCount != 3 {
		t.Errorf("DisplayCount: want 3, got %d", result.DisplayCount)
	}

	// Top 3 by score: G2(100), G3(70), G6(70) — tie broken by ID so G3 < G6.
	wantIDs := []int64{2, 3, 6}
	for i, g := range result.Goroutines {
		if g.ID != wantIDs[i] {
			t.Errorf("position %d: want G%d, got G%d", i, wantIDs[i], g.ID)
		}
	}
}

func TestSampleGoroutines_WarningMessage(t *testing.T) {
	t.Parallel()
	goroutines := make([]model.Goroutine, 20)
	for i := range goroutines {
		goroutines[i] = makeGoroutine(int64(i+1), model.StateRunning, 0)
	}
	result := SampleGoroutines(goroutines, SamplingPolicy{MaxDisplay: 5})
	if result.Warning == "" {
		t.Error("expected non-empty Warning when sampled")
	}
}

func TestGoroutineAnomalyScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		g         model.Goroutine
		wantScore float64
	}{
		{"blocked 30s+", makeGoroutine(1, model.StateBlocked, 31_000_000_000), 100},
		{"waiting 1s+", makeGoroutine(2, model.StateWaiting, 2_000_000_000), 70},
		{"syscall no wait", makeGoroutine(3, model.StateSyscall, 0), 30},
		{"running", makeGoroutine(4, model.StateRunning, 0), 10},
		{"blocked 200ms", makeGoroutine(5, model.StateBlocked, 200_000_000), 70},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := goroutineAnomalyScore(tc.g)
			if got != tc.wantScore {
				t.Errorf("anomalyScore: want %v, got %v", tc.wantScore, got)
			}
		})
	}
}
