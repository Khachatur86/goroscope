import { useEffect, useRef, useCallback, useState } from "react";
import * as d3 from "d3";
import type { Goroutine } from "../api/client";

const STATE_COLORS: Record<string, string> = {
  RUNNING: "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING: "#f59e0b",
  BLOCKED: "#f43f5e",
  SYSCALL: "#4da6ff",
  DONE:    "#4b5563",
};

const DIFF_COLORS = {
  appeared:    "#22c55e",   // green
  disappeared: "#f43f5e",   // red / crimson
  unchanged:   null,        // use STATE_COLORS
} as const;

const NODE_RADIUS = 14;

type DiffStatus = "appeared" | "disappeared" | "unchanged";

type Props = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelectGoroutine: (id: number) => void;
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
  return STATE_COLORS[d.state] ?? "#94a3b8";
}

/** Goroutine spawn-tree as a D3 force-directed DAG, with optional diff overlay. */
export function DependencyGraph({ goroutines, selectedId, onSelectGoroutine }: Props) {
  const svgRef = useRef<SVGSVGElement>(null);
  const onSelectRef = useRef(onSelectGoroutine);
  onSelectRef.current = onSelectGoroutine;

  const selectedIdRef = useRef(selectedId);
  selectedIdRef.current = selectedId;

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

    const width  = svgEl.clientWidth  || 600;
    const height = svgEl.clientHeight || 460;

    const svg = d3.select(svgEl);
    svg.selectAll("*").remove();

    // ── Data ────────────────────────────────────────────────────────────────
    const idSet = new Set(effectiveGoroutines.map((g) => g.goroutine_id));

    const nodes: NodeDatum[] = effectiveGoroutines.map((g) => ({
      id:    g.goroutine_id,
      state: g.state,
      fn:    g.labels?.["function"] ?? "",
      diff:  diffMap.get(g.goroutine_id) ?? "unchanged",
    }));

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
      .attr("fill", "#475569");

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
      .attr("fill", "#7f1d1d");

    // ── Zoom group ──────────────────────────────────────────────────────────
    const g = svg.append("g").attr("class", "dep-zoom-group");

    const zoom = d3
      .zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 8])
      .on("zoom", (event) => { g.attr("transform", event.transform); });

    svg.call(zoom).on("dblclick.zoom", null);

    // ── Links ───────────────────────────────────────────────────────────────
    const link = g
      .append("g")
      .attr("class", "dep-links")
      .selectAll<SVGLineElement, LinkDatum>("line")
      .data(links)
      .join("line")
      .attr("stroke", (d) => d.disappeared ? "#7f1d1d" : "#334155")
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
      .attr("stroke", (d) => (d.id === selectedIdRef.current ? "#f8fafc" : "none"))
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
        d.id === selectedIdRef.current ? "#f8fafc" : "rgba(0,0,0,0.4)"
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
      .attr("fill", "#0f172a")
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
        d.diff === "appeared" ? "#86efac" :
        d.diff === "disappeared" ? "#fca5a5" : "#94a3b8"
      )
      .attr("pointer-events", "none")
      .text((d) => {
        if (!d.fn) return "";
        const short = d.fn.slice(d.fn.lastIndexOf(".") + 1);
        return short.length > 14 ? short.slice(0, 13) + "…" : short;
      });

    // ── Simulation ──────────────────────────────────────────────────────────
    const sim = d3
      .forceSimulation<NodeDatum>(nodes)
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
      });

    return () => { sim.stop(); };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveGoroutines, diffMap]);

  // Update selection highlight without full rebuild.
  useEffect(() => {
    const svgEl = svgRef.current;
    if (!svgEl) return;
    const svg = d3.select(svgEl);

    svg.selectAll<SVGCircleElement, NodeDatum>(".dep-node-circle")
      .attr("stroke", (d) => d.id === selectedId ? "#f8fafc" : "rgba(0,0,0,0.4)")
      .attr("stroke-width", (d) => (d.id === selectedId ? 2.5 : 1));

    svg.selectAll<SVGCircleElement, NodeDatum>(".dep-node-glow")
      .attr("stroke", (d) => (d.id === selectedId ? "#f8fafc" : "none"));
  }, [selectedId]);

  const handleResetZoom = useCallback(() => {
    const svgEl = svgRef.current;
    if (!svgEl) return;
    d3.select(svgEl)
      .transition()
      .duration(400)
      .call(
        d3.zoom<SVGSVGElement, unknown>().transform as any,
        d3.zoomIdentity,
      );
  }, []);

  return (
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
          onClick={handleResetZoom}
          title="Reset zoom to center"
        >
          ⊙ Reset
        </button>
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
  );
}
