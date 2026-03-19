package analysis

import (
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestGenerateInsights_Deadlock(t *testing.T) {
	t.Parallel()

	// Two goroutines blocked on each other's resources.
	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, ResourceID: "mutex:0x01"},
		{ID: 2, State: model.StateBlocked, ResourceID: "mutex:0x02"},
	}
	edges := []model.ResourceEdge{
		{FromGoroutineID: 1, ToGoroutineID: 2, ResourceID: "mutex:0x01"},
		{FromGoroutineID: 2, ToGoroutineID: 1, ResourceID: "mutex:0x02"},
	}

	insights := GenerateInsights(GenerateInsightsInput{
		Goroutines: goroutines,
		Edges:      edges,
	})

	if len(insights) == 0 {
		t.Fatal("expected at least one insight for a deadlock cycle")
	}
	first := insights[0]
	if first.Kind != InsightKindDeadlock {
		t.Errorf("expected kind=deadlock, got %q", first.Kind)
	}
	if first.Severity != SeverityCritical {
		t.Errorf("expected severity=critical, got %q", first.Severity)
	}
	if first.Score < 99 {
		t.Errorf("expected score ~100, got %f", first.Score)
	}
	if !strings.Contains(first.Title, "deadlock") && !strings.Contains(first.Title, "Deadlock") {
		t.Errorf("expected 'deadlock' in title, got %q", first.Title)
	}
	if len(first.GoroutineIDs) == 0 {
		t.Error("expected goroutine IDs in deadlock insight")
	}
}

func TestGenerateInsights_Leak(t *testing.T) {
	t.Parallel()

	const leakNS = int64(5_000_000_000) // 5s threshold for the test

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, WaitNS: leakNS + 1,
			LastStack: &model.StackSnapshot{Frames: []model.StackFrame{
				{Func: "runtime.goexit"},
				{Func: "github.com/example/worker.(*Pool).wait"},
			}}},
		{ID: 2, State: model.StateWaiting, WaitNS: leakNS + 1000,
			LastStack: &model.StackSnapshot{Frames: []model.StackFrame{
				{Func: "runtime.goexit"},
				{Func: "github.com/example/worker.(*Pool).wait"},
			}}},
		{ID: 3, State: model.StateBlocked, WaitNS: 1000}, // below threshold, not a leak
	}

	insights := GenerateInsights(GenerateInsightsInput{
		Goroutines:      goroutines,
		LeakThresholdNS: leakNS,
	})

	var leakInsight *Insight
	for i := range insights {
		if insights[i].Kind == InsightKindLeak {
			leakInsight = &insights[i]
			break
		}
	}
	if leakInsight == nil {
		t.Fatal("expected a leak insight")
	}
	if leakInsight.Severity != SeverityCritical {
		t.Errorf("expected severity=critical for leak, got %q", leakInsight.Severity)
	}
	if len(leakInsight.GoroutineIDs) == 0 {
		t.Error("expected goroutine IDs in leak insight")
	}
	if !strings.Contains(leakInsight.Title, "2") {
		t.Errorf("expected count 2 in leak title, got %q", leakInsight.Title)
	}
}

func TestGenerateInsights_Contention(t *testing.T) {
	t.Parallel()

	// 5 goroutines all blocked on the same mutex for various durations.
	segments := []model.TimelineSegment{
		{GoroutineID: 1, State: model.StateBlocked, ResourceID: "mutex:0xABCD", StartNS: 0, EndNS: 100_000_000},
		{GoroutineID: 2, State: model.StateBlocked, ResourceID: "mutex:0xABCD", StartNS: 5_000_000, EndNS: 95_000_000},
		{GoroutineID: 3, State: model.StateBlocked, ResourceID: "mutex:0xABCD", StartNS: 10_000_000, EndNS: 80_000_000},
		{GoroutineID: 4, State: model.StateBlocked, ResourceID: "mutex:0xABCD", StartNS: 15_000_000, EndNS: 75_000_000},
		{GoroutineID: 5, State: model.StateBlocked, ResourceID: "mutex:0xABCD", StartNS: 20_000_000, EndNS: 70_000_000},
	}

	insights := GenerateInsights(GenerateInsightsInput{
		Segments:          segments,
		ContentionMinPeak: 4, // peak=5 exceeds this threshold
	})

	var contentionInsight *Insight
	for i := range insights {
		if insights[i].Kind == InsightKindContention {
			contentionInsight = &insights[i]
			break
		}
	}
	if contentionInsight == nil {
		t.Fatal("expected a contention insight")
	}
	if contentionInsight.Severity != SeverityWarning {
		t.Errorf("expected severity=warning for contention, got %q", contentionInsight.Severity)
	}
	if !strings.Contains(contentionInsight.Title, "mutex:0xABCD") {
		t.Errorf("expected resource ID in contention title, got %q", contentionInsight.Title)
	}
	if len(contentionInsight.ResourceIDs) == 0 {
		t.Error("expected resource IDs in contention insight")
	}
}

func TestGenerateInsights_Blocking(t *testing.T) {
	t.Parallel()

	const blockNS = int64(500_000_000)  // 0.5s threshold
	const leakNS = int64(5_000_000_000) // 5s

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, WaitNS: blockNS + 100}, // blocked, not leak
		{ID: 2, State: model.StateWaiting, WaitNS: blockNS + 200}, // waiting, not leak
		{ID: 3, State: model.StateRunning, WaitNS: blockNS + 300}, // running — should not appear
		{ID: 4, State: model.StateBlocked, WaitNS: leakNS + 1},    // leak — excluded from blocking
	}

	insights := GenerateInsights(GenerateInsightsInput{
		Goroutines:           goroutines,
		LongBlockThresholdNS: blockNS,
		LeakThresholdNS:      leakNS,
	})

	var blockingInsight *Insight
	for i := range insights {
		if insights[i].Kind == InsightKindBlocking {
			blockingInsight = &insights[i]
			break
		}
	}
	if blockingInsight == nil {
		t.Fatal("expected a blocking insight")
	}
	if blockingInsight.Severity != SeverityWarning {
		t.Errorf("expected severity=warning for blocking, got %q", blockingInsight.Severity)
	}
	// Only 2 goroutines qualify (IDs 1 and 2; 3 is RUNNING, 4 is a leak).
	if len(blockingInsight.GoroutineIDs) != 2 {
		t.Errorf("expected 2 goroutine IDs in blocking insight, got %d", len(blockingInsight.GoroutineIDs))
	}
}

func TestGenerateInsights_GoroutineCount(t *testing.T) {
	t.Parallel()

	goroutines := make([]model.Goroutine, 1500)
	for i := range goroutines {
		goroutines[i] = model.Goroutine{ID: int64(i + 1), State: model.StateRunning}
	}

	insights := GenerateInsights(GenerateInsightsInput{
		Goroutines:        goroutines,
		GoroutineCountMin: 1000,
	})

	var gcInsight *Insight
	for i := range insights {
		if insights[i].Kind == InsightKindGoroutines {
			gcInsight = &insights[i]
			break
		}
	}
	if gcInsight == nil {
		t.Fatal("expected a goroutine-count insight")
	}
	if gcInsight.Severity != SeverityInfo {
		t.Errorf("expected severity=info for goroutine count, got %q", gcInsight.Severity)
	}
	if !strings.Contains(gcInsight.Title, "1500") {
		t.Errorf("expected 1500 in goroutine count title, got %q", gcInsight.Title)
	}
}

func TestGenerateInsights_Empty(t *testing.T) {
	t.Parallel()

	insights := GenerateInsights(GenerateInsightsInput{})
	if len(insights) != 0 {
		t.Errorf("expected no insights for empty input, got %d", len(insights))
	}
}

func TestGenerateInsights_SortedByScore(t *testing.T) {
	t.Parallel()

	// A mix of leak + contention + blocking to verify ordering.
	const leakNS = int64(1_000_000)
	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, WaitNS: leakNS + 1, ResourceID: "mutex:0x1"},
		{ID: 2, State: model.StateBlocked, WaitNS: leakNS + 2, ResourceID: "mutex:0x1"},
	}
	segments := []model.TimelineSegment{
		{GoroutineID: 1, State: model.StateBlocked, ResourceID: "mutex:0x1", StartNS: 0, EndNS: 1_000_000},
		{GoroutineID: 2, State: model.StateBlocked, ResourceID: "mutex:0x1", StartNS: 0, EndNS: 1_000_000},
		{GoroutineID: 3, State: model.StateBlocked, ResourceID: "mutex:0x1", StartNS: 0, EndNS: 1_000_000},
		{GoroutineID: 4, State: model.StateBlocked, ResourceID: "mutex:0x1", StartNS: 0, EndNS: 1_000_000},
		{GoroutineID: 5, State: model.StateBlocked, ResourceID: "mutex:0x1", StartNS: 0, EndNS: 1_000_000},
	}

	insights := GenerateInsights(GenerateInsightsInput{
		Goroutines:        goroutines,
		Segments:          segments,
		LeakThresholdNS:   leakNS,
		ContentionMinPeak: 4,
	})

	for i := 1; i < len(insights); i++ {
		if insights[i].Score > insights[i-1].Score {
			t.Errorf("insights not sorted: [%d].Score=%f > [%d].Score=%f",
				i, insights[i].Score, i-1, insights[i-1].Score)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ns   int64
		want string
	}{
		{0, "0ns"},
		{500, "500ns"},
		{1_500, "1.5µs"},
		{1_500_000, "1.5ms"},
		{1_000_000_000, "1.00s"},
		{30_000_000_000, "30.00s"},
	}
	for _, tc := range tests {
		if got := fmtDuration(tc.ns); got != tc.want {
			t.Errorf("fmtDuration(%d) = %q, want %q", tc.ns, got, tc.want)
		}
	}
}
