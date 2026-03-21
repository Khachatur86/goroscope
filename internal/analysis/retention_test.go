package analysis

import (
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// makeSession returns a minimal Session suitable for Engine.Reset.
func makeSession(id string) *model.Session {
	return &model.Session{
		ID:        id,
		Name:      id,
		Target:    "demo://retention-test",
		Status:    model.SessionStatusRunning,
		StartedAt: time.Now(),
	}
}

// applyStateTransition drives the engine through a simple create→running→blocked cycle
// for the given goroutine ID, producing exactly two closed segments.
func applyStateTransition(e *Engine, goroutineID int64, base time.Time) {
	e.ApplyEvents([]model.Event{
		{Kind: model.EventKindGoroutineCreate, GoroutineID: goroutineID, Timestamp: base},
		{Kind: model.EventKindGoroutineStart, GoroutineID: goroutineID, Timestamp: base.Add(time.Millisecond)},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: goroutineID,
			Timestamp:   base.Add(2 * time.Millisecond),
			State:       model.StateBlocked,
			Reason:      model.ReasonChanRecv,
			ResourceID:  "chan:0x1",
		},
		{Kind: model.EventKindGoroutineEnd, GoroutineID: goroutineID, Timestamp: base.Add(3 * time.Millisecond)},
	})
}

// TestDefaultRetentionPolicy verifies that DefaultRetentionPolicy returns non-zero caps.
func TestDefaultRetentionPolicy(t *testing.T) {
	t.Parallel()

	p := DefaultRetentionPolicy()
	if p.MaxClosedSegments <= 0 {
		t.Errorf("MaxClosedSegments = %d; want > 0", p.MaxClosedSegments)
	}
	if p.MaxStacksPerGoroutine <= 0 {
		t.Errorf("MaxStacksPerGoroutine = %d; want > 0", p.MaxStacksPerGoroutine)
	}
}

// TestWithRetentionDisable verifies that passing a zero RetentionPolicy disables all limits.
func TestWithRetentionDisable(t *testing.T) {
	t.Parallel()

	e := NewEngine(WithRetention(RetentionPolicy{}))
	stats := e.MemoryStats()

	if stats.MaxClosedSegments != 0 {
		t.Errorf("MaxClosedSegments = %d; want 0 (unlimited)", stats.MaxClosedSegments)
	}
	if stats.MaxStacksPerGoroutine != 0 {
		t.Errorf("MaxStacksPerGoroutine = %d; want 0 (unlimited)", stats.MaxStacksPerGoroutine)
	}
}

// TestClosedSegmentsTrimming verifies that the engine drops oldest closed segments
// when MaxClosedSegments is exceeded.
func TestClosedSegmentsTrimming(t *testing.T) {
	t.Parallel()

	const cap = 5
	e := NewEngine(WithRetention(RetentionPolicy{
		MaxClosedSegments:     cap,
		MaxStacksPerGoroutine: 0, // unlimited
	}))
	e.Reset(makeSession("seg-trim"))

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Each applyStateTransition produces 2 closed segments (running + blocked).
	// Adding 5 goroutines → 10 closed segments, well over the cap of 5.
	for i := int64(1); i <= 5; i++ {
		applyStateTransition(e, i, base.Add(time.Duration(i)*time.Second))
	}

	stats := e.MemoryStats()
	if stats.ClosedSegments > cap+(cap/10)+1 {
		t.Errorf("ClosedSegments = %d; want ≤ %d (cap + hysteresis)", stats.ClosedSegments, cap+(cap/10)+1)
	}
}

// TestClosedSegmentsNoTrimWhenUnderCap verifies that segments are not trimmed
// prematurely when the count is below the cap.
func TestClosedSegmentsNoTrimWhenUnderCap(t *testing.T) {
	t.Parallel()

	const cap = 1000
	e := NewEngine(WithRetention(RetentionPolicy{
		MaxClosedSegments:     cap,
		MaxStacksPerGoroutine: 0,
	}))
	e.Reset(makeSession("seg-notrim"))

	base := time.Now()
	// 2 goroutines → at most 4 closed segments, far below cap of 1000.
	for i := int64(1); i <= 2; i++ {
		applyStateTransition(e, i, base.Add(time.Duration(i)*time.Second))
	}

	stats := e.MemoryStats()
	if stats.ClosedSegments > cap {
		t.Errorf("ClosedSegments = %d unexpectedly exceeded cap %d", stats.ClosedSegments, cap)
	}
	if stats.ClosedSegments == 0 {
		t.Errorf("ClosedSegments = 0; expected some segments to be retained")
	}
}

// TestStackSnapshotsTrimming verifies that per-goroutine stack snapshots are
// trimmed when MaxStacksPerGoroutine is exceeded.
func TestStackSnapshotsTrimming(t *testing.T) {
	t.Parallel()

	const maxPer = 3
	e := NewEngine(WithRetention(RetentionPolicy{
		MaxClosedSegments:     0,
		MaxStacksPerGoroutine: maxPer,
	}))
	e.Reset(makeSession("stacks-trim"))

	base := time.Now()
	const goroutineID int64 = 7

	// Push maxPer+3 stacks for the same goroutine.
	for i := 0; i < maxPer+3; i++ {
		e.ApplyStackSnapshot(model.StackSnapshot{
			GoroutineID: goroutineID,
			Timestamp:   base.Add(time.Duration(i) * time.Millisecond),
			Frames: []model.StackFrame{
				{Func: "main.worker", File: "main.go", Line: 10},
			},
		})
	}

	stacks := e.GetStacksFor(goroutineID)
	if len(stacks) > maxPer {
		t.Errorf("GetStacksFor returned %d stacks; want ≤ %d", len(stacks), maxPer)
	}
	if len(stacks) == 0 {
		t.Errorf("GetStacksFor returned 0 stacks; want at least 1 retained")
	}
}

// TestStackSnapshotsRetainNewest verifies that after trimming the newest snapshots
// are kept (not the oldest).
func TestStackSnapshotsRetainNewest(t *testing.T) {
	t.Parallel()

	const maxPer = 2
	e := NewEngine(WithRetention(RetentionPolicy{
		MaxClosedSegments:     0,
		MaxStacksPerGoroutine: maxPer,
	}))
	e.Reset(makeSession("stacks-newest"))

	base := time.Now()
	const goroutineID int64 = 11

	// Push 4 stacks; newest should be the survivors.
	for i := 0; i < 4; i++ {
		e.ApplyStackSnapshot(model.StackSnapshot{
			GoroutineID: goroutineID,
			Timestamp:   base.Add(time.Duration(i) * time.Millisecond),
			Frames: []model.StackFrame{
				{Func: "main.worker", File: "main.go", Line: i + 1},
			},
		})
	}

	stacks := e.GetStacksFor(goroutineID)
	// The newest snapshot has Line=4 (i=3), second newest has Line=3 (i=2).
	// We want the two newest to survive.
	newestLine := 4
	found := false
	for _, s := range stacks {
		for _, f := range s.Frames {
			if f.Line == newestLine {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("newest stack snapshot (Line=%d) was trimmed; got stacks: %+v", newestLine, stacks)
	}
}

// TestMemoryStatsReflectsRetentionConfig verifies that MemoryStats returns the
// configured caps.
func TestMemoryStatsReflectsRetentionConfig(t *testing.T) {
	t.Parallel()

	p := RetentionPolicy{MaxClosedSegments: 12345, MaxStacksPerGoroutine: 67}
	e := NewEngine(WithRetention(p))

	stats := e.MemoryStats()
	if stats.MaxClosedSegments != p.MaxClosedSegments {
		t.Errorf("MaxClosedSegments = %d; want %d", stats.MaxClosedSegments, p.MaxClosedSegments)
	}
	if stats.MaxStacksPerGoroutine != p.MaxStacksPerGoroutine {
		t.Errorf("MaxStacksPerGoroutine = %d; want %d", stats.MaxStacksPerGoroutine, p.MaxStacksPerGoroutine)
	}
}

// TestMemoryStatsCountsAreAccurate verifies that MemoryStats counts match
// observed engine state.
func TestMemoryStatsCountsAreAccurate(t *testing.T) {
	t.Parallel()

	e := NewEngine(WithRetention(RetentionPolicy{})) // unlimited
	e.Reset(makeSession("stats-accurate"))

	base := time.Now()
	// Add 3 goroutines with state transitions.
	for i := int64(1); i <= 3; i++ {
		applyStateTransition(e, i, base.Add(time.Duration(i)*time.Second))
	}

	stats := e.MemoryStats()
	if stats.Goroutines != 3 {
		t.Errorf("Goroutines = %d; want 3", stats.Goroutines)
	}
	if stats.ClosedSegments == 0 {
		t.Errorf("ClosedSegments = 0; want > 0")
	}
}

// TestNoLimitRetentionGrowsUnbounded verifies that with zero limits, data is
// not trimmed regardless of volume.
func TestNoLimitRetentionGrowsUnbounded(t *testing.T) {
	t.Parallel()

	e := NewEngine(WithRetention(RetentionPolicy{}))
	e.Reset(makeSession("unlimited"))

	base := time.Now()
	const n = 20
	for i := int64(1); i <= n; i++ {
		applyStateTransition(e, i, base.Add(time.Duration(i)*time.Second))
	}

	stats := e.MemoryStats()
	// Each goroutine goes through create→running→blocked→end = at minimum 2 closed segments.
	if stats.ClosedSegments < n {
		t.Errorf("ClosedSegments = %d; want ≥ %d (unlimited mode dropped data)", stats.ClosedSegments, n)
	}
}
