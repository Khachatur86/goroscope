package analysis

import (
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func makeGoroutineWithStack(id int64, state model.GoroutineState, frames []string) model.Goroutine {
	snapshotFrames := make([]model.StackFrame, len(frames))
	for i, f := range frames {
		snapshotFrames[i] = model.StackFrame{Func: f}
	}
	snap := &model.StackSnapshot{Frames: snapshotFrames}
	return model.Goroutine{ID: id, State: state, LastStack: snap}
}

func TestBuildFlamegraph_Empty(t *testing.T) {
	t.Parallel()
	result := BuildFlamegraph(FlamegraphInput{})
	if result.TotalGoroutines != 0 {
		t.Fatalf("want 0 goroutines, got %d", result.TotalGoroutines)
	}
	if result.Root.Value != 0 {
		t.Fatalf("want root value 0, got %d", result.Root.Value)
	}
}

func TestBuildFlamegraph_NoStack(t *testing.T) {
	t.Parallel()
	g := model.Goroutine{ID: 1, State: model.StateRunning}
	result := BuildFlamegraph(FlamegraphInput{Goroutines: []model.Goroutine{g}})
	if result.TotalGoroutines != 1 {
		t.Fatalf("want 1 goroutine, got %d", result.TotalGoroutines)
	}
	if result.Root.Value != 1 {
		t.Fatalf("want root value 1, got %d", result.Root.Value)
	}
	if len(result.Root.Children) != 0 {
		t.Fatalf("want no children for goroutine without stack, got %d", len(result.Root.Children))
	}
}

func TestBuildFlamegraph_SingleGoroutine(t *testing.T) {
	t.Parallel()
	// frames are innermost-first: ["leaf", "middle", "root_fn"]
	// root→leaf traversal should be: root_fn → middle → leaf
	g := makeGoroutineWithStack(1, model.StateRunning, []string{"leaf", "middle", "root_fn"})
	result := BuildFlamegraph(FlamegraphInput{Goroutines: []model.Goroutine{g}})

	if result.TotalGoroutines != 1 {
		t.Fatalf("want 1 goroutine, got %d", result.TotalGoroutines)
	}
	if result.Root.Value != 1 {
		t.Fatalf("want root value 1, got %d", result.Root.Value)
	}
	if len(result.Root.Children) != 1 || result.Root.Children[0].Name != "root_fn" {
		t.Fatalf("want root child 'root_fn', got %v", result.Root.Children)
	}
	mid := result.Root.Children[0].Children
	if len(mid) != 1 || mid[0].Name != "middle" {
		t.Fatalf("want middle child 'middle', got %v", mid)
	}
	leaf := mid[0].Children
	if len(leaf) != 1 || leaf[0].Name != "leaf" {
		t.Fatalf("want leaf 'leaf', got %v", leaf)
	}
}

func TestBuildFlamegraph_SharedPath(t *testing.T) {
	t.Parallel()
	// Two goroutines sharing the same call path: value should accumulate.
	g1 := makeGoroutineWithStack(1, model.StateRunning, []string{"leaf", "common"})
	g2 := makeGoroutineWithStack(2, model.StateRunning, []string{"leaf", "common"})
	result := BuildFlamegraph(FlamegraphInput{Goroutines: []model.Goroutine{g1, g2}})

	if result.TotalGoroutines != 2 {
		t.Fatalf("want 2, got %d", result.TotalGoroutines)
	}
	if len(result.Root.Children) != 1 {
		t.Fatalf("want 1 child of root, got %d", len(result.Root.Children))
	}
	common := result.Root.Children[0]
	if common.Name != "common" || common.Value != 2 {
		t.Fatalf("want common value 2, got %s=%d", common.Name, common.Value)
	}
}

func TestBuildFlamegraph_StateFilter(t *testing.T) {
	t.Parallel()
	g1 := makeGoroutineWithStack(1, model.StateRunning, []string{"fn"})
	g2 := makeGoroutineWithStack(2, model.StateBlocked, []string{"fn"})
	result := BuildFlamegraph(FlamegraphInput{
		Goroutines:  []model.Goroutine{g1, g2},
		StateFilter: string(model.StateBlocked),
	})
	if result.TotalGoroutines != 1 {
		t.Fatalf("want 1 filtered goroutine, got %d", result.TotalGoroutines)
	}
	if result.StateFilter != string(model.StateBlocked) {
		t.Fatalf("want state_filter echoed, got %q", result.StateFilter)
	}
}

func TestBuildFlamegraph_MaxDepth(t *testing.T) {
	t.Parallel()
	// 4 frames deep, limit to 2
	g := makeGoroutineWithStack(1, model.StateRunning, []string{"d", "c", "b", "a"})
	result := BuildFlamegraph(FlamegraphInput{
		Goroutines: []model.Goroutine{g},
		MaxDepth:   2,
	})
	// Should have at most 2 levels of children under root
	depth := 0
	cur := &result.Root
	for len(cur.Children) > 0 {
		cur = cur.Children[0]
		depth++
	}
	if depth != 2 {
		t.Fatalf("want depth 2, got %d", depth)
	}
}

func TestBuildFlamegraph_SortedByValue(t *testing.T) {
	t.Parallel()
	// g1, g2 share "common"; g3 uses "rare"
	g1 := makeGoroutineWithStack(1, model.StateRunning, []string{"common"})
	g2 := makeGoroutineWithStack(2, model.StateRunning, []string{"common"})
	g3 := makeGoroutineWithStack(3, model.StateRunning, []string{"rare"})
	result := BuildFlamegraph(FlamegraphInput{Goroutines: []model.Goroutine{g1, g2, g3}})

	if len(result.Root.Children) < 2 {
		t.Fatalf("want 2 root children, got %d", len(result.Root.Children))
	}
	if result.Root.Children[0].Name != "common" {
		t.Fatalf("want 'common' first (highest value), got %q", result.Root.Children[0].Name)
	}
}

func TestFoldedStacks_Basic(t *testing.T) {
	t.Parallel()
	g1 := makeGoroutineWithStack(1, model.StateRunning, []string{"leaf", "mid", "root_fn"})
	g2 := makeGoroutineWithStack(2, model.StateRunning, []string{"leaf", "mid", "root_fn"})
	out := FoldedStacks(FlamegraphInput{Goroutines: []model.Goroutine{g1, g2}})

	// Should contain "root;root_fn;mid;leaf 2"
	if !strings.Contains(out, "root;root_fn;mid;leaf 2") {
		t.Fatalf("expected aggregated line, got:\n%s", out)
	}
}

func TestFoldedStacks_NoStack(t *testing.T) {
	t.Parallel()
	g := model.Goroutine{ID: 1, State: model.StateRunning}
	out := FoldedStacks(FlamegraphInput{Goroutines: []model.Goroutine{g}})
	if !strings.Contains(out, "root 1") {
		t.Fatalf("want 'root 1' for goroutine with no stack, got:\n%s", out)
	}
}

func TestFoldedStacks_StateFilter(t *testing.T) {
	t.Parallel()
	g1 := makeGoroutineWithStack(1, model.StateRunning, []string{"fn"})
	g2 := makeGoroutineWithStack(2, model.StateBlocked, []string{"fn"})
	out := FoldedStacks(FlamegraphInput{
		Goroutines:  []model.Goroutine{g1, g2},
		StateFilter: string(model.StateBlocked),
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line (blocked only), got %d lines:\n%s", len(lines), out)
	}
	if !strings.HasSuffix(lines[0], " 1") {
		t.Fatalf("want count 1, got: %s", lines[0])
	}
}

func TestItoa(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{1000, "1000"},
	}
	for _, tc := range cases {
		if got := itoa(tc.n); got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
