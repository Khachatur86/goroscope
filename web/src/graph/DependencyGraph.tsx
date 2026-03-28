import { useEffect, useRef, useCallback, useState } from "react";
import { createPortal } from "react-dom";
import * as d3 from "d3";
import type { Goroutine } from "../api/client";
import { STATE_COLORS, DIFF_COLORS, COLOR_UNKNOWN, COLOR_AXIS_TEXT, COLOR_EDGE,
  COLOR_EDGE_GONE, COLOR_SELECTED, BG_BASE,
  COLOR_DIFF_APPEARED_TEXT, COLOR_DIFF_DISAPPEARED_TEXT } from "../theme/tokens";

const NODE_RADIUS = 14;

type DiffStatus = "appeared" | "disappeared" | "unchanged";

type Props = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelectGoroutine: (id: number) => void;
  /** Internal flag: true when rendered inside the expand modal (prevents recursion). */
  _modal?: boolean;
};

interface NodeDatum extends d3.SimulationNodeDatum {
  id: number;
  state: string;
  fn: string;
  diff: DiffStatus;
}

interface LinkDatum extends d3.SimulationLinkDatum<NodeDatum> {
  disappeared: boolean;
}

function nodeColor(d: NodeDatum): string {
  if (d.diff === "appeared")    return DIFF_COLORS.appeared;
  if (d.diff === "disappeared") return DIFF_COLORS.disappeared;
  return STATE_COLORS[d.state] ?? COLOR_UNKNOWN;
}

/** Goroutine spawn-tree as a D3 force-directed DAG, with optional diff overlay. */
export function DependencyGraph({ goroutines, selectedId, onSelectGoroutine, _modal = false }: Props) {
  const svgRef = useRef<SVGSVGElement>(null);
  const onSelectRef = useRef(onSelectGoroutine);
  onSelectRef.current = onSelectGoroutine;

  const selectedIdRef = useRef(selectedId);
  selectedIdRef.current = selectedId;

  // Keeps the previous simulation alive so we can harvest node positions
  // before the next rebuild — prevents nodes from jumping on every poll.
  const simRef = useRef<d3.Simulation<NodeDatum, LinkDatum> | null>(null);
  // Stable reference to the D3 zoom behavior so fitToView can use it.
  const zoomRef = useRef<d3.ZoomBehavior<SVGSVGElement, unknown> | null>(null);
  // Tracks the goroutine ID set from the previous render to detect topology changes.
  const prevNodeIDsRef = useRef<Set<number>>(new Set());

  const [expanded, setExpanded] = useState(false);

  // ── Diff state ──────────────────────────────────────────────────────────
  const [baseGoroutines, setBaseGoroutines] = useState<Goroutine[] | null>(null);
  const diffActive = baseGoroutines !== null;

  // Build the effective goroutine set: union when diff is active.
  const { effectiveGoroutines, diffMap } = (() => {
    if (!baseGoroutines) {
      const map = new Map<number, DiffStatus>();
      for (const g of goroutines) map.set(g.goroutine_id, "unchanged");
      return { effectiveGoroutines: goroutines, diffMap: map };
    }
    const baseMap = new Map(baseGoroutines.map((g) => [g.goroutine_id, g]));
    const currMap = new Map(goroutines.map((g) => [g.goroutine_id, g]));
    const map = new Map<number, DiffStatus>();
    const union: Goroutine[] = [];

    for (const g of goroutines) {
      map.set(g.goroutine_id, baseMap.has(g.goroutine_id) ? "unchanged" : "appeared");
      union.push(g);
    }
    for (const g of baseGoroutines) {
      if (!currMap.has(g.goroutine_id)) {
        map.set(g.goroutine_id, "disappeared");
        union.push(g);
      }
    }
    return { effectiveGoroutines: union, diffMap: map };
  })();

  // Snapshot current goroutines as baseline.
  const handleSnapshot = useCallback(() => {
    setBaseGoroutines(goroutines);
  }, [goroutines]);

  const handleClearDiff = useCallback(() => {
    setBaseGoroutines(null);
  }, []);

  // Counts for toolbar.
  const appeared    = diffActive ? [...diffMap.values()].filter((v) => v === "appeared").length    : 0;
  const disappeared = diffActive ? [...diffMap.values()].filter((v) => v === "disappeared").length : 0;

  // ── Rebuild graph ────────────────────────────────────────────────────────
  useEffect(() => {
    const svgEl = svgRef.current;
    if (!svgEl) return;

    // ── Topology check ──────────────────────────────────────────────────────
    // If the set of goroutine IDs hasn't changed, only state/colors differ.
    // Skip the full rebuild and just repaint node colours — this prevents the
    // simulation from restarting and nodes from slowly drifting on every poll.
    const newNodeIDs = new Set(effectiveGoroutines.map((g) => g.goroutine_id));
    const topologyUnchanged =
      newNodeIDs.size === prevNodeIDsRef.current.size &&
      [...newNodeIDs].every((id) => prevNodeIDsRef.current.has(id));

    if (topologyUnchanged && simRef.current) {
      prevNodeIDsRef.current = newNodeIDs;
      const stateMap = new Map(effectiveGoroutines.map((g) => [g.goroutine_id, g.state]));
      d3.select(svgEl)
        .selectAll<SVGGElement, NodeDatum>(".dep-nodes g")
        .each(function(d) {
          const s = stateMap.get(d.id);
          if (s) d.state = s;
        })
        .select<SVGCircleElement>(".dep-node-circle")
        .attr("fill", nodeColor);
      return;
    }
    prevNodeIDsRef.current = newNodeIDs;

    const width  = svgEl.clientWidth  || 600;
    const height = svgEl.clientHeight || 460;

    const svg = d3.select(svgEl);

    // Harvest positions from the previous simulation so existing nodes don't
    // jump when the graph is rebuilt on the next poll cycle.
    const savedPos = new Map<number, { x: number; y: number }>();
    if (simRef.current) {
      for (const n of simRef.current.nodes()) {
        if (n.x !== undefined && n.y !== undefined) {
          savedPos.set(n.id, { x: n.x, y: n.y });
        }
      }
      simRef.current.stop();
    }

    svg.selectAll("*").remove();

    // ── Data ────────────────────────────────────────────────────────────────
    const idSet = new Set(effectiveGoroutines.map((g) => g.goroutine_id));

    const nodes: NodeDatum[] = effectiveGoroutines.map((g) => {
      const pos = savedPos.get(g.goroutine_id);
      return {
        id:    g.goroutine_id,
        state: g.state,
        fn:    g.labels?.["function"] ?? "",
        diff:  diffMap.get(g.goroutine_id) ?? "unchanged",
        // Restore previous position so the node doesn't jump; new nodes
        // get undefined and will be placed by the simulation.
        x: pos?.x,
        y: pos?.y,
      };
    });

    const links: LinkDatum[] = effectiveGoroutines
      .filter((g) => g.parent_id && idSet.has(g.parent_id))
      .map((g) => ({
        source:      g.parent_id as number,
        target:      g.goroutine_id,
        disappeared: diffMap.get(g.goroutine_id) === "disappeared" ||
                     diffMap.get(g.parent_id!)   === "disappeared",
      }));

    // ── Defs: arrowheads ────────────────────────────────────────────────────
    const defs = svg.append("defs");

    defs.append("marker")
      .attr("id", "dep-arrow")
      .attr("viewBox", "0 -4 10 8")
      .attr("refX", NODE_RADIUS + 9)
      .attr("refY", 0)
      .attr("markerWidth", 6)
      .attr("markerHeight", 6)
      .attr("orient", "auto")
      .append("path")
      .attr("d", "M0,-4L10,0L0,4")
      .attr("fill", COLOR_AXIS_TEXT);

    defs.append("marker")
      .attr("id", "dep-arrow-gone")
      .attr("viewBox", "0 -4 10 8")
      .attr("refX", NODE_RADIUS + 9)
      .attr("refY", 0)
      .attr("markerWidth", 6)
      .attr("markerHeight", 6)
      .attr("orient", "auto")
      .append("path")
      .attr("d", "M0,-4L10,0L0,4")
      .attr("fill", COLOR_EDGE_GONE);

    // ── Zoom group ──────────────────────────────────────────────────────────
    const g = svg.append("g").attr("class", "dep-zoom-group");

    const zoom = d3
      .zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 8])
      .on("zoom", (event) => { g.attr("transform", event.transform); });

    svg.call(zoom).on("dblclick.zoom", null);
    zoomRef.current = zoom;

    // ── Links ───────────────────────────────────────────────────────────────
    const link = g
      .append("g")
      .attr("class", "dep-links")
      .selectAll<SVGLineElement, LinkDatum>("line")
      .data(links)
      .join("line")
      .attr("stroke", (d) => d.disappeared ? COLOR_EDGE_GONE : COLOR_EDGE)
      .attr("stroke-width", 1.5)
      .attr("stroke-dasharray", (d) => d.disappeared ? "4,3" : null)
      .attr("opacity",  (d) => d.disappeared ? 0.5 : 1)
      .attr("marker-end", (d) => d.disappeared ? "url(#dep-arrow-gone)" : "url(#dep-arrow)");

    // ── Nodes ───────────────────────────────────────────────────────────────
    const nodeGroup = g
      .append("g")
      .attr("class", "dep-nodes")
      .selectAll<SVGGElement, NodeDatum>("g")
      .data(nodes)
      .join("g")
      .attr("cursor", "pointer")
      .attr("opacity", (d) => d.diff === "disappeared" ? 0.55 : 1)
      .on("click", (event, d) => {
        event.stopPropagation();
        if (d.diff !== "disappeared") onSelectRef.current(d.id);
      });

    // Drag
    const drag = d3
      .drag<SVGGElement, NodeDatum>()
      .on("start", (event, d) => {
        if (!event.active) sim.alphaTarget(0.3).restart();
        d.fx = d.x; d.fy = d.y;
      })
      .on("drag",  (event, d) => { d.fx = event.x; d.fy = event.y; })
      .on("end",   (event, d) => {
        if (!event.active) sim.alphaTarget(0);
        d.fx = null; d.fy = null;
      });

    nodeGroup.call(drag as d3.DragBehavior<SVGGElement, NodeDatum, NodeDatum | d3.SubjectPosition>);

    // Glow ring for selected
    nodeGroup
      .append("circle")
      .attr("class", "dep-node-glow")
      .attr("r", NODE_RADIUS + 5)
      .attr("fill", "none")
      .attr("stroke", (d) => (d.id === selectedIdRef.current ? COLOR_SELECTED : "none"))
      .attr("stroke-width", 2)
      .attr("opacity", 0.5);

    // Diff halo (outer ring showing appeared / disappeared)
    nodeGroup
      .filter((d) => d.diff !== "unchanged")
      .append("circle")
      .attr("r", NODE_RADIUS + 4)
      .attr("fill", "none")
      .attr("stroke", (d) => d.diff === "appeared" ? DIFF_COLORS.appeared : DIFF_COLORS.disappeared)
      .attr("stroke-width", 2.5)
      .attr("stroke-dasharray", (d) => d.diff === "disappeared" ? "4,3" : null)
      .attr("opacity", 0.8);

    // Main circle
    nodeGroup
      .append("circle")
      .attr("class", "dep-node-circle")
      .attr("r", NODE_RADIUS)
      .attr("fill", nodeColor)
      .attr("stroke", (d) =>
        d.id === selectedIdRef.current ? COLOR_SELECTED : "rgba(0,0,0,0.4)"
      )
      .attr("stroke-width", (d) => (d.id === selectedIdRef.current ? 2.5 : 1));

    // G{id} label
    nodeGroup
      .append("text")
      .attr("text-anchor", "middle")
      .attr("dy", "0.35em")
      .attr("font-size", "9px")
      .attr("font-family", "monospace")
      .attr("font-weight", "700")
      .attr("fill", BG_BASE)
      .attr("pointer-events", "none")
      .text((d) => `G${d.id}`);

    // Function name below
    nodeGroup
      .append("text")
      .attr("text-anchor", "middle")
      .attr("dy", NODE_RADIUS + 13)
      .attr("font-size", "8px")
      .attr("font-family", "sans-serif")
      .attr("fill", (d) =>
        d.diff === "appeared" ? COLOR_DIFF_APPEARED_TEXT :
        d.diff === "disappeared" ? COLOR_DIFF_DISAPPEARED_TEXT : COLOR_UNKNOWN
      )
      .attr("pointer-events", "none")
      .text((d) => {
        if (!d.fn) return "";
        const short = d.fn.slice(d.fn.lastIndexOf(".") + 1);
        return short.length > 14 ? short.slice(0, 13) + "…" : short;
      });

    // ── Simulation ──────────────────────────────────────────────────────────
    // Use a low alpha when most nodes already have saved positions so they
    // don't visibly rearrange. New nodes (no saved position) will still
    // settle in naturally because the simulation runs long enough.
    const hasKnownPositions = nodes.some((n) => n.x !== undefined);

    const sim = d3
      .forceSimulation<NodeDatum>(nodes)
      .alpha(hasKnownPositions ? 0.15 : 0.8)
      .force(
        "link",
        d3.forceLink<NodeDatum, LinkDatum>(links)
          .id((d) => d.id)
          .distance(90)
          .strength(0.5),
      )
      .force("charge",  d3.forceManyBody<NodeDatum>().strength(-250))
      .force("center",  d3.forceCenter(width / 2, height / 2))
      .force("collide", d3.forceCollide<NodeDatum>(NODE_RADIUS + 10))
      .on("tick", () => {
        link
          .attr("x1", (d) => (d.source as NodeDatum).x ?? 0)
          .attr("y1", (d) => (d.source as NodeDatum).y ?? 0)
          .attr("x2", (d) => (d.target as NodeDatum).x ?? 0)
          .attr("y2", (d) => (d.target as NodeDatum).y ?? 0);

        nodeGroup.attr("transform", (d) => `translate(${d.x ?? 0},${d.y ?? 0})`);
      })
      .on("end", () => {
        // Auto-fit only on the first layout (no saved positions).
        // Wrap in rAF so the browser has finished painting all nodes
        // before we measure getBBox() / getBoundingClientRect().
        if (!hasKnownPositions) requestAnimationFrame(() => fitToView());
      });

    simRef.current = sim;
    return () => { sim.stop(); };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveGoroutines, diffMap]);

  // Update selection highlight without full rebuild.
  useEffect(() => {
    const svgEl = svgRef.current;
    if (!svgEl) return;
    const svg = d3.select(svgEl);

    svg.selectAll<SVGCircleElement, NodeDatum>(".dep-node-circle")
      .attr("stroke", (d) => d.id === selectedId ? COLOR_SELECTED : "rgba(0,0,0,0.4)")
      .attr("stroke-width", (d) => (d.id === selectedId ? 2.5 : 1));

    svg.selectAll<SVGCircleElement, NodeDatum>(".dep-node-glow")
      .attr("stroke", (d) => (d.id === selectedId ? COLOR_SELECTED : "none"));
  }, [selectedId]);

  const fitToView = useCallback(() => {
    const svgEl = svgRef.current;
    const zoom  = zoomRef.current;
    if (!svgEl || !zoom) return;

    // Use getBBox() on the zoom group — it includes node circles AND text
    // labels and is not affected by the current zoom transform.
    const g = svgEl.querySelector<SVGGElement>(".dep-zoom-group");
    if (!g) return;
    const bbox = g.getBBox();
    if (bbox.width === 0 && bbox.height === 0) return;

    const padding = 48;
    // getBoundingClientRect gives the actual rendered SVG size after CSS layout.
    const { width: w, height: h } = svgEl.getBoundingClientRect();
    if (w === 0 || h === 0) return;

    const scale = Math.min(
      (w - 2 * padding) / bbox.width,
      (h - 2 * padding) / bbox.height,
      2, // cap zoom-in so a single node doesn't fill the screen
    );
    const tx = w / 2 - scale * (bbox.x + bbox.width  / 2);
    const ty = h / 2 - scale * (bbox.y + bbox.height / 2);

    d3.select(svgEl)
      .transition()
      .duration(400)
      .call(zoom.transform, d3.zoomIdentity.translate(tx, ty).scale(scale));
  }, []);

  return (
    <>
    <div className="dep-graph-container">
      <div className="dep-graph-toolbar">
        <span className="dep-graph-hint">
          {effectiveGoroutines.length} goroutines ·{" "}
          {effectiveGoroutines.filter((g) => g.parent_id).length} spawn edges
        </span>

        {/* Diff controls */}
        {!diffActive ? (
          <button
            type="button"
            className="timeline-control-button diff-snapshot-btn"
            onClick={handleSnapshot}
            title="Snapshot current goroutines as baseline for diff"
          >
            📷 Snapshot
          </button>
        ) : (
          <div className="dep-diff-status">
            <span className="dep-diff-badge dep-diff-appeared">
              +{appeared} appeared
            </span>
            <span className="dep-diff-badge dep-diff-disappeared">
              −{disappeared} gone
            </span>
            <button
              type="button"
              className="timeline-control-button"
              onClick={handleClearDiff}
              title="Clear diff baseline"
            >
              ✕ Clear diff
            </button>
          </div>
        )}

        <button
          type="button"
          className="timeline-control-button"
          onClick={fitToView}
          title="Fit all nodes into view"
        >
          ⊙ Fit
        </button>

        {!_modal && (
          <button
            type="button"
            className="timeline-control-button"
            onClick={() => setExpanded(true)}
            title="Open graph in full-screen window"
          >
            ⤢ Expand
          </button>
        )}
      </div>

      {/* Diff legend */}
      {diffActive && (
        <div className="dep-diff-legend">
          <span className="dep-diff-legend-item dep-diff-legend-appeared">
            <span className="dep-diff-legend-dot" />
            Appeared since snapshot
          </span>
          <span className="dep-diff-legend-item dep-diff-legend-disappeared">
            <span className="dep-diff-legend-dot dep-diff-legend-dot--dashed" />
            Gone since snapshot
          </span>
          <span className="dep-diff-legend-item">
            <span className="dep-diff-legend-dot dep-diff-legend-dot--unchanged" />
            Unchanged
          </span>
        </div>
      )}

      <svg ref={svgRef} className="dep-graph-svg" />
    </div>

    {expanded && !_modal && createPortal(
      <div
        className="dep-graph-overlay"
        onClick={(e) => { if (e.target === e.currentTarget) setExpanded(false); }}
      >
        <div className="dep-graph-modal">
          <div className="dep-graph-modal-header">
            <span className="dep-graph-modal-title">Goroutine Spawn Tree</span>
            <button
              type="button"
              className="timeline-control-button dep-graph-modal-close"
              onClick={() => setExpanded(false)}
              title="Close"
            >
              ✕ Close
            </button>
          </div>
          <DependencyGraph
            goroutines={goroutines}
            selectedId={selectedId}
            onSelectGoroutine={onSelectGoroutine}
            _modal={true}
          />
        </div>
      </div>,
      document.body,
    )}
    </>
  );
}
