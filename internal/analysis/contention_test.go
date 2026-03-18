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
