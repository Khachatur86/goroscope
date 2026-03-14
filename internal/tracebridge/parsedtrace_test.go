package tracebridge

import (
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestParseParsedTrace(t *testing.T) {
	// Trace format mirrors actual "go tool trace -d=parsed" output:
	//   G=<running-goroutine> in the prefix is the parent on NotExist→* lines.
	//   GoID= is redundant (always equals Resource=Goroutine(N)).
	const parsed = `M=-1 P=-1 G=-1 Sync Time=100 N=1 Trace=101 Mono=100 Wall=2026-03-14T12:00:00Z
M=1 P=0 G=-1 StateTransition Time=100 Resource=Goroutine(1) Reason="" GoID=1 Undetermined->Running
Stack=
	main.main @ 0x1
		/work/main.go:10

M=1 P=0 G=1 StateTransition Time=150 Resource=Goroutine(5) Reason="" GoID=5 NotExist->Runnable

M=1 P=0 G=1 StateTransition Time=200 Resource=Goroutine(1) Reason="chan receive" GoID=1 Running->Waiting
Stack=
	main.main @ 0x1
		/work/main.go:20

M=1 P=0 G=5 StateTransition Time=210 Resource=Goroutine(5) Reason="" GoID=5 Runnable->Running
Stack=
	main.worker @ 0x2
		/work/main.go:60

M=1 P=0 G=1 StateTransition Time=300 Resource=Goroutine(1) Reason="" GoID=1 Waiting->Runnable
Stack=
	main.main @ 0x1
		/work/main.go:21

M=1 P=0 G=1 StateTransition Time=400 Resource=Goroutine(1) Reason="" GoID=1 Runnable->Running
Stack=
	main.main @ 0x1
		/work/main.go:22

M=1 P=0 G=1 StateTransition Time=500 Resource=Goroutine(1) Reason="" GoID=1 Running->NotExist
`

	capture, err := ParseParsedTrace(strings.NewReader(parsed))
	if err != nil {
		t.Fatalf("expected parsed trace to decode: %v", err)
	}

	// G1: create, chan-recv block, unblock, run, end = 5 events
	// G5: create (no stack → filtered), run = 1 event
	if len(capture.Events) != 6 {
		t.Fatalf("expected 6 events, got %d: %v", len(capture.Events), capture.Events)
	}
	if capture.Events[0].Kind != model.EventKindGoroutineCreate {
		t.Fatalf("expected first event kind %q, got %q", model.EventKindGoroutineCreate, capture.Events[0].Kind)
	}
	if capture.Events[0].State != model.StateRunning {
		t.Fatalf("expected first event state %q, got %q", model.StateRunning, capture.Events[0].State)
	}
	if capture.Events[1].State != model.StateBlocked || capture.Events[1].Reason != model.ReasonChanRecv {
		t.Fatalf("expected blocking event, got %+v", capture.Events[1])
	}
	if capture.Events[5].Kind != model.EventKindGoroutineEnd {
		t.Fatalf("expected last event kind %q, got %q", model.EventKindGoroutineEnd, capture.Events[5].Kind)
	}
	if len(capture.Stacks) != 4 {
		t.Fatalf("expected 4 stack snapshots, got %d", len(capture.Stacks))
	}
	if capture.Stacks[0].Frames[0].Func != "main.main" {
		t.Fatalf("expected first stack frame main.main, got %+v", capture.Stacks[0].Frames[0])
	}

	// G5 was spawned by G1 — parent ID must survive the user-frame filter.
	if capture.ParentIDs[5] != 1 {
		t.Fatalf("expected G5 parent_id=1, got %d (ParentIDs=%v)", capture.ParentIDs[5], capture.ParentIDs)
	}
	// G1 has no parent (G=-1 on its create transition).
	if _, ok := capture.ParentIDs[1]; ok {
		t.Fatalf("expected G1 to have no parent, got parent_id=%d", capture.ParentIDs[1])
	}
}

func TestMapWaitingReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason      string
		wantState   model.GoroutineState
		wantBlocked model.BlockingReason
	}{
		{"chan receive", model.StateBlocked, model.ReasonChanRecv},
		{"chan send", model.StateBlocked, model.ReasonChanSend},
		{"select", model.StateBlocked, model.ReasonSelect},
		{"sync", model.StateBlocked, model.ReasonMutexLock},
		{"sync.(*Cond).Wait", model.StateWaiting, model.ReasonSyncCond},
		{"sleep", model.StateWaiting, model.ReasonSleep},
		{"GC mark assist wait for work", model.StateWaiting, model.ReasonGCAssist},
		{"GC background sweeper wait", model.StateWaiting, model.ReasonGCAssist},
		{"GC weak to strong wait", model.StateWaiting, model.ReasonGCAssist},
		{"network", model.StateWaiting, model.ReasonUnknown},
		{"forever", model.StateWaiting, model.ReasonUnknown},
		{"system goroutine wait", model.StateWaiting, model.ReasonUnknown},
		{"", model.StateWaiting, model.ReasonUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			t.Parallel()
			gotState, gotReason := mapWaitingReason(tc.reason)
			if gotState != tc.wantState || gotReason != tc.wantBlocked {
				t.Errorf("mapWaitingReason(%q) = (%q, %q), want (%q, %q)",
					tc.reason, gotState, gotReason, tc.wantState, tc.wantBlocked)
			}
		})
	}
}
