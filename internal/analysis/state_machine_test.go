package analysis

import (
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestStateMachineCreateAndStartLifecycle(t *testing.T) {
	sm := NewStateMachine()
	base := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)

	created := sm.Apply(model.Goroutine{}, model.Event{
		Kind:        model.EventKindGoroutineCreate,
		GoroutineID: 42,
		Timestamp:   base,
		Labels:      model.Labels{"function": "main.worker"},
	})

	if created.ID != 42 {
		t.Fatalf("expected goroutine id 42, got %d", created.ID)
	}
	if created.State != model.StateRunnable {
		t.Fatalf("expected state %q after create, got %q", model.StateRunnable, created.State)
	}
	if !created.CreatedAt.Equal(base) {
		t.Fatalf("expected created_at %v, got %v", base, created.CreatedAt)
	}
	if !created.LastSeenAt.Equal(base) {
		t.Fatalf("expected last_seen_at %v, got %v", base, created.LastSeenAt)
	}
	if got := created.Labels["function"]; got != "main.worker" {
		t.Fatalf("expected function label to be preserved, got %q", got)
	}

	started := sm.Apply(created, model.Event{
		Kind:      model.EventKindGoroutineStart,
		Timestamp: base.Add(5 * time.Millisecond),
	})

	if started.State != model.StateRunning {
		t.Fatalf("expected state %q after start, got %q", model.StateRunning, started.State)
	}
	if !started.CreatedAt.Equal(base) {
		t.Fatalf("expected created_at to stay %v, got %v", base, started.CreatedAt)
	}
	if started.WaitNS != 0 {
		t.Fatalf("expected wait_ns to be reset, got %d", started.WaitNS)
	}
}

func TestStateMachineTracksWaitDurationForContinuousBlock(t *testing.T) {
	sm := NewStateMachine()
	base := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)

	current := model.Goroutine{
		ID:         42,
		State:      model.StateRunning,
		CreatedAt:  base,
		LastSeenAt: base,
	}

	blocked := sm.Apply(current, model.Event{
		Kind:        model.EventKindGoroutineState,
		GoroutineID: 42,
		Timestamp:   base.Add(100 * time.Millisecond),
		State:       model.StateBlocked,
		Reason:      model.ReasonChanRecv,
		ResourceID:  "chan:0xc000018230",
	})

	if blocked.State != model.StateBlocked {
		t.Fatalf("expected state %q, got %q", model.StateBlocked, blocked.State)
	}
	if blocked.WaitNS != 0 {
		t.Fatalf("expected first blocking event to start at wait_ns=0, got %d", blocked.WaitNS)
	}
	if blocked.Reason != model.ReasonChanRecv {
		t.Fatalf("expected reason %q, got %q", model.ReasonChanRecv, blocked.Reason)
	}
	if blocked.ResourceID != "chan:0xc000018230" {
		t.Fatalf("expected resource id to be preserved, got %q", blocked.ResourceID)
	}

	stillBlocked := sm.Apply(blocked, model.Event{
		Kind:        model.EventKindGoroutineState,
		GoroutineID: 42,
		Timestamp:   base.Add(330 * time.Millisecond),
		State:       model.StateBlocked,
		Reason:      model.ReasonChanRecv,
		ResourceID:  "chan:0xc000018230",
	})

	expectedWait := int64(230 * time.Millisecond)
	if stillBlocked.WaitNS != expectedWait {
		t.Fatalf("expected wait_ns %d, got %d", expectedWait, stillBlocked.WaitNS)
	}
	if !stillBlocked.LastSeenAt.Equal(base.Add(330 * time.Millisecond)) {
		t.Fatalf("expected last_seen_at to move forward, got %v", stillBlocked.LastSeenAt)
	}
}

func TestStateMachineResetsBlockingMetadataOnWakeAndEnd(t *testing.T) {
	sm := NewStateMachine()
	base := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)

	blocked := model.Goroutine{
		ID:         77,
		State:      model.StateWaiting,
		Reason:     model.ReasonMutexLock,
		ResourceID: "mutex:0xc000014180",
		WaitNS:     int64(110 * time.Millisecond),
		CreatedAt:  base,
		LastSeenAt: base.Add(110 * time.Millisecond),
	}

	running := sm.Apply(blocked, model.Event{
		Kind:        model.EventKindGoroutineState,
		GoroutineID: 77,
		Timestamp:   base.Add(220 * time.Millisecond),
		State:       model.StateRunning,
	})

	if running.State != model.StateRunning {
		t.Fatalf("expected state %q, got %q", model.StateRunning, running.State)
	}
	if running.WaitNS != 0 {
		t.Fatalf("expected wait_ns reset on wake, got %d", running.WaitNS)
	}
	if running.Reason != "" {
		t.Fatalf("expected reason to be cleared, got %q", running.Reason)
	}
	if running.ResourceID != "" {
		t.Fatalf("expected resource id to be cleared, got %q", running.ResourceID)
	}

	done := sm.Apply(running, model.Event{
		Kind:        model.EventKindGoroutineEnd,
		GoroutineID: 77,
		Timestamp:   base.Add(300 * time.Millisecond),
	})

	if done.State != model.StateDone {
		t.Fatalf("expected state %q after end, got %q", model.StateDone, done.State)
	}
	if done.WaitNS != 0 {
		t.Fatalf("expected wait_ns to stay reset after end, got %d", done.WaitNS)
	}
}
