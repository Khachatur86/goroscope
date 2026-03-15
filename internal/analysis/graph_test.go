package analysis

import (
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestDeriveResourceEdgesFromTimeline(t *testing.T) {
	segments := []model.TimelineSegment{
		{GoroutineID: 1, ResourceID: "chan:0x1", State: model.StateBlocked},
		{GoroutineID: 2, ResourceID: "chan:0x1", State: model.StateBlocked},
		{GoroutineID: 2, ResourceID: "mutex:0x2", State: model.StateBlocked},
		{GoroutineID: 3, ResourceID: "mutex:0x2", State: model.StateBlocked},
	}
	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked},
		{ID: 2, State: model.StateBlocked},
		{ID: 3, State: model.StateBlocked},
	}

	edges := DeriveResourceEdgesFromTimeline(segments, goroutines)

	if len(edges) < 2 {
		t.Fatalf("expected at least 2 edges (1-2 and 2-3), got %d", len(edges))
	}

	has12 := false
	has23 := false
	for _, e := range edges {
		if (e.FromGoroutineID == 1 && e.ToGoroutineID == 2) || (e.FromGoroutineID == 2 && e.ToGoroutineID == 1) {
			has12 = true
		}
		if (e.FromGoroutineID == 2 && e.ToGoroutineID == 3) || (e.FromGoroutineID == 3 && e.ToGoroutineID == 2) {
			has23 = true
		}
	}
	if !has12 {
		t.Error("expected edge between G1 and G2 (chan:0x1)")
	}
	if !has23 {
		t.Error("expected edge between G2 and G3 (mutex:0x2)")
	}
}

func TestFindDeadlockHints(t *testing.T) {
	edges := []model.ResourceEdge{
		{FromGoroutineID: 1, ToGoroutineID: 2, ResourceID: "chan:0x1"},
		{FromGoroutineID: 2, ToGoroutineID: 1, ResourceID: "chan:0x1"},
		{FromGoroutineID: 2, ToGoroutineID: 3, ResourceID: "mutex:0x2"},
		{FromGoroutineID: 3, ToGoroutineID: 2, ResourceID: "mutex:0x2"},
		{FromGoroutineID: 3, ToGoroutineID: 1, ResourceID: "chan:0x3"},
		{FromGoroutineID: 1, ToGoroutineID: 3, ResourceID: "chan:0x3"},
	}
	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked},
		{ID: 2, State: model.StateBlocked},
		{ID: 3, State: model.StateBlocked},
	}

	hints := FindDeadlockHints(edges, goroutines)

	if len(hints) == 0 {
		t.Fatal("expected at least one deadlock hint (cycle 1-2-3)")
	}

	found := false
	for _, h := range hints {
		if len(h.GoroutineIDs) >= 2 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected hint with cycle, got %v", hints)
	}
}

func TestFindDeadlockHints_NoHintWhenNotBlocked(t *testing.T) {
	edges := []model.ResourceEdge{
		{FromGoroutineID: 1, ToGoroutineID: 2, ResourceID: "chan:0x1"},
		{FromGoroutineID: 2, ToGoroutineID: 1, ResourceID: "chan:0x1"},
	}
	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked},
		{ID: 2, State: model.StateRunning}, // not blocked
	}

	hints := FindDeadlockHints(edges, goroutines)

	if len(hints) != 0 {
		t.Errorf("expected no hints when cycle has running goroutine, got %v", hints)
	}
}
