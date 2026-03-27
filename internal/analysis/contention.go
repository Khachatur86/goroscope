package analysis

import (
	"sort"

	"github.com/Khachatur86/goroscope/internal/model"
)

// HeatmapRow holds per-bin concurrent-waiter counts for one resource.
type HeatmapRow struct {
	ResourceID string  `json:"resource_id"`
	Counts     []int32 `json:"counts"`
}

// ContentionHeatmapResult is the response shape for GET /api/v1/contention/heatmap.
type ContentionHeatmapResult struct {
	// BinsNS contains the nanosecond start timestamp of each bin.
	BinsNS    []int64      `json:"bins_ns"`
	Resources []HeatmapRow `json:"resources"`
}

// ContentionHeatmapInput bundles parameters for ContentionHeatmap (CS-5).
type ContentionHeatmapInput struct {
	Segments       []model.TimelineSegment
	ResolutionNS   int64 // width of each bin; must be > 0
	LimitResources int   // max rows returned; 0 = no limit
}

// ContentionHeatmap builds a time × resource matrix of concurrent-waiter counts.
//
// Algorithm (per resource):
//  1. Build a delta array: delta[b_start]++ and delta[b_end+1]-- for each segment.
//  2. Prefix-sum gives the instantaneous concurrent count at the start of each bin.
//
// This runs in O(S + B) per resource (S=segments, B=bins) and handles 1000
// resources × 1000 bins well within the 100ms budget.
func ContentionHeatmap(in ContentionHeatmapInput) ContentionHeatmapResult {
	if in.ResolutionNS <= 0 {
		return ContentionHeatmapResult{}
	}

	// Collect segments by resource and find the overall time range.
	byResource := make(map[string][]segBounds)
	var minNS, maxNS int64
	first := true
	for _, s := range in.Segments {
		if s.ResourceID == "" || s.EndNS <= s.StartNS {
			continue
		}
		byResource[s.ResourceID] = append(byResource[s.ResourceID], segBounds{s.StartNS, s.EndNS})
		if first || s.StartNS < minNS {
			minNS = s.StartNS
		}
		if first || s.EndNS > maxNS {
			maxNS = s.EndNS
		}
		first = false
	}

	if len(byResource) == 0 {
		return ContentionHeatmapResult{}
	}

	numBins := int((maxNS-minNS)/in.ResolutionNS) + 1

	// Build bins_ns slice.
	binsNS := make([]int64, numBins)
	for i := range binsNS {
		binsNS[i] = minNS + int64(i)*in.ResolutionNS
	}

	// Sort resources by total segment count (most contended first) and apply limit.
	type resourceEntry struct {
		id   string
		segs []segBounds
	}
	resources := make([]resourceEntry, 0, len(byResource))
	for id, segs := range byResource {
		resources = append(resources, resourceEntry{id, segs})
	}
	sort.Slice(resources, func(i, j int) bool {
		if len(resources[i].segs) != len(resources[j].segs) {
			return len(resources[i].segs) > len(resources[j].segs)
		}
		return resources[i].id < resources[j].id
	})
	if in.LimitResources > 0 && len(resources) > in.LimitResources {
		resources = resources[:in.LimitResources]
	}

	// Build rows using delta arrays.
	rows := make([]HeatmapRow, len(resources))
	for ri, res := range resources {
		delta := make([]int32, numBins+1)
		for _, seg := range res.segs {
			bStart := int((seg.start - minNS) / in.ResolutionNS)
			// seg.end is exclusive; subtract 1 so a segment ending exactly on a
			// bin boundary doesn't bleed into the following bin.
			bEnd := int((seg.end - 1 - minNS) / in.ResolutionNS)
			if bStart < 0 {
				bStart = 0
			}
			if bEnd >= numBins {
				bEnd = numBins - 1
			}
			delta[bStart]++
			if bEnd+1 <= numBins {
				delta[bEnd+1]--
			}
		}
		counts := make([]int32, numBins)
		var running int32
		for i := range counts {
			running += delta[i]
			counts[i] = running
		}
		rows[ri] = HeatmapRow{ResourceID: res.id, Counts: counts}
	}

	return ContentionHeatmapResult{BinsNS: binsNS, Resources: rows}
}

// ResourceContention holds contention metrics for a single resource.
type ResourceContention struct {
	ResourceID   string  `json:"resource_id"`
	PeakWaiters  int     `json:"peak_waiters"`
	SegmentCount int     `json:"segment_count"`
	TotalWaitNS  int64   `json:"total_wait_ns"`
	AvgWaitNS    float64 `json:"avg_wait_ns"`
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
		ns    int64
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
