package analysis_test

import (
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
)

func makeSnap(goroutineID int64, funcs ...string) model.StackSnapshot {
	frames := make([]model.StackFrame, len(funcs))
	for i, f := range funcs {
		frames[i] = model.StackFrame{Func: f, File: "file.go", Line: i + 1}
	}
	return model.StackSnapshot{GoroutineID: goroutineID, Frames: frames}
}

func TestStackPatternDiff_Empty(t *testing.T) {
	t.Parallel()
	result := analysis.StackPatternDiff(model.Capture{}, model.Capture{})
	if len(result.Appeared) != 0 || len(result.Disappeared) != 0 || result.CommonCount != 0 {
		t.Errorf("expected all-zero result for empty captures, got %+v", result)
	}
}

func TestStackPatternDiff_AllNew(t *testing.T) {
	t.Parallel()

	baseline := model.Capture{}
	compare := model.Capture{
		Stacks: []model.StackSnapshot{
			makeSnap(1, "main.worker", "sync.(*Mutex).Lock"),
			makeSnap(2, "main.reader", "net.(*Conn).Read"),
		},
	}

	result := analysis.StackPatternDiff(baseline, compare)

	if len(result.Appeared) != 2 {
		t.Fatalf("appeared: got %d, want 2", len(result.Appeared))
	}
	if len(result.Disappeared) != 0 {
		t.Fatalf("disappeared: got %d, want 0", len(result.Disappeared))
	}
	if result.CommonCount != 0 {
		t.Errorf("common: got %d, want 0", result.CommonCount)
	}
}

func TestStackPatternDiff_AllGone(t *testing.T) {
	t.Parallel()

	baseline := model.Capture{
		Stacks: []model.StackSnapshot{
			makeSnap(1, "main.worker", "sync.(*Mutex).Lock"),
		},
	}
	compare := model.Capture{}

	result := analysis.StackPatternDiff(baseline, compare)

	if len(result.Appeared) != 0 {
		t.Fatalf("appeared: got %d, want 0", len(result.Appeared))
	}
	if len(result.Disappeared) != 1 {
		t.Fatalf("disappeared: got %d, want 1", len(result.Disappeared))
	}
}

func TestStackPatternDiff_CommonAndDiff(t *testing.T) {
	t.Parallel()

	sharedStack := []string{"main.worker", "sync.(*Mutex).Lock"}
	onlyInBaseline := []string{"main.oldFunc", "runtime.gopark"}
	onlyInCompare := []string{"main.newFunc", "net.(*Conn).Read"}

	baseline := model.Capture{
		Stacks: []model.StackSnapshot{
			makeSnap(1, sharedStack...),
			makeSnap(2, onlyInBaseline...),
		},
	}
	compare := model.Capture{
		Stacks: []model.StackSnapshot{
			makeSnap(10, sharedStack...),
			makeSnap(11, onlyInCompare...),
		},
	}

	result := analysis.StackPatternDiff(baseline, compare)

	if result.CommonCount != 1 {
		t.Errorf("common: got %d, want 1", result.CommonCount)
	}
	if len(result.Appeared) != 1 {
		t.Fatalf("appeared: got %d, want 1", len(result.Appeared))
	}
	if !strings.Contains(result.Appeared[0].Signature, "newFunc") {
		t.Errorf("appeared signature should contain newFunc, got %q", result.Appeared[0].Signature)
	}
	if len(result.Disappeared) != 1 {
		t.Fatalf("disappeared: got %d, want 1", len(result.Disappeared))
	}
	if !strings.Contains(result.Disappeared[0].Signature, "oldFunc") {
		t.Errorf("disappeared signature should contain oldFunc, got %q", result.Disappeared[0].Signature)
	}
}

func TestStackPatternDiff_LineNumbersIgnored(t *testing.T) {
	t.Parallel()

	// Same functions, different line numbers → same signature.
	snap1 := model.StackSnapshot{
		GoroutineID: 1,
		Frames: []model.StackFrame{
			{Func: "main.worker", File: "worker.go", Line: 42},
		},
	}
	snap2 := model.StackSnapshot{
		GoroutineID: 2,
		Frames: []model.StackFrame{
			{Func: "main.worker", File: "worker.go", Line: 99}, // different line
		},
	}

	baseline := model.Capture{Stacks: []model.StackSnapshot{snap1}}
	compare := model.Capture{Stacks: []model.StackSnapshot{snap2}}

	result := analysis.StackPatternDiff(baseline, compare)
	if result.CommonCount != 1 {
		t.Errorf("same func different line: expected common=1, got %d", result.CommonCount)
	}
	if len(result.Appeared) != 0 || len(result.Disappeared) != 0 {
		t.Errorf("expected no diff for same func different line, got %+v", result)
	}
}

func TestStackPatternDiff_CountAggregation(t *testing.T) {
	t.Parallel()

	// Three goroutines with the same stack in compare → count=3.
	compare := model.Capture{
		Stacks: []model.StackSnapshot{
			makeSnap(1, "main.worker"),
			makeSnap(2, "main.worker"),
			makeSnap(3, "main.worker"),
		},
	}

	result := analysis.StackPatternDiff(model.Capture{}, compare)
	if len(result.Appeared) != 1 {
		t.Fatalf("appeared: got %d, want 1", len(result.Appeared))
	}
	if result.Appeared[0].Count != 3 {
		t.Errorf("count: got %d, want 3", result.Appeared[0].Count)
	}
}

func TestStackPatternDiff_SortedByCountDesc(t *testing.T) {
	t.Parallel()

	compare := model.Capture{
		Stacks: []model.StackSnapshot{
			makeSnap(1, "main.rare"),
			makeSnap(2, "main.common"),
			makeSnap(3, "main.common"),
			makeSnap(4, "main.common"),
		},
	}

	result := analysis.StackPatternDiff(model.Capture{}, compare)
	if len(result.Appeared) < 2 {
		t.Fatalf("expected at least 2 appeared, got %d", len(result.Appeared))
	}
	if result.Appeared[0].Count < result.Appeared[1].Count {
		t.Errorf("appeared should be sorted by count desc: [0].Count=%d [1].Count=%d",
			result.Appeared[0].Count, result.Appeared[1].Count)
	}
}
