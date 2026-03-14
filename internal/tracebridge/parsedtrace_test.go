package tracebridge

import (
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestParseParsedTrace(t *testing.T) {
	const parsed = `M=-1 P=-1 G=-1 Sync Time=100 N=1 Trace=101 Mono=100 Wall=2026-03-14T12:00:00Z
M=1 P=0 G=1 StateTransition Time=100 Resource=Goroutine(1) Reason="" GoID=1 Undetermined->Running
Stack=
	main.main @ 0x1
		/work/main.go:10

M=1 P=0 G=1 StateTransition Time=200 Resource=Goroutine(1) Reason="chan receive" GoID=1 Running->Waiting
Stack=
	main.main @ 0x1
		/work/main.go:20

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

	if len(capture.Events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(capture.Events))
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
	if capture.Events[4].Kind != model.EventKindGoroutineEnd {
		t.Fatalf("expected last event kind %q, got %q", model.EventKindGoroutineEnd, capture.Events[4].Kind)
	}
	if len(capture.Stacks) != 4 {
		t.Fatalf("expected 4 stack snapshots, got %d", len(capture.Stacks))
	}
	if capture.Stacks[0].Frames[0].Func != "main.main" {
		t.Fatalf("expected first stack frame main.main, got %+v", capture.Stacks[0].Frames[0])
	}
}
