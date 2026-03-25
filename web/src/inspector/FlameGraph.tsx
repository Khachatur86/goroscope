import { useEffect, useRef, useState, useCallback } from "react";
import type { StackSnapshot } from "../api/client";
import { fetchStacks } from "../api/client";

export type { StackSnapshot };

// ── Constants ──────────────────────────────────────────────────────────────────

const ROW_H = 18;       // px per flame row
const MIN_PX = 1;       // skip rects narrower than this (px)
const PADDING_X = 0;
const PADDING_TOP = 0;

// ── Types ──────────────────────────────────────────────────────────────────────

interface FlameNode {
  name: string;
  file: string;
  value: number;           // number of samples through this node
  children: Map<string, FlameNode>;
}

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
  name: string;
  file: string;
  value: number;
  total: number;
  depth: number;
  node: FlameNode;
}

// ── Helpers ────────────────────────────────────────────────────────────────────

function buildFlameTree(samples: StackSnapshot[]): FlameNode {
  const root: FlameNode = { name: "[all]", file: "", value: samples.length, children: new Map() };
  for (const s of samples) {
    // frames[0] = innermost leaf, frames[N-1] = outermost root.
    // Reverse so we walk root → leaf.
    const frames = s.frames.slice().reverse();
    let node = root;
    for (const f of frames) {
      const key = f.func;
      let child = node.children.get(key);
      if (!child) {
        child = { name: f.func, file: f.file ?? "", value: 0, children: new Map() };
        node.children.set(key, child);
      }
      child.value++;
      node = child;
    }
  }
  return root;
}

/** Hash function name to a warm/cool HSL color. */
function colorFor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) {
    h = Math.imul(31, h) + name.charCodeAt(i) | 0;
  }
  const abs = Math.abs(h);
  const isRuntime = name.startsWith("runtime") || name === "[all]";
  const hue  = isRuntime ? 215 + (abs % 50)  : 15 + (abs % 45);
  const sat  = isRuntime ? 35              : 72;
  const lit  = isRuntime ? 48 + (abs % 12) : 44 + (abs % 14);
  return `hsl(${hue},${sat}%,${lit}%)`;
}

/** Lay out all flame rects via DFS. Returns list sorted by depth asc. */
function layout(
  node: FlameNode,
  x: number,
  w: number,
  depth: number,
  totalSamples: number,
  out: Rect[],
): void {
  if (w < MIN_PX) return;
  out.push({
    x,
    y: depth * ROW_H + PADDING_TOP,
    w,
    h: ROW_H - 1,
    name: node.name,
    file: node.file,
    value: node.value,
    total: totalSamples,
    depth,
    node,
  });
  let cx = x;
  // Stable order: largest subtree first, then alpha.
  const sorted = [...node.children.values()].sort(
    (a, b) => b.value - a.value || a.name.localeCompare(b.name)
  );
  for (const child of sorted) {
    const cw = (child.value / node.value) * w;
    layout(child, cx, cw, depth + 1, totalSamples, out);
    cx += cw;
  }
}

// ── Component ──────────────────────────────────────────────────────────────────

type Props = {
  /** Fetch by goroutine ID (mutually exclusive with externalSamples). */
  goroutineId?: number;
  /** Pre-fetched samples; skips the internal fetch when provided. */
  externalSamples?: StackSnapshot[];
  /** Optional label for the empty-state message. */
  emptyHint?: string;
};

type Tooltip = { x: number; y: number; rect: Rect } | null;

/** Flame graph of all stack snapshots for a goroutine, or an external sample set. */
export function FlameGraph({ goroutineId, externalSamples, emptyHint }: Props) {
  const canvasRef  = useRef<HTMLCanvasElement>(null);
  const rectsRef   = useRef<Rect[]>([]);
  const rootRef    = useRef<FlameNode | null>(null);

  const [loading,   setLoading]   = useState(externalSamples === undefined);
  const [samples,   setSamples]   = useState<StackSnapshot[]>(externalSamples ?? []);
  const [zoomNode,  setZoomNode]  = useState<FlameNode | null>(null);
  const [zoomPath,  setZoomPath]  = useState<string[]>([]);
  const [tooltip,   setTooltip]   = useState<Tooltip>(null);
  const [canvasH,   setCanvasH]   = useState(200);

  // ── Fetch (only when using goroutineId mode) ────────────────────────────────
  useEffect(() => {
    if (externalSamples !== undefined) {
      setSamples(externalSamples);
      setLoading(false);
      setZoomNode(null);
      setZoomPath([]);
      return;
    }
    if (goroutineId === undefined) return;
    setLoading(true);
    setSamples([]);
    setZoomNode(null);
    setZoomPath([]);
    fetchStacks(goroutineId).then((s) => {
      setSamples(s);
      setLoading(false);
    });
  }, [goroutineId, externalSamples]);

  // ── Draw ───────────────────────────────────────────────────────────────────
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    if (loading || samples.length === 0) {
      const ctx = canvas.getContext("2d")!;
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      return;
    }

    const root = buildFlameTree(samples);
    rootRef.current = root;

    const focusNode = zoomNode ?? root;
    const rects: Rect[] = [];
    const totalSamples = root.value;

    const W = canvas.clientWidth || 400;
    layout(focusNode, PADDING_X, W - PADDING_X * 2, 0, totalSamples, rects);

    // Canvas height = number of rows * ROW_H + some padding
    const maxDepth = rects.reduce((m, r) => Math.max(m, r.depth), 0);
    const needed = (maxDepth + 1) * ROW_H + PADDING_TOP + 4;
    setCanvasH(needed);
    canvas.width  = W * devicePixelRatio;
    canvas.height = needed * devicePixelRatio;
    canvas.style.width  = `${W}px`;
    canvas.style.height = `${needed}px`;

    const ctx = canvas.getContext("2d")!;
    ctx.scale(devicePixelRatio, devicePixelRatio);
    ctx.clearRect(0, 0, W, needed);

    for (const r of rects) {
      if (r.w < MIN_PX) continue;
      ctx.fillStyle = colorFor(r.name);
      ctx.fillRect(r.x, r.y, r.w - 0.5, r.h);

      if (r.w > 20) {
        ctx.fillStyle = "#0f172a";
        ctx.font = `10px monospace`;
        ctx.textBaseline = "middle";
        const label = r.name.slice(r.name.lastIndexOf(".") + 1);
        const maxChars = Math.floor((r.w - 4) / 6);
        const text = label.length > maxChars ? label.slice(0, maxChars - 1) + "…" : label;
        ctx.fillText(text, r.x + 3, r.y + ROW_H / 2);
      }
    }

    rectsRef.current = rects;
  }, [samples, zoomNode, loading]);

  // ── Mouse ──────────────────────────────────────────────────────────────────
  const hitTest = useCallback((cx: number, cy: number): Rect | null => {
    for (let i = rectsRef.current.length - 1; i >= 0; i--) {
      const r = rectsRef.current[i];
      if (cx >= r.x && cx <= r.x + r.w && cy >= r.y && cy <= r.y + r.h) {
        return r;
      }
    }
    return null;
  }, []);

  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const bnd = canvas.getBoundingClientRect();
    const cx = e.clientX - bnd.left;
    const cy = e.clientY - bnd.top;
    const hit = hitTest(cx, cy);
    if (hit) {
      setTooltip({ x: e.clientX, y: e.clientY, rect: hit });
    } else {
      setTooltip(null);
    }
  }, [hitTest]);

  const handleMouseLeave = useCallback(() => setTooltip(null), []);

  const handleClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const bnd = canvas.getBoundingClientRect();
    const cx = e.clientX - bnd.left;
    const cy = e.clientY - bnd.top;
    const hit = hitTest(cx, cy);
    if (!hit) return;

    if (hit.node === (zoomNode ?? rootRef.current)) {
      // Clicking root / current focus → zoom out one level
      return;
    }
    // Build breadcrumb path by finding ancestors
    const path = [...zoomPath, hit.name];
    setZoomNode(hit.node);
    setZoomPath(path);
    setTooltip(null);
  }, [hitTest, zoomNode, zoomPath]);

  const resetZoom = useCallback(() => {
    setZoomNode(null);
    setZoomPath([]);
    setTooltip(null);
  }, []);

  // ── Render ────────────────────────────────────────────────────────────────
  if (loading) {
    return <div className="flame-empty">Loading stacks…</div>;
  }
  if (samples.length === 0) {
    return (
      <div className="flame-empty">
        {emptyHint ?? "No stack snapshots recorded for this goroutine."}
        <br />
        <span className="flame-empty-hint">
          Stack snapshots are captured from runtime/trace events.
        </span>
      </div>
    );
  }

  const pct = (v: number, total: number) =>
    total > 0 ? ((v / total) * 100).toFixed(1) : "0.0";

  return (
    <div className="flame-container">
      {/* Toolbar */}
      <div className="flame-toolbar">
        <span className="flame-stat">{samples.length} samples</span>
        {zoomPath.length > 0 && (
          <div className="flame-breadcrumb">
            <button
              type="button"
              className="flame-breadcrumb-item flame-breadcrumb-root"
              onClick={resetZoom}
              title="Reset zoom"
            >
              [all]
            </button>
            {zoomPath.map((name, i) => {
              const short = name.slice(name.lastIndexOf(".") + 1);
              return (
                <span key={i} className="flame-breadcrumb-sep">
                  › <span className="flame-breadcrumb-item">{short}</span>
                </span>
              );
            })}
          </div>
        )}
        {zoomPath.length > 0 && (
          <button
            type="button"
            className="flame-reset-btn"
            onClick={resetZoom}
            title="Reset zoom"
          >
            ⊙ Reset
          </button>
        )}
      </div>

      {/* Canvas */}
      <div className="flame-canvas-wrap" style={{ minHeight: canvasH }}>
        <canvas
          ref={canvasRef}
          className="flame-canvas"
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
          onClick={handleClick}
        />
      </div>

      {/* Tooltip */}
      {tooltip && (
        <div
          className="flame-tooltip"
          style={{ left: tooltip.x + 12, top: tooltip.y - 10 }}
        >
          <div className="flame-tooltip-name">{tooltip.rect.name}</div>
          {tooltip.rect.file && (
            <div className="flame-tooltip-file">{tooltip.rect.file}</div>
          )}
          <div className="flame-tooltip-stats">
            {tooltip.rect.value} sample{tooltip.rect.value !== 1 ? "s" : ""}
            {" "}·{" "}
            {pct(tooltip.rect.value, tooltip.rect.total)}%
          </div>
          <div className="flame-tooltip-hint">Click to zoom</div>
        </div>
      )}
    </div>
  );
}
