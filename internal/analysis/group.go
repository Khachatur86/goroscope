package analysis

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// GroupByField specifies the dimension to group goroutines by.
type GroupByField string

const (
	// GroupByFunction groups by the outermost user-space function name.
	GroupByFunction GroupByField = "function"
	// GroupByPackage groups by the outermost user-space package name.
	GroupByPackage GroupByField = "package"
	// GroupByParentID groups by the spawning goroutine's ID.
	GroupByParentID GroupByField = "parent_id"
	// GroupByLabel groups by the value of a specific pprof label key.
	GroupByLabel GroupByField = "label"
)

// GoroutineGroup aggregates metrics for a set of goroutines that share the
// same grouping key.
type GoroutineGroup struct {
	Key          string                       `json:"key"`
	By           GroupByField                 `json:"by"`
	Count        int                          `json:"count"`
	States       map[model.GoroutineState]int `json:"states"`
	AvgWaitNS    float64                      `json:"avg_wait_ns"`
	MaxWaitNS    int64                        `json:"max_wait_ns"`
	TotalWaitNS  int64                        `json:"total_wait_ns"`
	TotalCPUNS   int64                        `json:"total_cpu_ns"`
	GoroutineIDs []int64                      `json:"goroutine_ids"`
}

// GroupGoroutinesInput holds all inputs for GroupGoroutines.
// ctx is intentionally not in this struct per CTX-1: pass it as first arg if needed.
type GroupGoroutinesInput struct {
	Goroutines []model.Goroutine
	Segments   []model.TimelineSegment
	By         GroupByField
	// LabelKey is required when By == GroupByLabel.
	LabelKey string
}

// GroupGoroutines aggregates goroutines by the requested dimension and enriches
// each group with wait/CPU metrics derived from the timeline segments.
//
// Returns groups sorted by count descending (highest-traffic groups first).
// Returns an error only for invalid GroupByField values.
func GroupGoroutines(in GroupGoroutinesInput) ([]GoroutineGroup, error) {
	if err := validateGroupBy(in.By); err != nil {
		return nil, err
	}

	// Build per-goroutine CPU time from RUNNING segments.
	cpuByGoroutine := make(map[int64]int64, len(in.Goroutines))
	for _, seg := range in.Segments {
		if seg.State == model.StateRunning && seg.EndNS > seg.StartNS {
			cpuByGoroutine[seg.GoroutineID] += seg.EndNS - seg.StartNS
		}
	}

	// Bucket goroutines by key.
	type bucket struct {
		ids    []int64
		states map[model.GoroutineState]int
		waitNS []int64
		cpuNS  int64
	}
	buckets := make(map[string]*bucket)

	for _, g := range in.Goroutines {
		key := goroutineGroupKey(g, in.By, in.LabelKey)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{states: make(map[model.GoroutineState]int)}
			buckets[key] = b
		}
		b.ids = append(b.ids, g.ID)
		if g.State != "" {
			b.states[g.State]++
		}
		if g.WaitNS > 0 {
			b.waitNS = append(b.waitNS, g.WaitNS)
		}
		b.cpuNS += cpuByGoroutine[g.ID]
	}

	// Convert buckets to groups.
	groups := make([]GoroutineGroup, 0, len(buckets))
	for key, b := range buckets {
		var totalWait, maxWait int64
		for _, w := range b.waitNS {
			totalWait += w
			if w > maxWait {
				maxWait = w
			}
		}
		avgWait := 0.0
		if len(b.ids) > 0 {
			avgWait = float64(totalWait) / float64(len(b.ids))
		}

		// Sort IDs for deterministic output.
		sort.Slice(b.ids, func(i, j int) bool { return b.ids[i] < b.ids[j] })

		groups = append(groups, GoroutineGroup{
			Key:          key,
			By:           in.By,
			Count:        len(b.ids),
			States:       b.states,
			AvgWaitNS:    avgWait,
			MaxWaitNS:    maxWait,
			TotalWaitNS:  totalWait,
			TotalCPUNS:   b.cpuNS,
			GoroutineIDs: b.ids,
		})
	}

	// Sort by count descending, then key ascending for stability.
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Key < groups[j].Key
	})

	return groups, nil
}

// goroutineGroupKey returns the grouping key for g under the requested dimension.
func goroutineGroupKey(g model.Goroutine, by GroupByField, labelKey string) string {
	switch by {
	case GroupByFunction:
		if g.LastStack != nil {
			for _, f := range g.LastStack.Frames {
				if isUserFrame(f.Func) {
					return shortFuncName(f.Func)
				}
			}
		}
		return "(unknown)"
	case GroupByPackage:
		if g.LastStack != nil {
			for _, f := range g.LastStack.Frames {
				if isUserFrame(f.Func) {
					return packageFromFunc(f.Func)
				}
			}
		}
		return "(unknown)"
	case GroupByParentID:
		if g.ParentID == 0 {
			return "(root)"
		}
		return fmt.Sprintf("%d", g.ParentID)
	case GroupByLabel:
		if labelKey != "" && g.Labels != nil {
			if v, ok := g.Labels[labelKey]; ok && v != "" {
				return v
			}
		}
		return "(no label)"
	default:
		return "(unknown)"
	}
}

// isUserFrame reports whether the fully-qualified function name belongs to
// user code (as opposed to Go runtime or standard library internals).
func isUserFrame(fn string) bool {
	if fn == "" {
		return false
	}
	// Skip runtime-internal frames.
	if strings.HasPrefix(fn, "runtime.") ||
		strings.HasPrefix(fn, "runtime/") ||
		strings.HasPrefix(fn, "syscall.") ||
		strings.HasPrefix(fn, "sync.") ||
		strings.HasPrefix(fn, "internal/") {
		return false
	}
	return true
}

// shortFuncName returns the identifier portion of a fully-qualified function
// name, stripping the import-path prefix, e.g.:
//
//	"github.com/foo/bar.(*Baz).Run"  → "(*Baz).Run"
//	"github.com/foo/bar.Run"         → "bar.Run"
//	"main.main"                      → "main.main"
func shortFuncName(fn string) string {
	// Advance past the last path separator to reach the final package.identifier part.
	lastSlash := strings.LastIndexByte(fn, '/')
	suffix := fn
	if lastSlash >= 0 {
		suffix = fn[lastSlash+1:]
	}
	// suffix is now e.g. "bar.(*Baz).Run" or "main.main".
	// If the identifier starts with a type receiver like "(*" or "(", strip the
	// package prefix up to and including the first dot before the receiver.
	if dot := strings.IndexByte(suffix, '.'); dot >= 0 {
		rest := suffix[dot+1:]
		// Only strip if what follows the dot looks like a receiver or identifier
		// (starts with '(' for pointer receivers, or an uppercase/lowercase letter).
		if len(rest) > 0 && (rest[0] == '(' || rest[0] == '_' ||
			(rest[0] >= 'A' && rest[0] <= 'Z') || (rest[0] >= 'a' && rest[0] <= 'z')) {
			// If the rest itself contains a dot (method on a type), it's a receiver.
			// Return "Type.Method" form (drop the package name prefix only when there
			// is a receiver/type portion that makes it unambiguous).
			if strings.ContainsRune(rest, '.') || rest[0] == '(' {
				return rest
			}
		}
	}
	return suffix
}

// packageFromFunc extracts the import path up to (but not including) the last
// path component's dot-separated receiver/function, e.g.
// "github.com/foo/bar.(*Baz).Run" → "github.com/foo/bar".
func packageFromFunc(fn string) string {
	// The last '/' separates the final path segment from previous segments.
	// Within the final segment, the first '.' separates the package name from
	// the identifier.
	lastSlash := strings.LastIndexByte(fn, '/')
	suffix := fn
	prefix := ""
	if lastSlash >= 0 {
		prefix = fn[:lastSlash+1]
		suffix = fn[lastSlash+1:]
	}
	if dot := strings.IndexByte(suffix, '.'); dot >= 0 {
		return prefix + suffix[:dot]
	}
	return fn
}

// validateGroupBy returns an error for unrecognised GroupByField values.
func validateGroupBy(by GroupByField) error {
	switch by {
	case GroupByFunction, GroupByPackage, GroupByParentID, GroupByLabel:
		return nil
	default:
		return fmt.Errorf("invalid group_by value %q: must be one of function, package, parent_id, label", by)
	}
}
