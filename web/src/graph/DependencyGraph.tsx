import { useEffect, useRef, useCallback } from "react";
import * as d3 from "d3";
import type { Goroutine } from "../api/client";

const STATE_COLORS: Record<string, string> = {
  RUNNING: "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING: "#f59e0b",
  BLOCKED: "#f43f5e",
  SYSCALL: "#4da6ff",
  DONE: "#4b5563",
};

const NODE_RADIUS = 14;

type Props = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelectGoroutine: (id: number) => void;
};

interface NodeDatum extends d3.SimulationNodeDatum {
  id: number;
  state: string;
  fn: string;
}

interface LinkDatum extends d3.SimulationLinkDatum<NodeDatum> {
  // source/target are overwritten by D3 to NodeDatum after simulation init
}

/** Goroutine spawn-tree as a D3 force-directed DAG. */
export function DependencyGraph({ goroutines, selectedId, onSelectGoroutine }: Props) {
  const svgRef = useRef<SVGSVGElement>(null);
  const onSelectRef = useRef(onSelectGoroutine);
  onSelectRef.current = onSelectGoroutine;

  const selectedIdRef = useRef(selectedId);
  selectedIdRef.current = selectedId;

  // Rebuild the graph whenever the goroutines array changes.
  useEffect(() => {
    const svgEl = svgRef.current;
    if (!svgEl) return;

    const width = svgEl.clientWidth || 600;
    const height = svgEl.clientHeight || 460;

    const svg = d3.select(svgEl);
    svg.selectAll("*").remove();

    // ── Data ──────────────────────────────────────────────────────────────────
    const goroutineIds = new Set(goroutines.map((g) => g.goroutine_id));

    const nodes: NodeDatum[] = goroutines.map((g) => ({
      id: g.goroutine_id,
      state: g.state,
      fn: g.labels?.["function"] ?? "",
    }));

    const links: LinkDatum[] = goroutines
      .filter((g) => g.parent_id && goroutineIds.has(g.parent_id))
      .map((g) => ({
        source: g.parent_id as number,
        target: g.goroutine_id,
      }));

    // ── Arrowhead marker ──────────────────────────────────────────────────────
    svg
      .append("defs")
      .append("marker")
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

    // ── Zoom group ────────────────────────────────────────────────────────────
    const g = svg.append("g").attr("class", "dep-zoom-group");

    const zoom = d3
      .zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 8])
      .on("zoom", (event) => {
        g.attr("transform", event.transform);
      });

    svg.call(zoom).on("dblclick.zoom", null);

    // ── Links ─────────────────────────────────────────────────────────────────
    const link = g
      .append("g")
      .attr("class", "dep-links")
      .selectAll<SVGLineElement, LinkDatum>("line")
      .data(links)
      .join("line")
      .attr("stroke", "#334155")
      .attr("stroke-width", 1.5)
      .attr("marker-end", "url(#dep-arrow)");

    // ── Nodes ─────────────────────────────────────────────────────────────────
    const nodeGroup = g
      .append("g")
      .attr("class", "dep-nodes")
      .selectAll<SVGGElement, NodeDatum>("g")
      .data(nodes)
      .join("g")
      .attr("cursor", "pointer")
      .on("click", (event, d) => {
        event.stopPropagation();
        onSelectRef.current(d.id);
      });

    // Drag to pin/unpin
    const drag = d3
      .drag<SVGGElement, NodeDatum>()
      .on("start", (event, d) => {
        if (!event.active) sim.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on("drag", (event, d) => {
        d.fx = event.x;
        d.fy = event.y;
      })
      .on("end", (event, d) => {
        if (!event.active) sim.alphaTarget(0);
        // Unpin on mouse-up so nodes settle freely
        d.fx = null;
        d.fy = null;
      });

    nodeGroup.call(drag as d3.DragBehavior<SVGGElement, NodeDatum, NodeDatum | d3.SubjectPosition>);

    // Background glow circle for selected node
    nodeGroup
      .append("circle")
      .attr("class", "dep-node-glow")
      .attr("r", NODE_RADIUS + 5)
      .attr("fill", "none")
      .attr("stroke", (d) => (d.id === selectedIdRef.current ? "#f8fafc" : "none"))
      .attr("stroke-width", 2)
      .attr("opacity", 0.5);

    // Main circle
    nodeGroup
      .append("circle")
      .attr("class", "dep-node-circle")
      .attr("r", NODE_RADIUS)
      .attr("fill", (d) => STATE_COLORS[d.state] ?? "#94a3b8")
      .attr("stroke", (d) =>
        d.id === selectedIdRef.current ? "#f8fafc" : "rgba(0,0,0,0.4)"
      )
      .attr("stroke-width", (d) => (d.id === selectedIdRef.current ? 2.5 : 1));

    // G{id} label inside circle
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

    // Function name below the circle
    nodeGroup
      .append("text")
      .attr("text-anchor", "middle")
      .attr("dy", NODE_RADIUS + 13)
      .attr("font-size", "8px")
      .attr("font-family", "sans-serif")
      .attr("fill", "#94a3b8")
      .attr("pointer-events", "none")
      .text((d) => {
        if (!d.fn) return "";
        // Show last component of the fully-qualified function name, max 14 chars
        const short = d.fn.slice(d.fn.lastIndexOf(".") + 1);
        return short.length > 14 ? short.slice(0, 13) + "…" : short;
      });

    // ── Force simulation ──────────────────────────────────────────────────────
    const sim = d3
      .forceSimulation<NodeDatum>(nodes)
      .force(
        "link",
        d3
          .forceLink<NodeDatum, LinkDatum>(links)
          .id((d) => d.id)
          .distance(90)
          .strength(0.5)
      )
      .force("charge", d3.forceManyBody<NodeDatum>().strength(-250))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collide", d3.forceCollide<NodeDatum>(NODE_RADIUS + 10))
      .on("tick", () => {
        link
          .attr("x1", (d) => ((d.source as NodeDatum).x ?? 0))
          .attr("y1", (d) => ((d.source as NodeDatum).y ?? 0))
          .attr("x2", (d) => ((d.target as NodeDatum).x ?? 0))
          .attr("y2", (d) => ((d.target as NodeDatum).y ?? 0));

        nodeGroup.attr(
          "transform",
          (d) => `translate(${d.x ?? 0},${d.y ?? 0})`
        );
      });

    return () => {
      sim.stop();
    };
  }, [goroutines]);

  // When selectedId changes, update highlight without rebuilding the whole graph.
  useEffect(() => {
    const svgEl = svgRef.current;
    if (!svgEl) return;
    const svg = d3.select(svgEl);

    svg
      .selectAll<SVGCircleElement, NodeDatum>(".dep-node-circle")
      .attr("stroke", (d) =>
        d.id === selectedId ? "#f8fafc" : "rgba(0,0,0,0.4)"
      )
      .attr("stroke-width", (d) => (d.id === selectedId ? 2.5 : 1));

    svg
      .selectAll<SVGCircleElement, NodeDatum>(".dep-node-glow")
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
        d3.zoomIdentity
      );
  }, []);

  return (
    <div className="dep-graph-container">
      <div className="dep-graph-toolbar">
        <span className="dep-graph-hint">
          {goroutines.length} goroutines · {
            goroutines.filter((g) => g.parent_id).length
          } spawn edges
        </span>
        <button
          type="button"
          className="timeline-control-button"
          onClick={handleResetZoom}
          title="Reset zoom to center"
        >
          ⊙ Reset
        </button>
      </div>
      <svg
        ref={svgRef}
        className="dep-graph-svg"
      />
    </div>
  );
}
