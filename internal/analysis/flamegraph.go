package analysis

import (
	"sort"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// FlamegraphNode is one node in the call-tree used to render a flamegraph.
// The root node represents "all goroutines"; children are call-stack frames
// aggregated across all goroutines in the requested state.
type FlamegraphNode struct {
	// Name is the function name (or "root" for the synthetic root).
	Name string `json:"name"`
	// Value is the number of goroutines whose last stack contains this path.
	Value int `json:"value"`
	// Children is sorted by Value descending so the flamegraph renderer can
	// immediately draw the widest bars first.
	Children []*FlamegraphNode `json:"children,omitempty"`
}

// FlamegraphInput bundles parameters for BuildFlamegraph (CS-5).
type FlamegraphInput struct {
	// Goroutines is the full goroutine list from the engine.
	Goroutines []model.Goroutine
	// StateFilter restricts aggregation to goroutines in this state.
	// Empty string means "all states".
	StateFilter string
	// MaxDepth limits how many frames deep the tree is built.
	// 0 means unlimited.
	MaxDepth int
}

// FlamegraphResult wraps the root node and summary counts.
type FlamegraphResult struct {
	// Root is the synthetic root node whose value equals TotalGoroutines.
	Root FlamegraphNode `json:"root"`
	// TotalGoroutines is the number of goroutines included in the graph.
	TotalGoroutines int `json:"total_goroutines"`
	// StateFilter echoes the filter that was applied (empty = all states).
	StateFilter string `json:"state_filter,omitempty"`
}

// BuildFlamegraph aggregates the last known call-stacks of all matching
// goroutines into a call-tree suitable for d3-flamegraph or speedscope.
//
// Each goroutine contributes one path from the outermost (root) frame to the
// innermost (leaf) frame. Paths are built from the LAST recorded stack
// snapshot stored in Goroutine.LastStack. Goroutines without a LastStack are
// counted but appear only at the root level.
func BuildFlamegraph(in FlamegraphInput) FlamegraphResult {
	root := &FlamegraphNode{Name: "root"}
	total := 0

	for _, g := range in.Goroutines {
		if in.StateFilter != "" && string(g.State) != in.StateFilter {
			continue
		}
		total++
		root.Value++

		if g.LastStack == nil || len(g.LastStack.Frames) == 0 {
			// No stack data — count but don't add to tree.
			continue
		}

		// Frames are innermost-first; reverse for root→leaf traversal.
		frames := g.LastStack.Frames
		depth := len(frames)
		if in.MaxDepth > 0 && depth > in.MaxDepth {
			depth = in.MaxDepth
		}

		cur := root
		for i := depth - 1; i >= 0; i-- {
			name := frames[i].Func
			if name == "" {
				continue
			}
			cur = getOrCreateChild(cur, name)
			cur.Value++
		}
	}

	sortChildrenDeep(root)
	return FlamegraphResult{
		Root:            *root,
		TotalGoroutines: total,
		StateFilter:     in.StateFilter,
	}
}

// FoldedStacks returns the flamegraph in "folded stacks" format, one line per
// unique call path:
//
//	root;frame1;frame2;leaf N
//
// This format is consumed directly by Brendan Gregg's flamegraph.pl, speedscope
// (import → "Custom → perf script"), and the `inferno` crate.
func FoldedStacks(in FlamegraphInput) string {
	type pathCount struct {
		path  string
		count int
	}
	var lines []pathCount

	for _, g := range in.Goroutines {
		if in.StateFilter != "" && string(g.State) != in.StateFilter {
			continue
		}
		if g.LastStack == nil || len(g.LastStack.Frames) == 0 {
			lines = append(lines, pathCount{"root", 1})
			continue
		}

		frames := g.LastStack.Frames
		depth := len(frames)
		if in.MaxDepth > 0 && depth > in.MaxDepth {
			depth = in.MaxDepth
		}

		parts := make([]string, 0, depth+1)
		parts = append(parts, "root")
		for i := depth - 1; i >= 0; i-- {
			if frames[i].Func != "" {
				parts = append(parts, frames[i].Func)
			}
		}
		lines = append(lines, pathCount{strings.Join(parts, ";"), 1})
	}

	// Aggregate identical paths.
	counts := make(map[string]int, len(lines))
	for _, l := range lines {
		counts[l.path] += l.count
	}

	// Sort by path for deterministic output.
	sorted := make([]string, 0, len(counts))
	for p := range counts {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	var sb strings.Builder
	for _, p := range sorted {
		sb.WriteString(p)
		sb.WriteByte(' ')
		sb.WriteString(itoa(counts[p]))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// getOrCreateChild finds or creates a child node with the given name.
func getOrCreateChild(parent *FlamegraphNode, name string) *FlamegraphNode {
	for _, c := range parent.Children {
		if c.Name == name {
			return c
		}
	}
	child := &FlamegraphNode{Name: name}
	parent.Children = append(parent.Children, child)
	return child
}

// sortChildrenDeep sorts all children recursively by Value descending.
func sortChildrenDeep(n *FlamegraphNode) {
	sort.Slice(n.Children, func(i, j int) bool {
		return n.Children[i].Value > n.Children[j].Value
	})
	for _, c := range n.Children {
		sortChildrenDeep(c)
	}
}

// itoa converts an int to its decimal string representation without fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// Reverse.
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
