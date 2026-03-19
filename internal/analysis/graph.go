package analysis

import (
	"fmt"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// BuildResourceEdges derives resource dependency edges from a sequence of events.
func BuildResourceEdges(events []model.Event) []model.ResourceEdge {
	edges := make([]model.ResourceEdge, 0, len(events))

	for _, event := range events {
		if event.Kind != model.EventKindResourceEdge || event.ResourceID == "" {
			continue
		}

		edges = append(edges, model.ResourceEdge{
			FromGoroutineID: event.GoroutineID,
			ResourceID:      event.ResourceID,
			Kind:            "unknown",
		})
	}

	return edges
}

// DeriveResourceEdgesFromTimeline builds resource edges from timeline segments.
// Goroutines that share a resource_id (e.g. same channel or mutex) get edges.
// Used when capture.Resources is empty (e.g. from runtime trace).
func DeriveResourceEdgesFromTimeline(
	segments []model.TimelineSegment,
	goroutines []model.Goroutine,
) []model.ResourceEdge {
	// resourceID -> goroutine IDs that used it
	byResource := make(map[string]map[int64]struct{})

	for _, seg := range segments {
		if seg.ResourceID == "" {
			continue
		}
		if byResource[seg.ResourceID] == nil {
			byResource[seg.ResourceID] = make(map[int64]struct{})
		}
		byResource[seg.ResourceID][seg.GoroutineID] = struct{}{}
	}

	// Also add current goroutine state (blocked on resource)
	for _, g := range goroutines {
		if g.ResourceID == "" {
			continue
		}
		if byResource[g.ResourceID] == nil {
			byResource[g.ResourceID] = make(map[int64]struct{})
		}
		byResource[g.ResourceID][g.ID] = struct{}{}
	}

	var edges []model.ResourceEdge
	for resourceID, ids := range byResource {
		idList := make([]int64, 0, len(ids))
		for id := range ids {
			idList = append(idList, id)
		}
		for i := 0; i < len(idList); i++ {
			for j := i + 1; j < len(idList); j++ {
				edges = append(edges, model.ResourceEdge{
					FromGoroutineID: idList[i],
					ToGoroutineID:   idList[j],
					ResourceID:      resourceID,
					Kind:            "derived",
				})
				edges = append(edges, model.ResourceEdge{
					FromGoroutineID: idList[j],
					ToGoroutineID:   idList[i],
					ResourceID:      resourceID,
					Kind:            "derived",
				})
			}
		}
	}

	return edges
}

// DeadlockHint describes a potential deadlock: a cycle of goroutines all blocked.
type DeadlockHint struct {
	GoroutineIDs []int64  `json:"goroutine_ids"`
	ResourceIDs  []string `json:"resource_ids"`
	// BlameChain is a human-readable chain, e.g. "G17 holds chan:0x1 waiting for G42; G42 holds mutex:0x2 waiting for G17"
	BlameChain string `json:"blame_chain,omitempty"`
}

// FindDeadlockHints returns cycles in the resource graph where all goroutines
// are in a blocked/waiting state. Such cycles may indicate a deadlock.
func FindDeadlockHints(edges []model.ResourceEdge, goroutines []model.Goroutine) []DeadlockHint {
	blocked := make(map[int64]bool)
	for _, g := range goroutines {
		blocked[g.ID] = isBlockedState(g.State)
	}

	adj := make(map[int64][]int64)
	for _, e := range edges {
		if e.ToGoroutineID != 0 {
			adj[e.FromGoroutineID] = append(adj[e.FromGoroutineID], e.ToGoroutineID)
		}
	}

	seenCycles := make(map[string]bool)
	var hints []DeadlockHint

	for id := range adj {
		cycle := findCycleFrom(id, nil, adj, make(map[int64]bool))
		if len(cycle) >= 2 {
			key := cycleKey(cycle)
			if seenCycles[key] {
				continue
			}
			seenCycles[key] = true
			allBlocked := true
			for _, cid := range cycle {
				if !blocked[cid] {
					allBlocked = false
					break
				}
			}
			if allBlocked {
				resources := resourcesInCycle(cycle, edges)
				chain := buildBlameChain(cycle, edges, goroutines)
				hints = append(hints, DeadlockHint{
					GoroutineIDs: cycle,
					ResourceIDs:  resources,
					BlameChain:   chain,
				})
			}
		}
	}

	return hints
}

func isBlockedState(s model.GoroutineState) bool {
	switch s {
	case model.StateBlocked, model.StateWaiting, model.StateSyscall:
		return true
	default:
		return false
	}
}

// findCycleFrom does DFS; when it hits a node already in path (back edge), returns the cycle.
func findCycleFrom(curr int64, path []int64, adj map[int64][]int64, inPath map[int64]bool) []int64 {
	if inPath[curr] {
		// Back edge: curr is in path, extract cycle from curr to curr
		var startIdx int
		for i, v := range path {
			if v == curr {
				startIdx = i
				break
			}
		}
		cycle := make([]int64, len(path)-startIdx)
		copy(cycle, path[startIdx:])
		return cycle
	}

	inPath[curr] = true
	path = append(path, curr)

	for _, next := range adj[curr] {
		if c := findCycleFrom(next, path, adj, inPath); len(c) > 0 {
			return c
		}
	}

	inPath[curr] = false
	return nil
}

func cycleKey(cycle []int64) string {
	// Normalize: smallest ID first for consistent key
	if len(cycle) == 0 {
		return ""
	}
	minIdx := 0
	for i := 1; i < len(cycle); i++ {
		if cycle[i] < cycle[minIdx] {
			minIdx = i
		}
	}
	// Rotate so min is first
	rotated := make([]int64, len(cycle))
	for i := range cycle {
		rotated[i] = cycle[(minIdx+i)%len(cycle)]
	}
	var b []byte
	for _, id := range rotated {
		b = append(b, fmt.Sprintf(",%d", id)...)
	}
	return string(b)
}

// buildBlameChain produces a human-readable chain for a deadlock cycle.
// Example: "G17 holds chan:0x1 waiting for G42; G42 holds mutex:0x2 waiting for G17"
func buildBlameChain(cycle []int64, edges []model.ResourceEdge, goroutines []model.Goroutine) string {
	blockedOn := make(map[int64]string)
	for _, g := range goroutines {
		if g.ResourceID != "" && isBlockedState(g.State) {
			blockedOn[g.ID] = g.ResourceID
		}
	}
	// Build resource between consecutive pairs in cycle
	resourceBetween := func(a, b int64) string {
		for _, e := range edges {
			if ((e.FromGoroutineID == a && e.ToGoroutineID == b) || (e.FromGoroutineID == b && e.ToGoroutineID == a)) && e.ResourceID != "" {
				return e.ResourceID
			}
		}
		if r := blockedOn[a]; r != "" {
			return r
		}
		return "?"
	}

	var parts []string
	for i := 0; i < len(cycle); i++ {
		curr := cycle[i]
		next := cycle[(i+1)%len(cycle)]
		res := resourceBetween(curr, next)
		parts = append(parts, fmt.Sprintf("G%d holds %s waiting for G%d", curr, res, next))
	}
	return strings.Join(parts, "; ")
}

func resourcesInCycle(cycle []int64, edges []model.ResourceEdge) []string {
	seen := make(map[string]bool)
	var out []string
	cycleSet := make(map[int64]bool)
	for _, id := range cycle {
		cycleSet[id] = true
	}
	for _, e := range edges {
		if cycleSet[e.FromGoroutineID] && cycleSet[e.ToGoroutineID] && e.ResourceID != "" {
			if !seen[e.ResourceID] {
				seen[e.ResourceID] = true
				out = append(out, e.ResourceID)
			}
		}
	}
	return out
}
