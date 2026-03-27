package cli

import (
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/pprofpoll"
)

func TestRenderTopFrame_Basic(t *testing.T) {
	t.Parallel()
	snaps := []pprofpoll.GoroutineSnapshot{
		{ID: 1, State: model.StateRunning, Frames: []model.StackFrame{{Func: "main.serve"}}},
		{ID: 2, State: model.StateBlocked, Reason: model.ReasonMutexLock, Frames: []model.StackFrame{{Func: "sync.(*Mutex).Lock"}}},
		{ID: 3, State: model.StateWaiting, Reason: model.ReasonChanRecv},
	}

	var sb strings.Builder
	renderTopFrame(topInput{Target: "http://localhost:6060", N: 10, Stdout: &sb}, snaps)
	out := sb.String()

	// Header should mention total count.
	if !strings.Contains(out, "3") {
		t.Errorf("expected goroutine count 3 in header, got:\n%s", out)
	}
	// Blocked goroutine should appear.
	if !strings.Contains(out, "2") {
		t.Errorf("expected goroutine ID 2 in output, got:\n%s", out)
	}
	// Frame should appear.
	if !strings.Contains(out, "main.serve") {
		t.Errorf("expected top frame in output, got:\n%s", out)
	}
}

func TestRenderTopFrame_BlockedFirst(t *testing.T) {
	t.Parallel()
	// Blocked goroutines should appear before running ones.
	snaps := []pprofpoll.GoroutineSnapshot{
		{ID: 10, State: model.StateRunning},
		{ID: 5, State: model.StateBlocked},
		{ID: 7, State: model.StateWaiting},
	}

	var sb strings.Builder
	renderTopFrame(topInput{Target: "x", N: 10, Stdout: &sb}, snaps)
	out := sb.String()

	idxBlocked := strings.Index(out, "5")
	idxWaiting := strings.Index(out, "7")
	idxRunning := strings.Index(out, "10")

	if idxBlocked == -1 || idxWaiting == -1 || idxRunning == -1 {
		t.Fatalf("some goroutine IDs missing from output:\n%s", out)
	}
	if idxRunning < idxBlocked {
		t.Errorf("expected blocked goroutines before running; running appeared at pos %d, blocked at %d", idxRunning, idxBlocked)
	}
}

func TestRenderTopFrame_TruncatesRows(t *testing.T) {
	t.Parallel()
	snaps := make([]pprofpoll.GoroutineSnapshot, 10)
	for i := range snaps {
		snaps[i] = pprofpoll.GoroutineSnapshot{ID: int64(i + 1), State: model.StateRunning}
	}

	var sb strings.Builder
	renderTopFrame(topInput{Target: "x", N: 3, Stdout: &sb}, snaps)
	out := sb.String()

	if !strings.Contains(out, "more goroutines") {
		t.Errorf("expected '... more goroutines' truncation message, got:\n%s", out)
	}
}

func TestRenderTopFrame_LongFrameTruncated(t *testing.T) {
	t.Parallel()
	longFunc := strings.Repeat("x", 80)
	snaps := []pprofpoll.GoroutineSnapshot{
		{ID: 1, State: model.StateRunning, Frames: []model.StackFrame{{Func: longFunc}}},
	}

	var sb strings.Builder
	renderTopFrame(topInput{Target: "x", N: 10, Stdout: &sb}, snaps)
	out := sb.String()

	// The full long string should not appear — it should be truncated.
	if strings.Contains(out, longFunc) {
		t.Errorf("expected long frame to be truncated, but full frame appears in output")
	}
}

func TestTopCommand_MissingTarget(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	err := topCommand(t.Context(), []string{}, &strings.Builder{}, &stderr)
	if err == nil {
		t.Fatal("expected error for missing target, got nil")
	}
}

func TestBuildTopRows_Ordering(t *testing.T) {
	t.Parallel()
	snaps := []pprofpoll.GoroutineSnapshot{
		{ID: 1, State: model.StateRunning},
		{ID: 2, State: model.StateBlocked},
		{ID: 3, State: model.StateWaiting},
		{ID: 4, State: model.StateRunnable},
	}
	rows := buildTopRows(snaps, 10)

	// First two should be blocked/waiting.
	for i := 0; i < 2; i++ {
		if !isBlockedState(rows[i].State) {
			t.Errorf("row %d: expected blocked/waiting, got %q", i, rows[i].State)
		}
	}
	// Last two should be non-blocked.
	for i := 2; i < 4; i++ {
		if isBlockedState(rows[i].State) {
			t.Errorf("row %d: expected non-blocked, got %q", i, rows[i].State)
		}
	}
}
