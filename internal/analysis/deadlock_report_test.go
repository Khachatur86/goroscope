package analysis

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

// deadlockFixture returns a standard two-goroutine deadlock fixture.
func deadlockFixture() ([]model.ResourceEdge, []model.Goroutine) {
	edges := []model.ResourceEdge{
		{FromGoroutineID: 1, ToGoroutineID: 2, ResourceID: "mutex:0xA"},
		{FromGoroutineID: 2, ToGoroutineID: 1, ResourceID: "mutex:0xB"},
	}
	goroutines := []model.Goroutine{
		{
			ID:         1,
			State:      model.StateBlocked,
			ResourceID: "mutex:0xA",
			LastStack: &model.StackSnapshot{
				GoroutineID: 1,
				Frames: []model.StackFrame{
					{Func: "runtime.lock2", File: "runtime/lock_futex.go", Line: 141},
					{Func: "sync.(*Mutex).Lock", File: "sync/mutex.go", Line: 90},
					{Func: "main.transfer", File: "main.go", Line: 42},
				},
			},
		},
		{
			ID:         2,
			State:      model.StateBlocked,
			ResourceID: "mutex:0xB",
			LastStack: &model.StackSnapshot{
				GoroutineID: 2,
				Frames: []model.StackFrame{
					{Func: "runtime.lock2", File: "runtime/lock_futex.go", Line: 141},
					{Func: "sync.(*Mutex).Lock", File: "sync/mutex.go", Line: 90},
					{Func: "main.transfer", File: "main.go", Line: 55},
				},
			},
		},
	}
	return edges, goroutines
}

// TestBuildDeadlockReportTotal verifies the total count is correct.
func TestBuildDeadlockReportTotal(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	hints := FindDeadlockHints(edges, goroutines)
	report := BuildDeadlockReport(BuildDeadlockReportInput{Hints: hints, Goroutines: goroutines})

	if report.Total != len(hints) {
		t.Errorf("Total = %d; want %d", report.Total, len(hints))
	}
}

// TestBuildDeadlockReportGoroutineAnnotation verifies each cycle contains
// goroutines with state and top frame populated.
func TestBuildDeadlockReportGoroutineAnnotation(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	hints := FindDeadlockHints(edges, goroutines)
	if len(hints) == 0 {
		t.Skip("fixture produced no deadlock hints")
	}
	report := BuildDeadlockReport(BuildDeadlockReportInput{Hints: hints, Goroutines: goroutines})

	cycle := report.Cycles[0]
	for _, dg := range cycle.Goroutines {
		if dg.State == "" {
			t.Errorf("G%d has empty state in report", dg.ID)
		}
		if dg.TopFrame == nil {
			t.Errorf("G%d has nil TopFrame; expected user-space frame", dg.ID)
		} else if !strings.HasPrefix(dg.TopFrame.Func, "main.") {
			t.Errorf("G%d TopFrame.Func = %q; want main.* (runtime frames should be skipped)", dg.ID, dg.TopFrame.Func)
		}
	}
}

// TestDeadlockReportWriteTextContainsBlameChain verifies that text output
// contains the blame chain and goroutine IDs.
func TestDeadlockReportWriteTextContainsBlameChain(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	hints := FindDeadlockHints(edges, goroutines)
	if len(hints) == 0 {
		t.Skip("fixture produced no deadlock hints")
	}
	report := BuildDeadlockReport(BuildDeadlockReportInput{Hints: hints, Goroutines: goroutines})

	var buf bytes.Buffer
	report.WriteText(&buf)
	out := buf.String()

	if !strings.Contains(out, "cycle") && !strings.Contains(out, "Cycle") {
		t.Errorf("text output missing 'cycle' keyword:\n%s", out)
	}
	if !strings.Contains(out, "G1") {
		t.Errorf("text output missing goroutine G1:\n%s", out)
	}
}

// TestDeadlockReportWriteTextNoHints verifies the no-hints message.
func TestDeadlockReportWriteTextNoHints(t *testing.T) {
	t.Parallel()

	report := BuildDeadlockReport(BuildDeadlockReportInput{})

	var buf bytes.Buffer
	report.WriteText(&buf)
	out := buf.String()

	if !strings.Contains(out, "No deadlock") {
		t.Errorf("expected no-deadlock message, got: %q", out)
	}
}

// TestDeadlockReportWriteJSON verifies JSON output is valid and contains expected fields.
func TestDeadlockReportWriteJSON(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	hints := FindDeadlockHints(edges, goroutines)
	report := BuildDeadlockReport(BuildDeadlockReportInput{Hints: hints, Goroutines: goroutines})

	var buf bytes.Buffer
	if err := report.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"total"`) {
		t.Errorf("JSON missing 'total' field:\n%s", out)
	}
	if !strings.Contains(out, `"cycles"`) {
		t.Errorf("JSON missing 'cycles' field:\n%s", out)
	}
}

// TestDeadlockReportWriteGitHubAnnotations verifies GitHub annotation format.
func TestDeadlockReportWriteGitHubAnnotations(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	hints := FindDeadlockHints(edges, goroutines)
	if len(hints) == 0 {
		t.Skip("no hints from fixture")
	}
	report := BuildDeadlockReport(BuildDeadlockReportInput{Hints: hints, Goroutines: goroutines})

	var buf bytes.Buffer
	report.WriteGitHubAnnotations(&buf)
	out := buf.String()

	if !strings.HasPrefix(out, "::warning") {
		t.Errorf("expected GitHub annotation prefix '::warning', got:\n%s", out)
	}
	// Colons in the message must be escaped to %3A to not break annotation syntax.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		// After "::warning file=...::" the message part should not contain raw colons
		// except for the "file=" and "line=" parameter separators before the final "::".
		parts := strings.SplitN(line, "::", 3)
		if len(parts) < 3 {
			continue
		}
		msg := parts[2]
		if strings.Contains(msg, "::") {
			t.Errorf("unescaped '::' in annotation message: %q", line)
		}
	}
}

// TestTopUserFrameSkipsRuntime verifies that runtime frames are correctly skipped.
func TestTopUserFrameSkipsRuntime(t *testing.T) {
	t.Parallel()

	frames := []model.StackFrame{
		{Func: "runtime.gopark", File: "runtime/proc.go", Line: 381},
		{Func: "runtime.chanrecv", File: "runtime/chan.go", Line: 583},
		{Func: "main.producer", File: "main.go", Line: 20},
	}

	top := topUserFrame(frames)
	if top == nil {
		t.Fatal("topUserFrame returned nil; expected main.producer")
	}
	if top.Func != "main.producer" {
		t.Errorf("TopFrame.Func = %q; want main.producer", top.Func)
	}
}

// TestTopUserFrameAllRuntime verifies nil return when all frames are runtime.
func TestTopUserFrameAllRuntime(t *testing.T) {
	t.Parallel()

	frames := []model.StackFrame{
		{Func: "runtime.gopark", File: "runtime/proc.go", Line: 381},
		{Func: "runtime.mcall", File: "runtime/asm_amd64.s", Line: 459},
	}

	if top := topUserFrame(frames); top != nil {
		t.Errorf("expected nil for all-runtime frames, got %+v", top)
	}
}

// TestBuildWaitForGraphNodes verifies that BuildWaitForGraph populates node labels.
func TestBuildWaitForGraphNodes(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	wfg := BuildWaitForGraph(BuildWaitForGraphInput{Edges: edges, Goroutines: goroutines})

	for _, g := range goroutines {
		if _, ok := wfg.Nodes[g.ID]; !ok {
			t.Errorf("G%d missing from WaitForGraph.Nodes", g.ID)
		}
	}
}

// TestBuildWaitForGraphCycles verifies that cycles are detected.
func TestBuildWaitForGraphCycles(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	wfg := BuildWaitForGraph(BuildWaitForGraphInput{Edges: edges, Goroutines: goroutines})

	if len(wfg.Cycles) == 0 {
		t.Errorf("expected at least one cycle in WaitForGraph from deadlock fixture")
	}
}

// TestWriteDOTContainsNodes verifies DOT output contains node declarations.
func TestWriteDOTContainsNodes(t *testing.T) {
	t.Parallel()

	edges, goroutines := deadlockFixture()
	wfg := BuildWaitForGraph(BuildWaitForGraphInput{Edges: edges, Goroutines: goroutines})

	var buf bytes.Buffer
	wfg.WriteDOT(&buf)
	out := buf.String()

	if !strings.Contains(out, "digraph") {
		t.Errorf("DOT output missing 'digraph' keyword:\n%s", out)
	}
	if !strings.Contains(out, "G1") {
		t.Errorf("DOT output missing G1 node:\n%s", out)
	}
	if !strings.Contains(out, "G2") {
		t.Errorf("DOT output missing G2 node:\n%s", out)
	}
}

// TestEscapeAnnotation verifies that special characters are percent-encoded.
func TestEscapeAnnotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"a:b", "a%3Ab"},
		{"a,b", "a%2Cb"},
		{"a\nb", "a%0Ab"},
		{"a%b", "a%25b"},
		{"G1 holds mutex:0xA waiting for G2", "G1 holds mutex%3A0xA waiting for G2"},
	}
	for _, tc := range tests {
		got := escapeAnnotation(tc.input)
		if got != tc.want {
			t.Errorf("escapeAnnotation(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}
