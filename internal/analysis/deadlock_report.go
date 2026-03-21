package analysis

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// DeadlockReport is a structured, human- and machine-readable summary of all
// deadlock hints found in a capture, enriched with stack frames and formatted
// blame chains.
type DeadlockReport struct {
	// Total is the number of distinct deadlock cycles found.
	Total int `json:"total"`
	// Cycles contains one entry per distinct deadlock cycle.
	Cycles []DeadlockCycle `json:"cycles"`
}

// DeadlockCycle describes a single deadlock cycle with full goroutine detail.
type DeadlockCycle struct {
	// Index is the 1-based cycle number (for human output).
	Index int `json:"index"`
	// BlameChain is the short one-line chain: "G17 holds X waiting for G42; G42 holds Y waiting for G17"
	BlameChain string `json:"blame_chain"`
	// ResourceIDs are the resources involved in the cycle.
	ResourceIDs []string `json:"resource_ids"`
	// Goroutines lists each participating goroutine with its state and top stack frame.
	Goroutines []DeadlockGoroutine `json:"goroutines"`
}

// DeadlockGoroutine is one participant in a deadlock cycle.
type DeadlockGoroutine struct {
	// ID is the goroutine ID.
	ID int64 `json:"id"`
	// State is the current goroutine state (always blocked/waiting for deadlocks).
	State model.GoroutineState `json:"state"`
	// BlockedOn is the resource ID this goroutine is waiting for (may be empty).
	BlockedOn string `json:"blocked_on,omitempty"`
	// TopFrame is the innermost user-space frame from the last stack snapshot.
	TopFrame *model.StackFrame `json:"top_frame,omitempty"`
	// FullStack contains all frames from the last snapshot (deepest first).
	FullStack []model.StackFrame `json:"full_stack,omitempty"`
}

// BuildDeadlockReportInput holds everything needed to produce a DeadlockReport.
type BuildDeadlockReportInput struct {
	Hints      []DeadlockHint
	Goroutines []model.Goroutine
}

// BuildDeadlockReport combines the raw DeadlockHint slice with goroutine
// metadata to produce a fully annotated DeadlockReport.
func BuildDeadlockReport(in BuildDeadlockReportInput) DeadlockReport {
	goroutineByID := make(map[int64]model.Goroutine, len(in.Goroutines))
	for _, g := range in.Goroutines {
		goroutineByID[g.ID] = g
	}

	cycles := make([]DeadlockCycle, 0, len(in.Hints))
	for i, h := range in.Hints {
		goroutines := make([]DeadlockGoroutine, 0, len(h.GoroutineIDs))
		for _, gid := range h.GoroutineIDs {
			g, ok := goroutineByID[gid]
			if !ok {
				goroutines = append(goroutines, DeadlockGoroutine{ID: gid})
				continue
			}
			dg := DeadlockGoroutine{
				ID:        gid,
				State:     g.State,
				BlockedOn: g.ResourceID,
			}
			if g.LastStack != nil && len(g.LastStack.Frames) > 0 {
				frames := g.LastStack.Frames
				// Find the top user-space frame (skip runtime internals).
				top := topUserFrame(frames)
				if top != nil {
					dg.TopFrame = top
				}
				cp := make([]model.StackFrame, len(frames))
				copy(cp, frames)
				dg.FullStack = cp
			}
			goroutines = append(goroutines, dg)
		}
		cycles = append(cycles, DeadlockCycle{
			Index:      i + 1,
			BlameChain: h.BlameChain,
			ResourceIDs: func() []string {
				ids := make([]string, len(h.ResourceIDs))
				copy(ids, h.ResourceIDs)
				return ids
			}(),
			Goroutines: goroutines,
		})
	}
	return DeadlockReport{Total: len(cycles), Cycles: cycles}
}

// topUserFrame returns the first non-runtime frame from a stack, or nil if all
// frames are runtime internals.
func topUserFrame(frames []model.StackFrame) *model.StackFrame {
	for i := range frames {
		f := &frames[i]
		fn := f.Func
		if strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "runtime/") || strings.HasPrefix(fn, "sync.") {
			continue
		}
		return f
	}
	return nil
}

// WriteText formats the DeadlockReport as human-readable text and writes it to w.
// Each cycle prints its blame chain and the top stack frame for each goroutine.
func (r DeadlockReport) WriteText(w io.Writer) {
	if r.Total == 0 {
		_, _ = fmt.Fprintln(w, "No deadlock hints found.")
		return
	}
	_, _ = fmt.Fprintf(w, "⚠  %d potential deadlock cycle(s) found\n\n", r.Total)
	for _, c := range r.Cycles {
		_, _ = fmt.Fprintf(w, "── Cycle #%d ──────────────────────────────────────────\n", c.Index)
		_, _ = fmt.Fprintf(w, "   %s\n\n", c.BlameChain)
		for _, g := range c.Goroutines {
			_, _ = fmt.Fprintf(w, "   G%d  state=%s", g.ID, g.State)
			if g.BlockedOn != "" {
				_, _ = fmt.Fprintf(w, "  blocked-on=%s", g.BlockedOn)
			}
			_, _ = fmt.Fprintln(w)
			if g.TopFrame != nil {
				_, _ = fmt.Fprintf(w, "       %s\n", g.TopFrame.Func)
				_, _ = fmt.Fprintf(w, "       %s:%d\n", g.TopFrame.File, g.TopFrame.Line)
			}
		}
		_, _ = fmt.Fprintln(w)
	}
}

// WriteJSON writes the DeadlockReport as indented JSON to w.
func (r DeadlockReport) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteGitHubAnnotations writes GitHub Actions workflow command annotations so
// that deadlock cycles appear as warning annotations in the PR diff view.
// Format: ::warning file=<file>,line=<line>::<message>
func (r DeadlockReport) WriteGitHubAnnotations(w io.Writer) {
	for _, c := range r.Cycles {
		// Annotate each goroutine's top frame if available.
		annotated := false
		for _, g := range c.Goroutines {
			if g.TopFrame == nil {
				continue
			}
			file := g.TopFrame.File
			line := g.TopFrame.Line
			msg := fmt.Sprintf(
				"Potential deadlock in cycle #%d: G%d blocked on %s. %s",
				c.Index, g.ID, g.BlockedOn, c.BlameChain,
			)
			_, _ = fmt.Fprintf(w, "::warning file=%s,line=%d::%s\n", file, line, escapeAnnotation(msg))
			annotated = true
		}
		if !annotated {
			// No frame info: emit a generic annotation.
			_, _ = fmt.Fprintf(w, "::warning ::%s\n", escapeAnnotation(
				fmt.Sprintf("Potential deadlock in cycle #%d: %s", c.Index, c.BlameChain),
			))
		}
	}
}

// escapeAnnotation escapes characters that would break GitHub Actions workflow
// command syntax: % → %25, \r → %0D, \n → %0A, : → %3A, , → %2C.
func escapeAnnotation(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, ",", "%2C")
	return s
}

// ────────────────────────────────────────────────────────────────────────────
// Wait-For Graph (WFG) and DOT export
// ────────────────────────────────────────────────────────────────────────────

// WaitForEdge represents a directed "waits-for" dependency: goroutine From is
// blocked on ResourceID which is held (or last used) by goroutine To.
type WaitForEdge struct {
	From       int64  `json:"from"`
	To         int64  `json:"to"`
	ResourceID string `json:"resource_id"`
}

// WaitForGraph is a directed wait-for graph derived from the resource edge set
// and the current goroutine states.
type WaitForGraph struct {
	Edges  []WaitForEdge    `json:"edges"`
	Cycles [][]int64        `json:"cycles,omitempty"`
	Nodes  map[int64]string `json:"nodes"` // goroutineID → label
}

// BuildWaitForGraphInput holds the data needed to construct a WaitForGraph.
type BuildWaitForGraphInput struct {
	Edges      []model.ResourceEdge
	Goroutines []model.Goroutine
}

// BuildWaitForGraph produces a directed Wait-For Graph from the resource edge
// set and current goroutine states. Edges flow from the goroutine that is
// blocked toward the goroutine that holds the resource.
//
// When ResourceEdge.ToGoroutineID is set (e.g. from the agent), this is used
// directly. When edges are derived (bidirectional, Kind="derived"), the
// currently-blocked goroutines define the direction: blocked goroutine waits
// for the non-blocked one that shares the resource.
func BuildWaitForGraph(in BuildWaitForGraphInput) WaitForGraph {
	blocked := make(map[int64]string) // goroutineID → resourceID they're blocked on
	for _, g := range in.Goroutines {
		if isBlockedState(g.State) && g.ResourceID != "" {
			blocked[g.ID] = g.ResourceID
		}
	}

	nodeLabel := make(map[int64]string)
	for _, g := range in.Goroutines {
		label := fmt.Sprintf("G%d", g.ID)
		if g.LastStack != nil {
			if top := topUserFrame(g.LastStack.Frames); top != nil {
				short := top.Func
				if idx := strings.LastIndex(short, "."); idx >= 0 {
					short = short[idx+1:]
				}
				label = fmt.Sprintf("G%d\\n%s", g.ID, short)
			}
		}
		nodeLabel[g.ID] = label
	}

	seenEdge := make(map[string]bool)
	var wfEdges []WaitForEdge

	addEdge := func(from, to int64, resource string) {
		k := fmt.Sprintf("%d→%d@%s", from, to, resource)
		if seenEdge[k] || from == to {
			return
		}
		seenEdge[k] = true
		wfEdges = append(wfEdges, WaitForEdge{From: from, To: to, ResourceID: resource})
	}

	for _, e := range in.Edges {
		if e.ToGoroutineID != 0 {
			// Explicit directed edge from agent.
			addEdge(e.FromGoroutineID, e.ToGoroutineID, e.ResourceID)
			continue
		}
		// Undirected "derived" edge: infer direction from blocked state.
		r, fromBlocked := blocked[e.FromGoroutineID]
		if fromBlocked && r == e.ResourceID {
			// FromGoroutine is blocked on this resource; it waits for someone.
			// Use ToGoroutineID if non-zero, otherwise skip (no direction).
			continue
		}
	}

	// For blocked goroutines with a known resourceID, scan all edges for a
	// counterpart that shares the resource and is NOT blocked → waits-for that one.
	resourceUsers := make(map[string][]int64)
	for _, e := range in.Edges {
		if e.ResourceID != "" {
			resourceUsers[e.ResourceID] = append(resourceUsers[e.ResourceID], e.FromGoroutineID)
			if e.ToGoroutineID != 0 {
				resourceUsers[e.ResourceID] = append(resourceUsers[e.ResourceID], e.ToGoroutineID)
			}
		}
	}
	for blockedID, resID := range blocked {
		for _, candidate := range resourceUsers[resID] {
			if candidate == blockedID {
				continue
			}
			// candidate is on the same resource; if it's not blocked, it likely holds it.
			if _, candidateBlocked := blocked[candidate]; !candidateBlocked {
				addEdge(blockedID, candidate, resID)
			} else {
				// Both blocked → mutual wait, add both directions.
				addEdge(blockedID, candidate, resID)
			}
		}
	}

	// Detect cycles in the WFG using the same DFS from FindDeadlockHints.
	adj := make(map[int64][]int64)
	for _, e := range wfEdges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seenCycles := make(map[string]bool)
	var cycles [][]int64
	for id := range adj {
		cycle := findCycleFrom(id, nil, adj, make(map[int64]bool))
		if len(cycle) >= 2 {
			k := cycleKey(cycle)
			if !seenCycles[k] {
				seenCycles[k] = true
				cp := make([]int64, len(cycle))
				copy(cp, cycle)
				sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
				cycles = append(cycles, cp)
			}
		}
	}

	return WaitForGraph{
		Edges:  wfEdges,
		Cycles: cycles,
		Nodes:  nodeLabel,
	}
}

// WriteDOT writes the WaitForGraph as a Graphviz DOT digraph to w.
// Goroutines in a deadlock cycle are highlighted in red; all others in steel blue.
func (g WaitForGraph) WriteDOT(w io.Writer) {
	inCycle := make(map[int64]bool)
	for _, c := range g.Cycles {
		for _, id := range c {
			inCycle[id] = true
		}
	}

	_, _ = fmt.Fprintln(w, `digraph wait_for_graph {`)
	_, _ = fmt.Fprintln(w, `  rankdir=LR;`)
	_, _ = fmt.Fprintln(w, `  node [shape=box, style=filled, fontname="monospace", fontsize=11];`)
	_, _ = fmt.Fprintln(w, `  edge [fontsize=10];`)
	_, _ = fmt.Fprintln(w)

	// Emit nodes.
	nodeIDs := make([]int64, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	for _, id := range nodeIDs {
		label := g.Nodes[id]
		color := `"#5b9bd5"` // steel blue
		fontColor := `"white"`
		if inCycle[id] {
			color = `"#c00000"` // red for deadlocked
		}
		_, _ = fmt.Fprintf(w, "  G%d [label=%q, fillcolor=%s, fontcolor=%s];\n",
			id, label, color, fontColor)
	}
	_, _ = fmt.Fprintln(w)

	// Emit edges.
	for _, e := range g.Edges {
		label := e.ResourceID
		if len(label) > 30 {
			label = label[:27] + "..."
		}
		color := `"#888888"`
		inCycleEdge := inCycle[e.From] && inCycle[e.To]
		if inCycleEdge {
			color = `"#c00000"`
		}
		_, _ = fmt.Fprintf(w, "  G%d -> G%d [label=%q, color=%s];\n",
			e.From, e.To, label, color)
	}

	if len(g.Cycles) > 0 {
		_, _ = fmt.Fprintln(w)
		for i, c := range g.Cycles {
			ids := make([]string, len(c))
			for j, id := range c {
				ids[j] = fmt.Sprintf("G%d", id)
			}
			_, _ = fmt.Fprintf(w, "  // cycle #%d: %s\n", i+1, strings.Join(ids, " → "))
		}
	}

	_, _ = fmt.Fprintln(w, `}`)
}
