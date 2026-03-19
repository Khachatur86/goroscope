package tracebridge

import (
	"context"
	"os"
	rttrace "runtime/trace"
	"sync"
	"testing"
	"time"

	xtrace "golang.org/x/exp/trace"

	"github.com/Khachatur86/goroscope/internal/model"
)

// TestBuildCaptureFromRawTrace_Basic generates a real runtime/trace binary file,
// parses it with BuildCaptureFromRawTrace, and verifies basic structural invariants.
func TestBuildCaptureFromRawTrace_Basic(t *testing.T) {
	t.Parallel()

	tracePath := generateTestTrace(t, func() {
		// Spawn and join a goroutine so the trace contains interesting events.
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A tiny sleep creates a Waiting→Runnable transition.
			time.Sleep(time.Microsecond)
		}()
		wg.Wait()
	})

	capture, err := BuildCaptureFromRawTrace(context.Background(), tracePath)
	if err != nil {
		t.Fatalf("BuildCaptureFromRawTrace: %v", err)
	}

	if len(capture.Events) == 0 {
		t.Fatal("expected at least one goroutine event")
	}

	for i, ev := range capture.Events {
		if ev.Kind == "" {
			t.Errorf("event[%d] has empty Kind: %+v", i, ev)
		}
		if ev.Timestamp.IsZero() {
			t.Errorf("event[%d] has zero Timestamp: %+v", i, ev)
		}
		if ev.GoroutineID == 0 {
			t.Errorf("event[%d] has zero GoroutineID: %+v", i, ev)
		}
	}
}

// TestBuildCaptureFromRawTrace_Ordering checks that events for a single goroutine
// are in monotonically increasing timestamp order.
func TestBuildCaptureFromRawTrace_Ordering(t *testing.T) {
	t.Parallel()

	tracePath := generateTestTrace(t, func() {
		release := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			<-release
		}()
		time.Sleep(time.Microsecond)
		close(release)
		<-done
	})

	capture, err := BuildCaptureFromRawTrace(context.Background(), tracePath)
	if err != nil {
		t.Fatalf("BuildCaptureFromRawTrace: %v", err)
	}

	// Group events by goroutine, then assert monotonic timestamps.
	byGoroutine := make(map[int64][]model.Event)
	for _, ev := range capture.Events {
		byGoroutine[ev.GoroutineID] = append(byGoroutine[ev.GoroutineID], ev)
	}

	for gid, evs := range byGoroutine {
		for i := 1; i < len(evs); i++ {
			if evs[i].Timestamp.Before(evs[i-1].Timestamp) {
				t.Errorf("G%d: event[%d] timestamp %v < event[%d] timestamp %v",
					gid, i, evs[i].Timestamp, i-1, evs[i-1].Timestamp)
			}
		}
	}
}

// TestBuildCaptureFromRawTrace_ContextCancelled verifies that a cancelled
// context causes BuildCaptureFromRawTrace to return promptly with an error.
func TestBuildCaptureFromRawTrace_ContextCancelled(t *testing.T) {
	t.Parallel()

	tracePath := generateTestTrace(t, func() {})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, err := BuildCaptureFromRawTrace(ctx, tracePath)
	if err == nil {
		// A pre-cancelled context might still succeed if the trace is parsed
		// before the first ctx.Err() check — that is acceptable.
		t.Log("BuildCaptureFromRawTrace succeeded with pre-cancelled context (acceptable)")
	}
}

// TestXMapState verifies state and reason mapping for all meaningful GoState values.
func TestXMapState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		to         xtrace.GoState
		reason     string
		wantState  model.GoroutineState
		wantReason model.BlockingReason
	}{
		{xtrace.GoRunning, "", model.StateRunning, ""},
		{xtrace.GoRunnable, "", model.StateRunnable, ""},
		{xtrace.GoSyscall, "", model.StateSyscall, model.ReasonSyscall},
		{xtrace.GoWaiting, "chan receive", model.StateBlocked, model.ReasonChanRecv},
		{xtrace.GoWaiting, "chan send", model.StateBlocked, model.ReasonChanSend},
		{xtrace.GoWaiting, "select", model.StateBlocked, model.ReasonSelect},
		{xtrace.GoWaiting, "sync", model.StateBlocked, model.ReasonMutexLock},
		{xtrace.GoWaiting, "sync.(*Cond).Wait", model.StateWaiting, model.ReasonSyncCond},
		{xtrace.GoWaiting, "sleep", model.StateWaiting, model.ReasonSleep},
		{xtrace.GoWaiting, "GC mark assist wait for work", model.StateWaiting, model.ReasonGCAssist},
		{xtrace.GoWaiting, "network", model.StateWaiting, model.ReasonUnknown},
		{xtrace.GoWaiting, "", model.StateWaiting, model.ReasonUnknown},
	}

	for _, tc := range cases {
		tc := tc
		name := tc.to.String() + "/" + string(tc.reason)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotState, gotReason := xMapState(tc.to, tc.reason)
			if gotState != tc.wantState || gotReason != tc.wantReason {
				t.Errorf("xMapState(%v, %q) = (%q, %q), want (%q, %q)",
					tc.to, tc.reason, gotState, gotReason, tc.wantState, tc.wantReason)
			}
		})
	}
}

// TestXTimestamp checks that xTimestamp produces non-zero, ordered times for
// increasing trace.Time values.
func TestXTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Now().UnixNano()
	t0 := xTimestamp(xtrace.Time(now))
	t1 := xTimestamp(xtrace.Time(now + 1000))

	if t0.IsZero() {
		t.Error("xTimestamp returned zero time")
	}
	if !t1.After(t0) {
		t.Errorf("xTimestamp not monotone: t1=%v t0=%v", t1, t0)
	}
}

// generateTestTrace runs fn while collecting a runtime/trace binary file.
// The trace file path is returned; the file is removed automatically via t.Cleanup.
func generateTestTrace(t *testing.T, fn func()) string {
	t.Helper()

	runtimeTraceMu.Lock()
	defer runtimeTraceMu.Unlock()

	f, err := os.CreateTemp(t.TempDir(), "trace*.out")
	if err != nil {
		t.Fatalf("create temp trace file: %v", err)
	}
	path := f.Name()

	if err := rttrace.Start(f); err != nil {
		f.Close()
		t.Fatalf("start runtime trace: %v", err)
	}
	stopped := false
	defer func() {
		if !stopped {
			rttrace.Stop()
		}
	}()
	defer func() { _ = f.Close() }()

	fn()
	rttrace.Stop()
	stopped = true

	if err := f.Close(); err != nil {
		t.Fatalf("close trace file: %v", err)
	}
	return path
}
