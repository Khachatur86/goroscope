package analysis

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// RequestGroup aggregates goroutines that belong to the same HTTP request.
// Goroutines are associated via pprof labels (http.request_id / request_id /
// trace_id) or via parent-chain from a net/http.(*conn).serve goroutine.
type RequestGroup struct {
	RequestID      string         `json:"request_id"`
	URL            string         `json:"url,omitempty"`
	Method         string         `json:"method,omitempty"`
	StartNS        int64          `json:"start_ns"`
	EndNS          int64          `json:"end_ns"`
	DurationNS     int64          `json:"duration_ns"`
	GoroutineCount int            `json:"goroutine_count"`
	GoroutineIDs   []int64        `json:"goroutine_ids"`
	StateBreakdown map[string]int `json:"state_breakdown"`
	// Source indicates how the group was built: "label" or "stack".
	Source string `json:"source"`
}

// GroupByRequest returns goroutines grouped by HTTP request.
// Groups are sorted by DurationNS descending, falling back to GoroutineCount.
// Results are cached and only recomputed when goroutine labels or stacks
// have changed since the last call (I-4 incremental recompute).
func (e *Engine) GroupByRequest() []RequestGroup {
	// Fast path: read lock, return cache when clean.
	e.mu.RLock()
	if !e.groupsDirty {
		out := cloneRequestGroups(e.groupsCache)
		e.mu.RUnlock()
		return out
	}
	e.mu.RUnlock()

	// Slow path: recompute under write lock (double-check after acquiring).
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.groupsDirty {
		return cloneRequestGroups(e.groupsCache)
	}
	e.groupsCache = buildRequestGroups(e)
	e.groupsDirty = false
	return cloneRequestGroups(e.groupsCache)
}

func buildRequestGroups(e *Engine) []RequestGroup {
	groups := map[string]*RequestGroup{}

	// ── Pass 1: label-based grouping ────────────────────────────────────────
	for _, g := range e.goroutines {
		reqID := labelRequestID(g.Labels)
		if reqID == "" {
			continue
		}
		rg, ok := groups[reqID]
		if !ok {
			rg = &RequestGroup{
				RequestID:      reqID,
				StateBreakdown: make(map[string]int),
				Source:         "label",
				URL:            labelFirstOf(g.Labels, "http.url", "url"),
				Method:         labelFirstOf(g.Labels, "http.method", "method"),
			}
			groups[reqID] = rg
		}
		addGoroutineToGroup(rg, g)
	}

	// ── Pass 2: stack-based grouping (fallback) ──────────────────────────────
	// Build parent→children index for descendant walking.
	children := map[int64][]int64{}
	for _, g := range e.goroutines {
		if g.ParentID != 0 {
			children[g.ParentID] = append(children[g.ParentID], g.ID)
		}
	}

	// Track IDs already assigned so we don't double-count.
	assigned := map[int64]bool{}
	for _, rg := range groups {
		for _, id := range rg.GoroutineIDs {
			assigned[id] = true
		}
	}

	for _, g := range e.goroutines {
		if assigned[g.ID] {
			continue
		}
		if !stackHasHTTPServe(g.LastStack) {
			continue
		}
		reqID := fmt.Sprintf("http-%d", g.ID)
		rg := &RequestGroup{
			RequestID:      reqID,
			StateBreakdown: make(map[string]int),
			Source:         "stack",
			URL:            labelFirstOf(g.Labels, "http.url", "url"),
			Method:         labelFirstOf(g.Labels, "http.method", "method"),
		}
		// BFS over the serve goroutine and all its descendants.
		queue := []int64{g.ID}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if assigned[cur] {
				continue
			}
			assigned[cur] = true
			if cg, ok := e.goroutines[cur]; ok {
				addGoroutineToGroup(rg, cg)
			}
			queue = append(queue, children[cur]...)
		}
		groups[reqID] = rg
	}

	result := make([]RequestGroup, 0, len(groups))
	for _, rg := range groups {
		rg.GoroutineCount = len(rg.GoroutineIDs)
		if rg.EndNS > rg.StartNS {
			rg.DurationNS = rg.EndNS - rg.StartNS
		}
		sort.Slice(rg.GoroutineIDs, func(i, j int) bool {
			return rg.GoroutineIDs[i] < rg.GoroutineIDs[j]
		})
		result = append(result, *rg)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].DurationNS != result[j].DurationNS {
			return result[i].DurationNS > result[j].DurationNS
		}
		return result[i].GoroutineCount > result[j].GoroutineCount
	})
	return result
}

func addGoroutineToGroup(rg *RequestGroup, g model.Goroutine) {
	rg.GoroutineIDs = append(rg.GoroutineIDs, g.ID)
	rg.StateBreakdown[string(g.State)]++
	if g.BornNS > 0 && (rg.StartNS == 0 || g.BornNS < rg.StartNS) {
		rg.StartNS = g.BornNS
	}
	if g.DiedNS > 0 && g.DiedNS > rg.EndNS {
		rg.EndNS = g.DiedNS
	}
}

func labelRequestID(labels map[string]string) string {
	for _, key := range []string{"http.request_id", "request_id", "trace_id"} {
		if v := labels[key]; v != "" {
			return v
		}
	}
	return ""
}

func labelFirstOf(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ""
}

func stackHasHTTPServe(stack *model.StackSnapshot) bool {
	if stack == nil {
		return false
	}
	for _, f := range stack.Frames {
		if strings.Contains(f.Func, "net/http.(*conn).serve") ||
			strings.Contains(f.Func, "net/http.(*Server).Serve") ||
			strings.Contains(f.Func, "net/http.HandlerFunc.ServeHTTP") {
			return true
		}
	}
	return false
}
