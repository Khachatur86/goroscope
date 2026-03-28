import { useEffect, useRef, useState, useCallback, useMemo } from "react";
import type { TimelineSegment } from "../api/client";
import { BG_BASE, BG_PANEL, BG_CARD, TEXT_MUTED, TEXT_SECONDARY, COLOR_EDGE } from "../theme/tokens";

// ── Constants ──────────────────────────────────────────────────────────────────

const BUCKETS    = 100;   // time columns
const CELL_H     = 22;    // px per resource row
const HEADER_H   = 32;    // top axis height
const LEFT_W     = 160;   // label gutter width
const MIN_CELL_W = 4;     // minimum rendered cell width px

// ── Helpers ────────────────────────────────────────────────────────────────────

function heatColor(ratio: number): string {
  // 0 → dark slate, 1 → hot crimson — perceptually even via two-stop blend
  if (ratio <= 0) return BG_BASE;
  const r = Math.round(15  + (244 - 15)  * ratio);
  const g = Math.round(23  + (63  - 23)  * ratio);
  const b = Math.round(42  + (94  - 42)  * ratio);
  return `rgb(${r},${g},${b})`;
}

function formatNS(ns: number): string {
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(1)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(0)}µs`;
  return `${ns}ns`;
}

function shortResource(id: string): string {
  // chan(0x…) → chan·0x1234, sync.(*Mutex) → *Mutex, etc.
  const match = id.match(/0x[0-9a-f]+/i);
  if (match) return id.replace(match[0], "·" + match[0].slice(0, 8));
  return id.length > 24 ? id.slice(0, 22) + "…" : id;
}

// ── Types ──────────────────────────────────────────────────────────────────────

type HeatmapData = {
  resources: string[];    // sorted by total contention desc
  matrix: number[][];     // [resourceIdx][bucketIdx] = waiter count
  maxCount: number;
  minNS: number;
  maxNS: number;
  totalWaitByResource: number[];
};

function buildHeatmap(segments: TimelineSegment[]): HeatmapData | null {
  const relevant = segments.filter(
    (s) => s.resource_id && (s.state === "BLOCKED" || s.state === "WAITING")
  );
  if (relevant.length === 0) return null;

  const minNS = Math.min(...relevant.map((s) => s.start_ns));
  const maxNS = Math.max(...relevant.map((s) => s.end_ns));
  const span  = Math.max(maxNS - minNS, 1);

  // Aggregate total wait per resource to determine row order.
  const waitByRes = new Map<string, number>();
  for (const s of relevant) {
    const id = s.resource_id!;
    waitByRes.set(id, (waitByRes.get(id) ?? 0) + (s.end_ns - s.start_ns));
  }

  const resources = [...waitByRes.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([id]) => id);

  const resourceIdx = new Map(resources.map((r, i) => [r, i]));
  const totalWaitByResource = resources.map((r) => waitByRes.get(r) ?? 0);

  // Count overlapping segments per resource × bucket.
  const matrix: number[][] = resources.map(() => new Array(BUCKETS).fill(0));
  let maxCount = 0;

  for (const s of relevant) {
    const ri  = resourceIdx.get(s.resource_id!)!;
    const b1  = Math.floor(((s.start_ns - minNS) / span) * BUCKETS);
    const b2  = Math.ceil(((s.end_ns   - minNS) / span) * BUCKETS);
    for (let b = Math.max(0, b1); b < Math.min(BUCKETS, b2); b++) {
      matrix[ri][b]++;
      if (matrix[ri][b] > maxCount) maxCount = matrix[ri][b];
    }
  }

  return { resources, matrix, maxCount, minNS, maxNS, totalWaitByResource };
}

// ── Component ──────────────────────────────────────────────────────────────────

type TooltipInfo = {
  x: number;
  y: number;
  resourceId: string;
  count: number;
  bucketNS: number;
  bucketEndNS: number;
  totalWait: number;
};

type Props = {
  segments: TimelineSegment[];
  /** Called when the user clicks a cell. bucketMidNS is the midpoint of the time bucket. */
  onSelectResource?: (resourceId: string, bucketMidNS: number) => void;
};

/** Canvas heatmap: resource rows × time buckets, coloured by waiter count. */
export function ContentionHeatmap({ segments, onSelectResource }: Props) {
  const canvasRef   = useRef<HTMLCanvasElement>(null);
  const dataRef     = useRef<HeatmapData | null>(null);
  const cellWRef    = useRef(0);
  const [tooltip,   setTooltip]   = useState<TooltipInfo | null>(null);
  const [canvasH,   setCanvasH]   = useState(200);

  const data = useMemo(() => buildHeatmap(segments), [segments]);

  // ── Draw ───────────────────────────────────────────────────────────────────
  useEffect(() => {
    dataRef.current = data;
    const canvas = canvasRef.current;
    if (!canvas || !data) return;

    const dpr  = window.devicePixelRatio || 1;
    const W    = canvas.clientWidth || 600;
    const innerW  = W - LEFT_W;
    const cellW   = Math.max(MIN_CELL_W, innerW / BUCKETS);
    cellWRef.current = cellW;

    const rows    = data.resources.length;
    const totalH  = HEADER_H + rows * CELL_H + 1;
    setCanvasH(totalH);

    canvas.width  = Math.round(W * dpr);
    canvas.height = Math.round(totalH * dpr);
    canvas.style.width  = `${W}px`;
    canvas.style.height = `${totalH}px`;

    const ctx = canvas.getContext("2d")!;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, W, totalH);

    // Background
    ctx.fillStyle = BG_BASE;
    ctx.fillRect(0, 0, W, totalH);

    // Label gutter background
    ctx.fillStyle = BG_PANEL;
    ctx.fillRect(0, 0, LEFT_W, totalH);

    // ── Time axis ──────────────────────────────────────────────────────────
    ctx.fillStyle = BG_CARD;
    ctx.fillRect(LEFT_W, 0, innerW, HEADER_H);

    const tickCount = Math.min(10, BUCKETS);
    ctx.fillStyle = TEXT_MUTED;
    ctx.font = "9px monospace";
    ctx.textBaseline = "middle";
    for (let i = 0; i <= tickCount; i++) {
      const ratio = i / tickCount;
      const x = LEFT_W + ratio * innerW;
      const ns = data.minNS + ratio * (data.maxNS - data.minNS);
      ctx.fillStyle = COLOR_EDGE;
      ctx.fillRect(x, HEADER_H - 8, 1, 8);
      if (i < tickCount) {
        ctx.fillStyle = TEXT_MUTED;
        ctx.fillText(formatNS(ns - data.minNS), x + 3, HEADER_H / 2);
      }
    }

    // ── Cells ──────────────────────────────────────────────────────────────
    for (let ri = 0; ri < rows; ri++) {
      const y = HEADER_H + ri * CELL_H;

      // Row label
      ctx.fillStyle = BG_CARD;
      ctx.fillRect(0, y, LEFT_W, CELL_H - 1);
      ctx.fillStyle = TEXT_SECONDARY;
      ctx.font = "9px monospace";
      ctx.textBaseline = "middle";
      ctx.fillText(shortResource(data.resources[ri]), 6, y + CELL_H / 2);

      // Heat cells
      for (let b = 0; b < BUCKETS; b++) {
        const count = data.matrix[ri][b];
        if (count === 0) continue;
        const ratio = count / data.maxCount;
        ctx.fillStyle = heatColor(ratio);
        ctx.fillRect(
          LEFT_W + b * cellW,
          y,
          cellW - 0.5,
          CELL_H - 1,
        );
      }

      // Row separator
      ctx.fillStyle = BG_BASE;
      ctx.fillRect(LEFT_W, y + CELL_H - 1, innerW, 1);
    }
  }, [data]);

  // ── Interaction ────────────────────────────────────────────────────────────
  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const d = dataRef.current;
    if (!d) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const bnd = canvas.getBoundingClientRect();
    const cx  = e.clientX - bnd.left;
    const cy  = e.clientY - bnd.top;

    if (cx < LEFT_W || cy < HEADER_H) { setTooltip(null); return; }

    const ri = Math.floor((cy - HEADER_H) / CELL_H);
    const b  = Math.floor((cx - LEFT_W) / cellWRef.current);
    if (ri < 0 || ri >= d.resources.length || b < 0 || b >= BUCKETS) {
      setTooltip(null);
      return;
    }

    const count  = d.matrix[ri][b];
    const span   = d.maxNS - d.minNS;
    const bucketNS    = d.minNS + (b / BUCKETS) * span;
    const bucketEndNS = d.minNS + ((b + 1) / BUCKETS) * span;

    setTooltip({
      x: e.clientX,
      y: e.clientY,
      resourceId: d.resources[ri],
      count,
      bucketNS,
      bucketEndNS,
      totalWait: d.totalWaitByResource[ri],
    });
  }, []);

  const handleMouseLeave = useCallback(() => setTooltip(null), []);

  const handleClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const d = dataRef.current;
    if (!d) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const bnd = canvas.getBoundingClientRect();
    const cx  = e.clientX - bnd.left;
    const cy  = e.clientY - bnd.top;
    const ri  = Math.floor((cy - HEADER_H) / CELL_H);
    if (ri < 0 || ri >= d.resources.length) return;
    const b = Math.floor((cx - LEFT_W) / cellWRef.current);
    const clampedB = Math.max(0, Math.min(BUCKETS - 1, b));
    const span = d.maxNS - d.minNS;
    const bucketMidNS = d.minNS + ((clampedB + 0.5) / BUCKETS) * span;
    onSelectResource?.(d.resources[ri], bucketMidNS);
  }, [onSelectResource]);

  // ── Render ────────────────────────────────────────────────────────────────
  if (!data) {
    return (
      <div className="heatmap-empty">
        No resource contention recorded.
        <span className="heatmap-empty-hint">
          Contention appears when multiple goroutines block on the same
          mutex, channel, or sync primitive.
        </span>
      </div>
    );
  }

  return (
    <div className="heatmap-container">
      <div className="heatmap-toolbar">
        <span className="heatmap-stat">
          {data.resources.length} resources · {BUCKETS} time buckets ·
          span {formatNS(data.maxNS - data.minNS)}
        </span>
        <span className="heatmap-legend">
          <span className="heatmap-legend-swatch" style={{ background: heatColor(0.1) }} />
          low
          <span className="heatmap-legend-swatch" style={{ background: heatColor(0.5) }} />
          mid
          <span className="heatmap-legend-swatch" style={{ background: heatColor(1) }} />
          peak ({data.maxCount} waiters)
        </span>
      </div>

      <div className="heatmap-scroll">
        <canvas
          ref={canvasRef}
          className="heatmap-canvas"
          style={{ height: canvasH }}
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
          onClick={handleClick}
        />
      </div>

      {tooltip && (
        <div
          className="heatmap-tooltip"
          style={{ left: tooltip.x + 14, top: tooltip.y - 10 }}
        >
          <div className="heatmap-tooltip-resource">{tooltip.resourceId}</div>
          <div className="heatmap-tooltip-row">
            <span>Waiters at this moment</span>
            <strong>{tooltip.count}</strong>
          </div>
          <div className="heatmap-tooltip-row">
            <span>Time window</span>
            <strong>
              {formatNS(tooltip.bucketNS - data.minNS)}–
              {formatNS(tooltip.bucketEndNS - data.minNS)}
            </strong>
          </div>
          <div className="heatmap-tooltip-row">
            <span>Total wait</span>
            <strong>{formatNS(tooltip.totalWait)}</strong>
          </div>
          {onSelectResource && (
            <div className="heatmap-tooltip-hint">Click cell to scrub + filter</div>
          )}
        </div>
      )}
    </div>
  );
}
