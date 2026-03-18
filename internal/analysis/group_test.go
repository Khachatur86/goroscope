package analysis

import (
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestGroupGoroutines_ByFunction(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{
			ID:    1,
			State: model.StateRunning,
			LastStack: &model.StackSnapshot{
				Frames: []model.StackFrame{
					{Func: "runtime.goexit"},
					{Func: "github.com/example/server.(*Handler).ServeHTTP"},
				},
			},
		},
		{
			ID:    2,
			State: model.StateBlocked,
			LastStack: &model.StackSnapshot{
				Frames: []model.StackFrame{
					{Func: "runtime.goexit"},
					{Func: "github.com/example/server.(*Handler).ServeHTTP"},
				},
			},
		},
		{
			ID:    3,
			State: model.StateRunnable,
			LastStack: &model.StackSnapshot{
				Frames: []model.StackFrame{
					{Func: "runtime.goexit"},
					{Func: "github.com/example/worker.Process"},
				},
			},
		},
	}

	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: goroutines,
		By:         GroupByFunction,
	})
	if err != nil {
		t.Fatalf("GroupGoroutines: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// First group (highest count) should be ServeHTTP with 2 goroutines.
	if groups[0].Count != 2 {
		t.Errorf("expected first group count=2, got %d", groups[0].Count)
	}
	if groups[0].Key != "(*Handler).ServeHTTP" {
		t.Errorf("expected first group key=(*Handler).ServeHTTP, got %q", groups[0].Key)
	}
	if len(groups[0].GoroutineIDs) != 2 {
		t.Errorf("expected 2 goroutine IDs in first group, got %d", len(groups[0].GoroutineIDs))
	}

	// Second group: worker.Process
	if groups[1].Key != "worker.Process" {
		t.Errorf("expected second group key=worker.Process, got %q", groups[1].Key)
	}
}

func TestGroupGoroutines_ByPackage(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{
			ID:    1,
			State: model.StateRunning,
			LastStack: &model.StackSnapshot{
				Frames: []model.StackFrame{
					{Func: "runtime.goexit"},
					{Func: "github.com/example/server.HandleRequest"},
				},
			},
		},
		{
			ID:    2,
			State: model.StateRunning,
			LastStack: &model.StackSnapshot{
				Frames: []model.StackFrame{
					{Func: "runtime.goexit"},
					{Func: "github.com/example/server.HandleStream"},
				},
			},
		},
		{
			ID:    3,
			State: model.StateBlocked,
			LastStack: &model.StackSnapshot{
				Frames: []model.StackFrame{
					{Func: "runtime.goexit"},
					{Func: "github.com/example/db.Query"},
				},
			},
		},
	}

	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: goroutines,
		By:         GroupByPackage,
	})
	if err != nil {
		t.Fatalf("GroupGoroutines: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// server package has 2 goroutines.
	if groups[0].Count != 2 {
		t.Errorf("expected first group count=2, got %d", groups[0].Count)
	}
	wantPkg := "github.com/example/server"
	if groups[0].Key != wantPkg {
		t.Errorf("expected first group key=%q, got %q", wantPkg, groups[0].Key)
	}
}

func TestGroupGoroutines_ByParentID(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 2, ParentID: 1, State: model.StateRunning},
		{ID: 3, ParentID: 1, State: model.StateBlocked},
		{ID: 4, ParentID: 2, State: model.StateRunnable},
		{ID: 5, ParentID: 0, State: model.StateRunning},
	}

	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: goroutines,
		By:         GroupByParentID,
	})
	if err != nil {
		t.Fatalf("GroupGoroutines: %v", err)
	}

	// Expect: parent 1 → 2 goroutines, parent 2 → 1, root → 1
	found := make(map[string]int)
	for _, g := range groups {
		found[g.Key] = g.Count
	}
	if found["1"] != 2 {
		t.Errorf("expected 2 goroutines for parent 1, got %d", found["1"])
	}
	if found["2"] != 1 {
		t.Errorf("expected 1 goroutine for parent 2, got %d", found["2"])
	}
	if found["(root)"] != 1 {
		t.Errorf("expected 1 root goroutine, got %d", found["(root)"])
	}
}

func TestGroupGoroutines_ByLabel(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning, Labels: map[string]string{"request_id": "req-abc"}},
		{ID: 2, State: model.StateBlocked, Labels: map[string]string{"request_id": "req-abc"}},
		{ID: 3, State: model.StateRunning, Labels: map[string]string{"request_id": "req-xyz"}},
		{ID: 4, State: model.StateRunning}, // no label
	}

	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: goroutines,
		By:         GroupByLabel,
		LabelKey:   "request_id",
	})
	if err != nil {
		t.Fatalf("GroupGoroutines: %v", err)
	}

	found := make(map[string]int)
	for _, g := range groups {
		found[g.Key] = g.Count
	}
	if found["req-abc"] != 2 {
		t.Errorf("expected 2 goroutines for req-abc, got %d", found["req-abc"])
	}
	if found["req-xyz"] != 1 {
		t.Errorf("expected 1 goroutine for req-xyz, got %d", found["req-xyz"])
	}
	if found["(no label)"] != 1 {
		t.Errorf("expected 1 no-label goroutine, got %d", found["(no label)"])
	}
}

func TestGroupGoroutines_WaitAndCPUMetrics(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, WaitNS: 1000, Labels: map[string]string{"svc": "api"}},
		{ID: 2, State: model.StateBlocked, WaitNS: 3000, Labels: map[string]string{"svc": "api"}},
	}
	segments := []model.TimelineSegment{
		{GoroutineID: 1, State: model.StateRunning, StartNS: 0, EndNS: 500},
		{GoroutineID: 2, State: model.StateRunning, StartNS: 0, EndNS: 1500},
		// A non-RUNNING segment — should not count toward CPU.
		{GoroutineID: 1, State: model.StateBlocked, StartNS: 500, EndNS: 1500},
	}

	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: goroutines,
		Segments:   segments,
		By:         GroupByLabel,
		LabelKey:   "svc",
	})
	if err != nil {
		t.Fatalf("GroupGoroutines: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.TotalWaitNS != 4000 {
		t.Errorf("expected TotalWaitNS=4000, got %d", g.TotalWaitNS)
	}
	if g.MaxWaitNS != 3000 {
		t.Errorf("expected MaxWaitNS=3000, got %d", g.MaxWaitNS)
	}
	if g.AvgWaitNS != 2000 {
		t.Errorf("expected AvgWaitNS=2000, got %f", g.AvgWaitNS)
	}
	if g.TotalCPUNS != 2000 {
		t.Errorf("expected TotalCPUNS=2000 (500+1500), got %d", g.TotalCPUNS)
	}
}

func TestGroupGoroutines_InvalidBy(t *testing.T) {
	t.Parallel()

	_, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: []model.Goroutine{{ID: 1}},
		By:         GroupByField("unknown_dimension"),
	})
	if err == nil {
		t.Fatal("expected error for invalid GroupByField, got nil")
	}
}

func TestGroupGoroutines_NoStack_FallbackToUnknown(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning}, // no LastStack
		{ID: 2, State: model.StateBlocked}, // no LastStack
	}

	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: goroutines,
		By:         GroupByFunction,
	})
	if err != nil {
		t.Fatalf("GroupGoroutines: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group (unknown), got %d", len(groups))
	}
	if groups[0].Key != "(unknown)" {
		t.Errorf("expected key=(unknown), got %q", groups[0].Key)
	}
	if groups[0].Count != 2 {
		t.Errorf("expected count=2, got %d", groups[0].Count)
	}
}

func TestGroupGoroutines_Empty(t *testing.T) {
	t.Parallel()

	groups, err := GroupGoroutines(GroupGoroutinesInput{
		Goroutines: nil,
		By:         GroupByFunction,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected empty groups, got %d", len(groups))
	}
}

func TestIsUserFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fn   string
		want bool
	}{
		{"runtime.goexit", false},
		{"runtime/trace.Start", false},
		{"syscall.Read", false},
		{"sync.(*Mutex).Lock", false},
		{"internal/poll.Read", false},
		{"github.com/example/myapp.Handler", true},
		{"main.main", true},
		{"", false},
	}
	for _, tc := range tests {
		if got := isUserFrame(tc.fn); got != tc.want {
			t.Errorf("isUserFrame(%q) = %v, want %v", tc.fn, got, tc.want)
		}
	}
}

func TestPackageFromFunc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fn   string
		want string
	}{
		{"github.com/foo/bar.(*Baz).Run", "github.com/foo/bar"},
		{"github.com/foo/bar.Run", "github.com/foo/bar"},
		{"main.main", "main"},
		{"worker.Process", "worker"},
	}
	for _, tc := range tests {
		if got := packageFromFunc(tc.fn); got != tc.want {
			t.Errorf("packageFromFunc(%q) = %q, want %q", tc.fn, got, tc.want)
		}
	}
}

func TestShortFuncName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fn   string
		want string
	}{
		{"github.com/foo/bar.(*Baz).Run", "(*Baz).Run"},
		{"github.com/foo/bar.Run", "bar.Run"},
		{"main.main", "main.main"},
	}
	for _, tc := range tests {
		if got := shortFuncName(tc.fn); got != tc.want {
			t.Errorf("shortFuncName(%q) = %q, want %q", tc.fn, got, tc.want)
		}
	}
}
