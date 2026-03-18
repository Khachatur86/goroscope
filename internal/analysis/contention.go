package analysis

import (
	"sort"

	"github.com/Khachatur86/goroscope/internal/model"
)

// ResourceContention holds contention metrics for a single resource.
type ResourceContention struct {
	ResourceID         string  `json:"resource_id"`
	PeakWaiters        int     `json:"peak_waiters"`
	SegmentCount       int     `json:"segment_count"`
	TotalWaitNS        int64   `json:"total_wait_ns"`
	AvgWaitNS          float64 `json:"avg_wait_ns"`
}

// ComputeResourceContention derives contention metrics from timeline segments.
// For each resource, computes peak concurrent waiters and average wait duration.
func ComputeResourceContention(segments []model.TimelineSegment) []ResourceContention {
	// Collect segments that have a resource_id (blocked/waiting on a resource)
	byResource := make(map[string][]segBounds)
	for _, s := range segments {
		if s.ResourceID == "" {
			continue
		}
		byResource[s.ResourceID] = append(byResource[s.ResourceID], segBounds{s.StartNS, s.EndNS})
	}

	var out []ResourceContention
	for resourceID, segs := range byResource {
		peak := peakConcurrent(segs)
		var total int64
		for _, s := range segs {
			total += s.end - s.start
		}
		avg := 0.0
		if len(segs) > 0 {
			avg = float64(total) / float64(len(segs))
		}
		out = append(out, ResourceContention{
			ResourceID:   resourceID,
			PeakWaiters:  peak,
			SegmentCount: len(segs),
			TotalWaitNS:  total,
			AvgWaitNS:    avg,
		})
	}
	return out
}

type segBounds struct {
	start, end int64
}

// peakConcurrent returns the maximum number of overlapping segments.
func peakConcurrent(segs []segBounds) int {
	if len(segs) == 0 {
		return 0
	}
	// Sweep line: collect all start/end events, sort by time
	type event struct {
		ns   int64
		delta int // +1 for start, -1 for end
	}
	var events []event
	for _, s := range segs {
		events = append(events, event{s.start, 1}, event{s.end, -1})
	}
	// Sort by ns, then end (-1) before start (+1) so we don't double-count boundaries
	sort.Slice(events, func(i, j int) bool {
		if events[i].ns != events[j].ns {
			return events[i].ns < events[j].ns
		}
		return events[i].delta < events[j].delta
	})
	peak := 0
	cur := 0
	for _, e := range events {
		cur += e.delta
		if cur > peak {
			peak = cur
		}
	}
	return peak
}
