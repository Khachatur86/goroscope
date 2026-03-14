package analysis

import (
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestEngineBuildsTimelineFromEventStream(t *testing.T) {
	engine := NewEngine()
	base := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	engine.Reset(&model.Session{
		ID:        "sess_test",
		Name:      "test",
		Target:    "demo://timeline",
		Status:    model.SessionStatusRunning,
		StartedAt: base,
	})

	engine.ApplyEvents([]model.Event{
		{
			Kind:        model.EventKindGoroutineCreate,
			GoroutineID: 42,
			Timestamp:   base,
			Labels:      model.Labels{"function": "main.worker"},
		},
		{
			Kind:        model.EventKindGoroutineStart,
			GoroutineID: 42,
			Timestamp:   base,
		},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: 42,
			Timestamp:   base.Add(100 * time.Millisecond),
			State:       model.StateBlocked,
			Reason:      model.ReasonChanRecv,
			ResourceID:  "chan:0xc000018230",
		},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: 42,
			Timestamp:   base.Add(330 * time.Millisecond),
			State:       model.StateBlocked,
			Reason:      model.ReasonChanRecv,
			ResourceID:  "chan:0xc000018230",
		},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: 42,
			Timestamp:   base.Add(400 * time.Millisecond),
			State:       model.StateRunning,
		},
	})

	goroutine, ok := engine.GetGoroutine(42)
	if !ok {
		t.Fatal("expected goroutine 42 to exist")
	}
	if goroutine.State != model.StateRunning {
		t.Fatalf("expected final state %q, got %q", model.StateRunning, goroutine.State)
	}
	if goroutine.WaitNS != 0 {
		t.Fatalf("expected wait_ns reset after wake, got %d", goroutine.WaitNS)
	}
	if goroutine.Reason != "" || goroutine.ResourceID != "" {
		t.Fatalf("expected blocking metadata cleared after wake, got reason=%q resource=%q", goroutine.Reason, goroutine.ResourceID)
	}
	if got := goroutine.Labels["function"]; got != "main.worker" {
		t.Fatalf("expected function label to survive, got %q", got)
	}

	timeline := engine.Timeline()
	if len(timeline) != 2 {
		t.Fatalf("expected 2 timeline segments, got %d", len(timeline))
	}
	if timeline[0].State != model.StateRunning {
		t.Fatalf("expected first segment state %q, got %q", model.StateRunning, timeline[0].State)
	}
	if timeline[0].StartNS != base.UnixNano() || timeline[0].EndNS != base.Add(100*time.Millisecond).UnixNano() {
		t.Fatalf("unexpected running segment bounds: %+v", timeline[0])
	}
	if timeline[1].State != model.StateBlocked {
		t.Fatalf("expected second segment state %q, got %q", model.StateBlocked, timeline[1].State)
	}
	if timeline[1].Reason != model.ReasonChanRecv || timeline[1].ResourceID != "chan:0xc000018230" {
		t.Fatalf("unexpected blocked segment metadata: %+v", timeline[1])
	}
	if timeline[1].StartNS != base.Add(100*time.Millisecond).UnixNano() || timeline[1].EndNS != base.Add(400*time.Millisecond).UnixNano() {
		t.Fatalf("unexpected blocked segment bounds: %+v", timeline[1])
	}
}

func TestEngineStoresLatestStackSnapshot(t *testing.T) {
	engine := NewEngine()
	base := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	engine.Reset(&model.Session{
		ID:        "sess_stack",
		Name:      "stack",
		Target:    "demo://stack",
		Status:    model.SessionStatusRunning,
		StartedAt: base,
	})

	engine.ApplyEvents([]model.Event{
		{
			Kind:        model.EventKindGoroutineCreate,
			GoroutineID: 77,
			Timestamp:   base,
		},
		{
			Kind:        model.EventKindGoroutineStart,
			GoroutineID: 77,
			Timestamp:   base,
		},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: 77,
			Timestamp:   base.Add(100 * time.Millisecond),
			State:       model.StateWaiting,
			Reason:      model.ReasonMutexLock,
			ResourceID:  "mutex:0xc000014180",
		},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: 77,
			Timestamp:   base.Add(330 * time.Millisecond),
			State:       model.StateWaiting,
			Reason:      model.ReasonMutexLock,
			ResourceID:  "mutex:0xc000014180",
		},
	})

	engine.ApplyStackSnapshot(model.StackSnapshot{
		SessionID:   "sess_stack",
		Seq:         5,
		Timestamp:   base.Add(350 * time.Millisecond),
		StackID:     "stk_sink",
		GoroutineID: 77,
		Frames: []model.StackFrame{
			{Func: "main.sink", File: "/workspace/app/main.go", Line: 73},
			{Func: "main.main", File: "/workspace/app/main.go", Line: 95},
		},
	})

	goroutine, ok := engine.GetGoroutine(77)
	if !ok {
		t.Fatal("expected goroutine 77 to exist")
	}
	if goroutine.State != model.StateWaiting {
		t.Fatalf("expected final state %q, got %q", model.StateWaiting, goroutine.State)
	}
	if goroutine.WaitNS != int64(230*time.Millisecond) {
		t.Fatalf("expected wait_ns 230ms, got %d", goroutine.WaitNS)
	}
	if goroutine.LastStack == nil {
		t.Fatal("expected stack snapshot to be stored")
	}
	if len(goroutine.LastStack.Frames) != 2 {
		t.Fatalf("expected 2 stack frames, got %d", len(goroutine.LastStack.Frames))
	}
	if !goroutine.LastSeenAt.Equal(base.Add(350 * time.Millisecond)) {
		t.Fatalf("expected last_seen_at extended by stack snapshot, got %v", goroutine.LastSeenAt)
	}

	timeline := engine.Timeline()
	if len(timeline) != 2 {
		t.Fatalf("expected 2 timeline segments, got %d", len(timeline))
	}
	if timeline[1].State != model.StateWaiting {
		t.Fatalf("expected second segment state %q, got %q", model.StateWaiting, timeline[1].State)
	}
	if timeline[1].StartNS != base.Add(100*time.Millisecond).UnixNano() || timeline[1].EndNS != base.Add(350*time.Millisecond).UnixNano() {
		t.Fatalf("unexpected waiting segment bounds: %+v", timeline[1])
	}
}
