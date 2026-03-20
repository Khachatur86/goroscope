import { useEffect, useRef, useState, useCallback } from "react";
import type { Goroutine, TimelineSegment, ProcessorSegment } from "../api/client";

const COLORS: Record<string, string> = {
  RUNNING: "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING: "#f59e0b",
  BLOCKED: "#f43f5e",
  SYSCALL: "#4da6ff",
  DONE: "#4b5563",
};

function formatDuration(ns: number): string {
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(2)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(2)}µs`;
  return `${ns}ns`;
}

/**
 * Format a nanosecond offset for axis tick labels.
 * Strips unnecessary trailing zeros so ticks read "10ms" not "10.00ms".
 */
function formatAxisLabel(ns: number): string {
  if (ns === 0) return "0";
  const stripZeros = (n: number) => parseFloat(n.toPrecision(4)).toString();
  if (ns >= 1e9) return `${stripZeros(ns / 1e9)}s`;
  if (ns >= 1e6) return `${stripZeros(ns / 1e6)}ms`;
  if (ns >= 1e3) return `${stripZeros(ns / 1e3)}µs`;
  return `${ns}ns`;
}

/**
 * Return "nice" tick positions within [visibleStart, visibleStart+visibleSpan].
 * Step size is rounded to the nearest 1/2/5 × 10^n so labels land on clean numbers.
 */
function computeNiceTicks(visibleStart: number, visibleSpan: number, targetCount: number): number[] {
  if (visibleSpan <= 0 || targetCount < 1) return [];
  const rawStep = visibleSpan / targetCount;
  const magnitude = Math.pow(10, Math.floor(Math.log10(rawStep)));
  const normalized = rawStep / magnitude;
  let niceStep: number;
  if (normalized < 1.5) niceStep = magnitude;
  else if (normalized < 3.5) niceStep = 2 * magnitude;
  else if (normalized < 7.5) niceStep = 5 * magnitude;
  else niceStep = 10 * magnitude;

  const firstTick = Math.ceil(visibleStart / niceStep) * niceStep;
  const ticks: number[] = [];
  for (let t = firstTick; t <= visibleStart + visibleSpan + niceStep * 0.001; t += niceStep) {
    if (t <= visibleStart + visibleSpan) ticks.push(t);
  }
  return ticks;
}

const METRICS = {
  axisHeight: 38,
  rowHeight: 28,
  labelGutterWidth: 182,
  leftPadding: 14,
  rightPadding: 18,
  pRowH: 18,
  pLabelH: 14,
  pGap: 2,
};

/** Viewport height for the scrollable goroutine rows area (px). */
const ROW_AREA_VIEWPORT_HEIGHT = 420;

function goroutineHue(id: number): number {
  const hues = [195, 30, 270, 140, 355, 60, 310, 170, 80, 230, 15, 330];
  return hues[Number(id) % hues.length];
}

type Props = {
  goroutines: Goroutine[];
  segments: TimelineSegment[];
  processorSegments?: ProcessorSegment[];
  selectedId: number | null;
  onSelectGoroutine: (id: number, segment?: TimelineSegment) => void;
  zoomToSelected: boolean;
  onHoverSegment?: (seg: TimelineSegment | null) => void;
  /** Controlled mode: parent owns zoom/pan state for minimap sync */
  zoomLevel?: number;
  panOffsetNS?: number;
  onZoomPanChange?: (zoomLevel: number, panOffsetNS: number) => void;
  /** When set, goroutines NOT in this set are dimmed. */
  highlightedIds?: Set<number> | null;
  /** When true, drag creates a time-range brush instead of panning. */
  brushMode?: boolean;
  /** The currently committed brush range [startNS, endNS], drawn as an overlay. */
  brushRange?: [number, number] | null;
  /** Fired when the user drags a new brush or clears it (null). */
  onBrushChange?: (range: [number, number] | null) => void;
  /**
   * Time-scrubber: absolute NS timestamp of the user-selected moment.
   * Drawn as an amber cursor spanning both the axis and the goroutine rows.
   */
  scrubTimeNS?: number | null;
  /** Fired when the user clicks the axis to set (or double-clicks to clear) the scrub time. */
  onScrubChange?: (timeNS: number | null) => void;
};

export function TimelineCanvas({
  goroutines,
  segments,
  processorSegments = [],
  selectedId,
  onSelectGoroutine,
  zoomToSelected,
  onHoverSegment,
  zoomLevel: controlledZoom,
  panOffsetNS: controlledPan,
  onZoomPanChange,
  highlightedIds,
  brushMode = false,
  brushRange,
  onBrushChange,
  scrubTimeNS,
  onScrubChange,
}: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [internalZoom, setInternalZoom] = useState(1);
  const [internalPan, setInternalPan] = useState(0);

  const isControlled = controlledZoom !== undefined && controlledPan !== undefined;
  const zoomLevel = isControlled ? controlledZoom : internalZoom;
  const panOffsetNS = isControlled ? controlledPan : internalPan;
  const setZoomLevel = useCallback(
    (v: number) => {
      if (isControlled) onZoomPanChange?.(v, panOffsetNS);
      else setInternalZoom(v);
    },
    [isControlled, panOffsetNS, onZoomPanChange]
  );
  const setPanOffsetNS = useCallback(
    (v: number) => {
      if (isControlled) onZoomPanChange?.(zoomLevel, v);
      else setInternalPan(v);
    },
    [isControlled, zoomLevel, onZoomPanChange]
  );
  const [isDragging, setIsDragging] = useState(false);
  const [hasDragged, setHasDragged] = useState(false);
  const dragStartX = useRef(0);
  const dragStartPanNS = useRef(0);
  const [hoveredSegment, setHoveredSegment] = useState<TimelineSegment | null>(null);
  const [tooltipPos, setTooltipPos] = useState({ x: 0, y: 0 });
  const [rowScrollTop, setRowScrollTop] = useState(0);
  const scrollContainerRef = useRef<HTMLDivElement>(null);

  // Brush drag state (pixels from canvas left edge → converted to NS on commit)
  const [brushDragStartPx, setBrushDragStartPx] = useState<number | null>(null);
  const [brushDragCurrentPx, setBrushDragCurrentPx] = useState<number | null>(null);

  // Ghost cursor: NS position of mouse hover over the axis (not committed).
  // Stored in state so renderAxis reacts to it; axis redraws are fast enough
  // for per-pixel mouse tracking.
  const [ghostTimeNS, setGhostTimeNS] = useState<number | null>(null);

  const fullMinStart = Math.min(...segments.map((s) => s.start_ns));
  const fullMaxEnd = Math.max(...segments.map((s) => s.end_ns));
  const fullSpan = Math.max(fullMaxEnd - fullMinStart, 1);

  // Sync zoom/pan when zoomToSelected changes (only in uncontrolled mode)
  useEffect(() => {
    if (isControlled) return;
    if (zoomToSelected && selectedId) {
      const selectedSegs = segments.filter((s) => s.goroutine_id === selectedId);
      if (selectedSegs.length > 0) {
        const minStart = Math.min(...selectedSegs.map((s) => s.start_ns));
        const maxEnd = Math.max(...selectedSegs.map((s) => s.end_ns));
        const padding = Math.max((maxEnd - minStart) * 0.1, 1);
        const visibleStart = Math.max(fullMinStart, minStart - padding);
        const visibleEnd = Math.min(fullMaxEnd, maxEnd + padding);
        const visibleSpan = visibleEnd - visibleStart;
        setPanOffsetNS(visibleStart - fullMinStart);
        setZoomLevel(fullSpan / visibleSpan);
        return;
      }
    }
    setZoomLevel(1);
    setPanOffsetNS(0);
  }, [isControlled, zoomToSelected, selectedId, fullMinStart, fullSpan, segments]);

  const visibleSpan = fullSpan / zoomLevel;
  const visibleStart = fullMinStart + Math.max(0, Math.min(fullSpan - visibleSpan, panOffsetNS));

  const plotLeft = METRICS.labelGutterWidth + METRICS.leftPadding;

  /** Convert a canvas-relative X pixel to an absolute NS timestamp. */
  const pxToNS = useCallback(
    (px: number): number => {
      const container = containerRef.current;
      if (!container) return visibleStart;
      const innerWidth = getInnerWidth(container.clientWidth);
      const offset = METRICS.labelGutterWidth + METRICS.leftPadding;
      const clamped = Math.max(0, Math.min(innerWidth, px - offset));
      return visibleStart + (clamped / innerWidth) * visibleSpan;
    },
    [visibleStart, visibleSpan]
  );
  const getInnerWidth = (width: number) =>
    Math.max(1, width - METRICS.labelGutterWidth - METRICS.leftPadding - METRICS.rightPadding);

  const processorIds = [...new Set(processorSegments.map((s) => s.processor_id))].sort((a, b) => a - b);
  const numPs = processorIds.length;
  const gmpH =
    numPs > 0
      ? METRICS.pLabelH + numPs * METRICS.pRowH + METRICS.pGap + 8
      : 0;
  const gTop = METRICS.axisHeight + gmpH;
  const totalRowsHeight = goroutines.length * METRICS.rowHeight;
  const rowAreaHeight = Math.min(ROW_AREA_VIEWPORT_HEIGHT, totalRowsHeight + 16);

  // Virtual row range: only draw goroutines whose Y range is within the visible viewport
  const firstVisibleIndex = Math.max(
    0,
    Math.floor(rowScrollTop / METRICS.rowHeight)
  );
  const lastVisibleIndex = Math.min(
    goroutines.length - 1,
    Math.floor((rowScrollTop + rowAreaHeight) / METRICS.rowHeight)
  );

  const renderAxis = useCallback(() => {
    const canvas = canvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    const width = Math.max(320, container.clientWidth);
    const height = gTop;
    const dpr = window.devicePixelRatio || 1;
    const innerWidth = getInnerWidth(width);

    if (canvas.width !== Math.floor(width * dpr)) canvas.width = Math.floor(width * dpr);
    if (canvas.height !== Math.floor(height * dpr)) canvas.height = Math.floor(height * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = "#0d1117";
    ctx.fillRect(0, 0, width, height);

    // Axis baseline
    ctx.strokeStyle = "rgba(219, 228, 238, 0.14)";
    ctx.beginPath();
    ctx.moveTo(plotLeft, METRICS.axisHeight - 10);
    ctx.lineTo(width - METRICS.rightPadding, METRICS.axisHeight - 10);
    ctx.stroke();

    const targetTickCount = Math.max(4, Math.floor(innerWidth / 90));
    const ticks = computeNiceTicks(visibleStart, visibleSpan, targetTickCount);
    ctx.font = '11px "IBM Plex Mono", monospace';
    ticks.forEach((tick) => {
      const ratio = (tick - visibleStart) / visibleSpan;
      if (ratio < 0 || ratio > 1) return;
      const x = plotLeft + ratio * innerWidth;
      ctx.strokeStyle = "rgba(219, 228, 238, 0.12)";
      ctx.beginPath();
      ctx.moveTo(x, METRICS.axisHeight - 18);
      ctx.lineTo(x, height);
      ctx.stroke();
      const label = formatAxisLabel(tick - fullMinStart);
      const labelWidth = ctx.measureText(label).width;
      const labelX = Math.min(x + 4, width - METRICS.rightPadding - labelWidth - 2);
      ctx.fillStyle = "#dbe4ee";
      ctx.fillText(label, labelX, 20);
    });

    // GMP strip
    if (numPs > 0) {
      const gmpTop = METRICS.axisHeight + 6;
      ctx.fillStyle = "rgba(219,228,238,0.38)";
      ctx.font = '10px "IBM Plex Mono", monospace';
      ctx.fillText("GMP", 4, gmpTop + 10);

      const pLanesTop = gmpTop + METRICS.pLabelH;
      processorIds.forEach((pid, pIdx) => {
        const py = pLanesTop + pIdx * METRICS.pRowH;
        ctx.fillStyle = "rgba(255,255,255,0.025)";
        ctx.fillRect(plotLeft, py, innerWidth, METRICS.pRowH - 1);
        ctx.fillStyle = "rgba(219,228,238,0.50)";
        ctx.font = '10px "IBM Plex Mono", monospace';
        ctx.fillText(`P${pid}`, plotLeft - 54, py + 11);

        processorSegments
          .filter((s) => s.processor_id === pid)
          .filter((s) => s.start_ns < visibleStart + visibleSpan && s.end_ns > visibleStart)
          .forEach((seg) => {
            const rawX = plotLeft + ((seg.start_ns - visibleStart) / visibleSpan) * innerWidth;
            const rawX2 = plotLeft + ((seg.end_ns - visibleStart) / visibleSpan) * innerWidth;
            const cx = Math.max(plotLeft, Math.min(rawX, plotLeft + innerWidth));
            const cx2 = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
            const cw = Math.max(cx2 - cx, rawX2 > rawX ? 1 : 0);
            if (cw === 0) return;
            ctx.fillStyle = `hsl(${goroutineHue(seg.goroutine_id)}, 70%, 58%)`;
            ctx.fillRect(cx, py + 1, cw, METRICS.pRowH - 3);
            if (cw > 2) {
              ctx.fillStyle = "rgba(255,255,255,0.20)";
              ctx.fillRect(cx + 1, py + 1, cw - 2, 1);
            }
            if (cw > 28) {
              ctx.fillStyle = "rgba(255,255,255,0.90)";
              ctx.font = '9px "IBM Plex Mono", monospace';
              ctx.fillText(`G${seg.goroutine_id}`, cx + 3, py + 11);
            }
          });
        ctx.strokeStyle = "rgba(219,228,238,0.06)";
        ctx.beginPath();
        ctx.moveTo(plotLeft, py + METRICS.pRowH - 0.5);
        ctx.lineTo(plotLeft + innerWidth, py + METRICS.pRowH - 0.5);
        ctx.stroke();
      });
    }

    ctx.fillStyle = "rgba(2, 6, 23, 0.48)";
    ctx.fillRect(0, METRICS.axisHeight, plotLeft - 8, height - METRICS.axisHeight);
    ctx.strokeStyle = "rgba(219, 228, 238, 0.10)";
    ctx.beginPath();
    ctx.moveTo(plotLeft - 0.5, METRICS.axisHeight - 18);
    ctx.lineTo(plotLeft - 0.5, height);
    ctx.stroke();

    // ── Cursor overlay ───────────────────────────────────────────────────
    // Ghost cursor: thin dashed sky-blue line following mouse hover on axis.
    if (ghostTimeNS != null) {
      const ratio = (ghostTimeNS - visibleStart) / visibleSpan;
      const gx = plotLeft + ratio * innerWidth;
      if (gx >= plotLeft && gx <= width - METRICS.rightPadding) {
        ctx.save();
        ctx.strokeStyle = "rgba(56, 189, 248, 0.55)";
        ctx.lineWidth = 1;
        ctx.setLineDash([4, 4]);
        ctx.beginPath();
        ctx.moveTo(gx, 0);
        ctx.lineTo(gx, height);
        ctx.stroke();
        ctx.setLineDash([]);
        // Time label above the axis baseline
        const lbl = `T+${formatAxisLabel(ghostTimeNS - fullMinStart)}`;
        ctx.font = '10px "IBM Plex Mono", monospace';
        const lw = ctx.measureText(lbl).width;
        const lx = Math.min(gx + 4, width - METRICS.rightPadding - lw - 2);
        ctx.fillStyle = "rgba(56, 189, 248, 0.9)";
        ctx.fillText(lbl, lx, 14);
        ctx.restore();
      }
    }

    // Scrub cursor: solid amber line at the committed scrub time.
    if (scrubTimeNS != null) {
      const ratio = (scrubTimeNS - visibleStart) / visibleSpan;
      const sx = plotLeft + ratio * innerWidth;
      if (sx >= plotLeft && sx <= width - METRICS.rightPadding) {
        ctx.save();
        ctx.strokeStyle = "rgba(250, 204, 21, 0.9)";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.moveTo(sx, 0);
        ctx.lineTo(sx, height);
        ctx.stroke();
        // Label with background
        const lbl = `⏱ T+${formatAxisLabel(scrubTimeNS - fullMinStart)}`;
        ctx.font = 'bold 10px "IBM Plex Mono", monospace';
        const lw = ctx.measureText(lbl).width + 8;
        const lx = Math.min(sx + 4, width - METRICS.rightPadding - lw - 2);
        ctx.fillStyle = "rgba(0, 0, 0, 0.55)";
        ctx.fillRect(lx - 2, 2, lw, 16);
        ctx.fillStyle = "rgba(250, 204, 21, 0.95)";
        ctx.fillText(lbl, lx + 2, 14);
        ctx.restore();
      }
    }
  }, [
    visibleStart,
    visibleSpan,
    fullMinStart,
    processorSegments,
    processorIds,
    numPs,
    gTop,
    ghostTimeNS,
    scrubTimeNS,
  ]);

  const rowsCanvasRef = useRef<HTMLCanvasElement>(null);

  const renderRows = useCallback(() => {
    const canvas = rowsCanvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    const width = Math.max(320, container.clientWidth);
    const height = rowAreaHeight;
    const dpr = window.devicePixelRatio || 1;
    const innerWidth = getInnerWidth(width);

    if (canvas.width !== Math.floor(width * dpr)) canvas.width = Math.floor(width * dpr);
    if (canvas.height !== Math.floor(height * dpr)) canvas.height = Math.floor(height * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = "#0d1117";
    ctx.fillRect(0, 0, width, height);

    ctx.fillStyle = "rgba(2, 6, 23, 0.48)";
    ctx.fillRect(0, 0, plotLeft - 8, height);
    ctx.strokeStyle = "rgba(219, 228, 238, 0.10)";
    ctx.beginPath();
    ctx.moveTo(plotLeft - 0.5, 0);
    ctx.lineTo(plotLeft - 0.5, height);
    ctx.stroke();

    // Virtual rows: only draw goroutines in the visible viewport range
    for (let index = firstVisibleIndex; index <= lastVisibleIndex; index++) {
      const g = goroutines[index];
      const drawY = index * METRICS.rowHeight - rowScrollTop;
      const isSelected = g.goroutine_id === selectedId;
      const isDimmed = highlightedIds !== null && highlightedIds !== undefined && !highlightedIds.has(g.goroutine_id);

      if (index % 2 === 0) {
        ctx.fillStyle = "rgba(255, 255, 255, 0.028)";
        ctx.fillRect(0, drawY, width, METRICS.rowHeight);
      }
      if (isSelected) {
        ctx.fillStyle = "rgba(96, 165, 250, 0.10)";
        ctx.fillRect(0, drawY, width, METRICS.rowHeight);
        ctx.fillStyle = "rgba(125, 211, 252, 0.95)";
        ctx.fillRect(0, drawY + 2, 4, METRICS.rowHeight - 4);
      }
      if (isDimmed) {
        ctx.fillStyle = "rgba(13, 17, 23, 0.68)";
        ctx.fillRect(0, drawY, width, METRICS.rowHeight);
      }
      ctx.strokeStyle = "rgba(219, 228, 238, 0.13)";
      ctx.beginPath();
      ctx.moveTo(0, drawY + METRICS.rowHeight - 0.5);
      ctx.lineTo(width - METRICS.rightPadding, drawY + METRICS.rowHeight - 0.5);
      ctx.stroke();

      ctx.globalAlpha = isDimmed ? 0.28 : 1;
      ctx.fillStyle = isSelected ? "#f8fafc" : "#9fb3c8";
      ctx.font = '12px "IBM Plex Mono", monospace';
      ctx.fillText(`G${g.goroutine_id}`, 14, drawY + 12);
      ctx.fillStyle = isSelected ? "rgba(219, 228, 238, 0.74)" : "rgba(159, 179, 200, 0.46)";
      ctx.font = '11px "IBM Plex Mono", monospace';
      const fn = (g.labels?.function || "unknown").slice(0, 20);
      ctx.fillText(fn, 14, drawY + 23);

      segments
        .filter((s) => s.goroutine_id === g.goroutine_id)
        .forEach((seg) => {
          const rawX = plotLeft + ((seg.start_ns - visibleStart) / visibleSpan) * innerWidth;
          const rawX2 = plotLeft + ((seg.end_ns - visibleStart) / visibleSpan) * innerWidth;
          const cx = Math.max(plotLeft, Math.min(rawX, plotLeft + innerWidth));
          const cx2 = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
          const cw = Math.max(cx2 - cx, rawX2 > rawX ? 2 : 0);
          if (cw === 0) return;

          const barHeight = 20;
          const barY = drawY + 4;
          const isHovered =
            hoveredSegment?.goroutine_id === seg.goroutine_id &&
            hoveredSegment?.start_ns === seg.start_ns &&
            hoveredSegment?.end_ns === seg.end_ns;

          ctx.fillStyle = COLORS[seg.state] ?? "#94a3b8";
          ctx.fillRect(cx, barY, cw, barHeight);
          if (cw > 2) {
            ctx.fillStyle = "rgba(255, 255, 255, 0.22)";
            ctx.fillRect(cx + 1, barY, cw - 2, 1);
          }
          if (isSelected || isHovered) {
            ctx.lineWidth = isHovered ? 2 : 1.5;
            ctx.strokeStyle = isHovered ? "rgba(255, 255, 255, 0.95)" : "rgba(186, 230, 253, 0.72)";
            ctx.strokeRect(cx, barY, cw, barHeight);
          }
        });
      ctx.globalAlpha = 1;
    }

    // Draw brush overlay (active drag or committed brushRange)
    const drawBrush = (startNS: number, endNS: number) => {
      const container2 = containerRef.current;
      if (!container2) return;
      const innerWidth = getInnerWidth(container2.clientWidth);
      const bx1 = plotLeft + ((Math.min(startNS, endNS) - visibleStart) / visibleSpan) * innerWidth;
      const bx2 = plotLeft + ((Math.max(startNS, endNS) - visibleStart) / visibleSpan) * innerWidth;
      const bw = Math.max(bx2 - bx1, 2);
      const clampedX = Math.max(plotLeft, bx1);
      const clampedW = Math.min(plotLeft + innerWidth, bx1 + bw) - clampedX;
      if (clampedW <= 0) return;
      ctx.fillStyle = "rgba(56, 189, 248, 0.12)";
      ctx.fillRect(clampedX, 0, clampedW, height);
      ctx.strokeStyle = "rgba(56, 189, 248, 0.7)";
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.moveTo(clampedX, 0);
      ctx.lineTo(clampedX, height);
      ctx.moveTo(clampedX + clampedW, 0);
      ctx.lineTo(clampedX + clampedW, height);
      ctx.stroke();
      ctx.lineWidth = 1;
    };

    if (brushDragStartPx !== null && brushDragCurrentPx !== null) {
      drawBrush(pxToNS(brushDragStartPx), pxToNS(brushDragCurrentPx));
    } else if (brushRange) {
      drawBrush(brushRange[0], brushRange[1]);
    }

    // Scrub cursor in rows area: dashed amber line spanning full height.
    if (scrubTimeNS != null) {
      const ratio = (scrubTimeNS - visibleStart) / visibleSpan;
      const sx = plotLeft + ratio * innerWidth;
      if (sx >= plotLeft && sx <= width - METRICS.rightPadding) {
        ctx.save();
        ctx.strokeStyle = "rgba(250, 204, 21, 0.38)";
        ctx.lineWidth = 1.5;
        ctx.setLineDash([4, 3]);
        ctx.beginPath();
        ctx.moveTo(sx, 0);
        ctx.lineTo(sx, height);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.restore();
      }
    }
  }, [
    goroutines,
    segments,
    selectedId,
    highlightedIds,
    visibleStart,
    visibleSpan,
    hoveredSegment,
    firstVisibleIndex,
    lastVisibleIndex,
    rowScrollTop,
    rowAreaHeight,
    brushDragStartPx,
    brushDragCurrentPx,
    brushRange,
    pxToNS,
    scrubTimeNS,
  ]);

  useEffect(() => {
    renderAxis();
  }, [renderAxis]);

  useEffect(() => {
    renderRows();
  }, [renderRows]);

  useEffect(() => {
    const resizeObserver = new ResizeObserver(() => {
      renderAxis();
      renderRows();
    });
    const el = containerRef.current;
    if (el) resizeObserver.observe(el);
    return () => resizeObserver.disconnect();
  }, [renderAxis, renderRows]);

  useEffect(() => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const onScroll = () => {
      setRowScrollTop(el.scrollTop);
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  const hitTest = useCallback(
    (clientX: number, clientY: number): TimelineSegment | null => {
      const canvas = rowsCanvasRef.current;
      const container = containerRef.current;
      if (!canvas || !container) return null;

      const rect = canvas.getBoundingClientRect();
      const x = clientX - rect.left;
      const y = clientY - rect.top;
      const width = container.clientWidth;
      const innerWidth = getInnerWidth(width);

      const rowIndex = Math.floor((y + rowScrollTop) / METRICS.rowHeight);
      if (rowIndex < 0 || rowIndex >= goroutines.length) return null;
      const g = goroutines[rowIndex];

      const segs = segments.filter((s) => s.goroutine_id === g.goroutine_id);
      const drawY = rowIndex * METRICS.rowHeight - rowScrollTop;
      const barY = drawY + 4;
      for (const seg of segs) {
        const rawX = plotLeft + ((seg.start_ns - visibleStart) / visibleSpan) * innerWidth;
        const rawX2 = plotLeft + ((seg.end_ns - visibleStart) / visibleSpan) * innerWidth;
        const cx = Math.max(plotLeft, Math.min(rawX, plotLeft + innerWidth));
        const cx2 = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
        if (x >= cx && x <= cx2 && y >= barY && y <= barY + 20) return seg;
      }
      return null;
    },
    [goroutines, segments, visibleStart, visibleSpan, rowScrollTop]
  );

  const handleWheel = useCallback(
    (e: React.WheelEvent) => {
      e.preventDefault();
      const container = containerRef.current;
      if (!container || fullSpan <= 1) return;

      const rect = canvasRef.current?.getBoundingClientRect();
      if (!rect) return;
      const canvasX = e.clientX - rect.left;
      const width = container.clientWidth;
      const innerWidth = getInnerWidth(width);
      const fx = Math.max(0, Math.min(1, (canvasX - plotLeft) / innerWidth));

      const cursorNS = panOffsetNS + fx * visibleSpan;
      const zoomFactor = e.deltaY < 0 ? 1.3 : 1 / 1.3;
      const newZoom = Math.max(1, Math.min(500, zoomLevel * zoomFactor));
      const newVisibleSpan = fullSpan / newZoom;
      let newPan = cursorNS - fx * newVisibleSpan;
      newPan = Math.max(0, Math.min(fullSpan - newVisibleSpan, newPan));

      setZoomLevel(newZoom);
      setPanOffsetNS(newPan);
    },
    [fullSpan, zoomLevel, panOffsetNS, visibleSpan]
  );

  const canvasRelativeX = useCallback((clientX: number): number => {
    const canvas = rowsCanvasRef.current;
    if (!canvas) return 0;
    return clientX - canvas.getBoundingClientRect().left;
  }, []);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      setHoveredSegment(null);
      onHoverSegment?.(null);
      if (brushMode) {
        const px = canvasRelativeX(e.clientX);
        setBrushDragStartPx(px);
        setBrushDragCurrentPx(px);
        setIsDragging(true);
        setHasDragged(false);
      } else {
        setIsDragging(true);
        setHasDragged(false);
        dragStartX.current = e.clientX;
        dragStartPanNS.current = panOffsetNS;
      }
    },
    [panOffsetNS, onHoverSegment, brushMode, canvasRelativeX]
  );

  const handleMouseMove = useCallback(
    (e: React.MouseEvent) => {
      if (isDragging) {
        setHasDragged(true);
        if (brushMode) {
          setBrushDragCurrentPx(canvasRelativeX(e.clientX));
        } else {
          const dx = e.clientX - dragStartX.current;
          const container = containerRef.current;
          if (!container) return;
          const innerWidth = getInnerWidth(container.clientWidth);
          const nsPerPx = visibleSpan / innerWidth;
          const newPan = dragStartPanNS.current - dx * nsPerPx;
          setPanOffsetNS(Math.max(0, Math.min(fullSpan - visibleSpan, newPan)));
        }
      } else {
        const seg = hitTest(e.clientX, e.clientY);
        setHoveredSegment(seg);
        setTooltipPos({ x: e.clientX, y: e.clientY });
        onHoverSegment?.(seg ?? null);
      }
    },
    [isDragging, brushMode, canvasRelativeX, visibleSpan, fullSpan, hitTest, onHoverSegment]
  );

  const handleMouseUp = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      if (brushMode && isDragging) {
        if (hasDragged && brushDragStartPx !== null && brushDragCurrentPx !== null) {
          const startNS = pxToNS(brushDragStartPx);
          const endNS = pxToNS(brushDragCurrentPx);
          if (Math.abs(endNS - startNS) > 0) {
            onBrushChange?.([Math.min(startNS, endNS), Math.max(startNS, endNS)]);
          }
        }
        setBrushDragStartPx(null);
        setBrushDragCurrentPx(null);
        setIsDragging(false);
        setHasDragged(false);
        return;
      }
      if (isDragging && !hasDragged) {
        const seg = hitTest(e.clientX, e.clientY);
        if (seg) {
          onSelectGoroutine(seg.goroutine_id, seg);
        } else {
          const canvas = rowsCanvasRef.current;
          const rect = canvas?.getBoundingClientRect();
          if (rect) {
            const y = e.clientY - rect.top;
            const rowIndex = Math.floor((y + rowScrollTop) / METRICS.rowHeight);
            if (rowIndex >= 0 && rowIndex < goroutines.length) {
              onSelectGoroutine(goroutines[rowIndex].goroutine_id, undefined);
            }
          }
        }
      }
      setIsDragging(false);
      setHasDragged(false);
    },
    [
      brushMode, isDragging, hasDragged,
      brushDragStartPx, brushDragCurrentPx,
      pxToNS, onBrushChange,
      hitTest, goroutines, onSelectGoroutine, rowScrollTop,
    ]
  );

  const handleMouseLeave = useCallback(() => {
    setHoveredSegment(null);
    onHoverSegment?.(null);
    if (isDragging) {
      if (brushMode) {
        setBrushDragStartPx(null);
        setBrushDragCurrentPx(null);
      }
      setIsDragging(false);
    }
  }, [onHoverSegment, isDragging, brushMode]);

  // ── Axis scrubber handlers ────────────────────────────────────────────────

  /** Convert axis canvas clientX → absolute NS timestamp. */
  const axisXToNS = useCallback(
    (clientX: number): number => {
      const canvas = canvasRef.current;
      const container = containerRef.current;
      if (!canvas || !container) return visibleStart;
      const rect = canvas.getBoundingClientRect();
      const x = clientX - rect.left;
      const innerWidth = getInnerWidth(container.clientWidth);
      const clamped = Math.max(0, Math.min(innerWidth, x - plotLeft));
      return visibleStart + (clamped / innerWidth) * visibleSpan;
    },
    [visibleStart, visibleSpan]
  );

  const handleAxisMouseMove = useCallback(
    (e: React.MouseEvent) => {
      const canvas = canvasRef.current;
      const container = containerRef.current;
      if (!canvas || !container) { setGhostTimeNS(null); return; }
      const rect = canvas.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const innerWidth = getInnerWidth(container.clientWidth);
      if (x >= plotLeft && x <= plotLeft + innerWidth) {
        setGhostTimeNS(visibleStart + ((x - plotLeft) / innerWidth) * visibleSpan);
      } else {
        setGhostTimeNS(null);
      }
    },
    [visibleStart, visibleSpan]
  );

  const handleAxisMouseLeave = useCallback(() => {
    setGhostTimeNS(null);
  }, []);

  const handleAxisClick = useCallback(
    (e: React.MouseEvent) => {
      onScrubChange?.(axisXToNS(e.clientX));
    },
    [axisXToNS, onScrubChange]
  );

  const handleAxisDoubleClick = useCallback(() => {
    onScrubChange?.(null);
  }, [onScrubChange]);

  return (
    <div ref={containerRef} className="timeline-canvas-container">
      <canvas
        ref={canvasRef}
        className="timeline-canvas timeline-canvas-axis"
        onWheel={handleWheel}
        onMouseMove={handleAxisMouseMove}
        onMouseLeave={handleAxisMouseLeave}
        onClick={handleAxisClick}
        onDoubleClick={handleAxisDoubleClick}
        style={{ cursor: "crosshair" }}
      />
      <div
        ref={scrollContainerRef}
        className="timeline-canvas-rows-scroll"
        style={{
          height: rowAreaHeight,
          overflowY: totalRowsHeight > rowAreaHeight ? "auto" : "hidden",
        }}
      >
        <div style={{ height: totalRowsHeight }}>
          <canvas
            ref={rowsCanvasRef}
            className="timeline-canvas timeline-canvas-rows"
            style={{
              position: "sticky",
              top: 0,
              cursor: brushMode
                ? (isDragging ? "col-resize" : "crosshair")
                : zoomLevel > 1
                  ? (isDragging ? "grabbing" : "grab")
                  : "pointer",
            }}
            onMouseDown={handleMouseDown}
            onMouseMove={handleMouseMove}
            onMouseUp={handleMouseUp}
            onMouseLeave={handleMouseLeave}
          />
        </div>
      </div>
      {hoveredSegment && (
        <div
          className="timeline-tooltip"
          style={{
            position: "fixed",
            left: tooltipPos.x,
            top: tooltipPos.y - 28,
            transform: "translate(-50%, 0)",
            pointerEvents: "none",
          }}
        >
          {hoveredSegment.state} {formatDuration(hoveredSegment.end_ns - hoveredSegment.start_ns)}
          {hoveredSegment.reason && ` · ${hoveredSegment.reason}`}
        </div>
      )}
    </div>
  );
}
