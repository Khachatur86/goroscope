package analysis

import (
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestComputeResourceContention(t *testing.T) {
	t.Parallel()

	segments := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 100, ResourceID: "chan:0x1"},
		{GoroutineID: 2, StartNS: 50, EndNS: 150, ResourceID: "chan:0x1"},
		{GoroutineID: 3, StartNS: 75, EndNS: 125, ResourceID: "chan:0x1"},
		{GoroutineID: 4, StartNS: 200, EndNS: 300, ResourceID: "mutex:0x2"},
	}

	out := ComputeResourceContention(segments)

	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 resources", len(out))
	}

	// chan:0x1: peak = 3 (all three overlap at 75-100), 3 segments, total 100+100+50=250, avg 250/3
	var chanRes, mutexRes *ResourceContention
	for i := range out {
		switch out[i].ResourceID {
		case "chan:0x1":
			chanRes = &out[i]
		case "mutex:0x2":
			mutexRes = &out[i]
		}
	}
	if chanRes == nil || mutexRes == nil {
		t.Fatalf("expected both resources, got %v", out)
	}
	if chanRes.PeakWaiters != 3 {
		t.Errorf("chan peak_waiters = %d, want 3", chanRes.PeakWaiters)
	}
	if chanRes.SegmentCount != 3 {
		t.Errorf("chan segment_count = %d, want 3", chanRes.SegmentCount)
	}
	if mutexRes.PeakWaiters != 1 {
		t.Errorf("mutex peak_waiters = %d, want 1", mutexRes.PeakWaiters)
	}
}

func TestPeakConcurrent(t *testing.T) {
	t.Parallel()

	segs := []segBounds{
		{0, 100},
		{50, 150},
		{75, 125},
	}
	if got := peakConcurrent(segs); got != 3 {
		t.Errorf("peakConcurrent = %d, want 3", got)
	}
}

func TestComputeResourceContention_Empty(t *testing.T) {
	t.Parallel()

	out := ComputeResourceContention(nil)
	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0", len(out))
	}
}

// ── ContentionHeatmap tests ───────────────────────────────────────────────────

func TestContentionHeatmap_Empty(t *testing.T) {
	t.Parallel()
	result := ContentionHeatmap(ContentionHeatmapInput{
		Segments:     nil,
		ResolutionNS: 100,
	})
	if len(result.BinsNS) != 0 || len(result.Resources) != 0 {
		t.Errorf("expected empty result for nil segments, got %+v", result)
	}
}

func TestContentionHeatmap_ZeroResolution(t *testing.T) {
	t.Parallel()
	segs := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 100, ResourceID: "mutex:0x1"},
	}
	result := ContentionHeatmap(ContentionHeatmapInput{Segments: segs, ResolutionNS: 0})
	if len(result.BinsNS) != 0 {
		t.Errorf("expected empty result for zero resolution, got %d bins", len(result.BinsNS))
	}
}

func TestContentionHeatmap_SingleSegment(t *testing.T) {
	t.Parallel()
	// Segment [0, 300) with resolution 100. The segment covers nanoseconds
	// [0,299], so it spans bins 0=[0,100), 1=[100,200), 2=[200,300).
	// Bin 3=[300,400) may exist but will have count 0.
	segs := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 300, ResourceID: "mutex:0x1"},
	}
	result := ContentionHeatmap(ContentionHeatmapInput{
		Segments:     segs,
		ResolutionNS: 100,
	})

	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	if result.Resources[0].ResourceID != "mutex:0x1" {
		t.Errorf("resource ID = %q, want mutex:0x1", result.Resources[0].ResourceID)
	}
	counts := result.Resources[0].Counts
	// Bins 0–2 must each have count 1.
	for i := 0; i < 3; i++ {
		if i >= len(counts) {
			t.Fatalf("expected at least 3 bins, got %d", len(counts))
		}
		if counts[i] != 1 {
			t.Errorf("bin %d: count = %d, want 1", i, counts[i])
		}
	}
}

func TestContentionHeatmap_PeakConcurrentAcrossBins(t *testing.T) {
	t.Parallel()
	// Two overlapping segments in bin 1 (ns 100–200), one in bin 0 and bin 2.
	//   seg1: [0, 200)  → bins 0 and 1
	//   seg2: [100, 300) → bins 1 and 2
	// Concurrent count per bin: bin0=1, bin1=2, bin2=1
	segs := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 200, ResourceID: "chan:0x1"},
		{GoroutineID: 2, StartNS: 100, EndNS: 300, ResourceID: "chan:0x1"},
	}
	result := ContentionHeatmap(ContentionHeatmapInput{
		Segments:     segs,
		ResolutionNS: 100,
	})

	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	counts := result.Resources[0].Counts
	if len(counts) < 3 {
		t.Fatalf("expected at least 3 bins, got %d", len(counts))
	}
	if counts[0] != 1 || counts[1] != 2 || counts[2] != 1 {
		t.Errorf("counts = %v, want [1 2 1 ...]", counts[:3])
	}
}

func TestContentionHeatmap_LimitResources(t *testing.T) {
	t.Parallel()
	segs := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 100, ResourceID: "a"},
		{GoroutineID: 2, StartNS: 0, EndNS: 100, ResourceID: "b"},
		{GoroutineID: 3, StartNS: 0, EndNS: 100, ResourceID: "c"},
	}
	result := ContentionHeatmap(ContentionHeatmapInput{
		Segments:       segs,
		ResolutionNS:   100,
		LimitResources: 2,
	})
	if len(result.Resources) != 2 {
		t.Errorf("expected 2 resources (limit=2), got %d", len(result.Resources))
	}
}

func TestContentionHeatmap_BinsNSAligned(t *testing.T) {
	t.Parallel()
	segs := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 500, EndNS: 1000, ResourceID: "x"},
	}
	result := ContentionHeatmap(ContentionHeatmapInput{
		Segments:     segs,
		ResolutionNS: 100,
	})
	if len(result.BinsNS) == 0 {
		t.Fatal("expected bins, got none")
	}
	// First bin should start at segment start (500).
	if result.BinsNS[0] != 500 {
		t.Errorf("first bin ns = %d, want 500", result.BinsNS[0])
	}
	// Consecutive bins differ by resolution.
	for i := 1; i < len(result.BinsNS); i++ {
		if result.BinsNS[i]-result.BinsNS[i-1] != 100 {
			t.Errorf("bin spacing at %d = %d, want 100", i, result.BinsNS[i]-result.BinsNS[i-1])
		}
	}
}

func TestContentionHeatmap_NoResourceSegmentsIgnored(t *testing.T) {
	t.Parallel()
	// Segments without ResourceID should be ignored.
	segs := []model.TimelineSegment{
		{GoroutineID: 1, StartNS: 0, EndNS: 100, ResourceID: ""},
		{GoroutineID: 2, StartNS: 0, EndNS: 100, ResourceID: "mutex:0x1"},
	}
	result := ContentionHeatmap(ContentionHeatmapInput{Segments: segs, ResolutionNS: 100})
	if len(result.Resources) != 1 {
		t.Errorf("expected 1 resource (empty ResourceID ignored), got %d", len(result.Resources))
	}
}
