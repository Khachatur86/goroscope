import { useEffect, useRef, useState, useCallback } from "react";
import type { Goroutine, TimelineSegment, ProcessorSegment } from "../api/client";
import { STATE_COLORS as COLORS, COLOR_UNKNOWN, COLOR_SELECTED, BG_SECONDARY } from "../theme/tokens";

function formatDuration(ns: number): string {
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(2)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(2)}µs`;
  return `${ns}ns`;
}

const METRICS = {
  axisHeight: 38,
  pRowH: 18,
  pLabelH: 14,
  pGap: 2,
  gRowH: 14,
  labelW: 58,
  rightPad: 18,
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
  processorSegments: ProcessorSegment[];
  selectedId: number | null;
  onSelectGoroutine: (id: number, segment?: TimelineSegment) => void;
  zoomLevel: number;
  panOffsetNS: number;
  fullMinStart: number;
  fullSpan: number;
  onZoomPanChange?: (zoomLevel: number, panOffsetNS: number) => void;
};

export function TimelineHeatmapCanvas({
  goroutines,
  segments,
  processorSegments,
  selectedId,
  onSelectGoroutine,
  zoomLevel,
  panOffsetNS,
  fullMinStart,
  fullSpan,
  onZoomPanChange,
}: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rowsCanvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [rowScrollTop, setRowScrollTop] = useState(0);
  const dragStartX = useRef(0);
  const dragStartPan = useRef(0);

  const processorIds = [...new Set(processorSegments.map((s) => s.processor_id))].sort((a, b) => a - b);
  const numPs = processorIds.length;
  const gmpH =
    numPs > 0
      ? METRICS.pLabelH + numPs * METRICS.pRowH + METRICS.pGap + 8
      : 0;
  const gTop = METRICS.axisHeight + gmpH;

  const visibleSpan = fullSpan / zoomLevel;
  const visibleStart = fullMinStart + Math.max(0, Math.min(fullSpan - visibleSpan, panOffsetNS));
  const plotLeft = METRICS.labelW;
  const getInnerWidth = (w: number) => Math.max(1, w - plotLeft - METRICS.rightPad);

  const byGoroutine = new Map<number, TimelineSegment[]>();
  for (const seg of segments) {
    const list = byGoroutine.get(seg.goroutine_id) ?? [];
    list.push(seg);
    byGoroutine.set(seg.goroutine_id, list);
  }

  const totalRowsHeight = goroutines.length * METRICS.gRowH;
  const rowAreaHeight = Math.min(ROW_AREA_VIEWPORT_HEIGHT, totalRowsHeight + 16);
  const firstVisibleIndex = Math.max(
    0,
    Math.floor(rowScrollTop / METRICS.gRowH)
  );
  const lastVisibleIndex = Math.min(
    goroutines.length - 1,
    Math.floor((rowScrollTop + rowAreaHeight) / METRICS.gRowH)
  );

  useEffect(() => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const onScroll = () => setRowScrollTop(el.scrollTop);
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  const renderAxis = useCallback(() => {
    const canvas = canvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    const width = Math.max(320, container.clientWidth);
    const height = gTop;
    const innerWidth = getInnerWidth(width);
    const dpr = window.devicePixelRatio || 1;

    if (canvas.width !== Math.floor(width * dpr)) canvas.width = Math.floor(width * dpr);
    if (canvas.height !== Math.floor(height * dpr)) canvas.height = Math.floor(height * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = BG_SECONDARY;
    ctx.fillRect(0, 0, width, height);

    ctx.strokeStyle = "rgba(219,228,238,0.14)";
    ctx.beginPath();
    ctx.moveTo(plotLeft, METRICS.axisHeight - 10);
    ctx.lineTo(plotLeft + innerWidth, METRICS.axisHeight - 10);
    ctx.stroke();

    ctx.fillStyle = "rgba(219,228,238,0.55)";
    ctx.font = '10px "IBM Plex Mono", monospace';
    for (let i = 0; i < 5; i++) {
      const ratio = i / 4;
      const ns = visibleStart + ratio * visibleSpan;
      const x = plotLeft + ratio * innerWidth;
      const label = formatDuration(ns - fullMinStart);
      ctx.fillText(
        label,
        x - (i === 4 ? ctx.measureText(label).width : 0),
        METRICS.axisHeight - 14
      );
      ctx.strokeStyle = "rgba(219,228,238,0.08)";
      ctx.beginPath();
      ctx.moveTo(x, METRICS.axisHeight - 10);
      ctx.lineTo(x, height);
      ctx.stroke();
    }

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

    ctx.fillStyle = "rgba(219,228,238,0.38)";
    ctx.font = '10px "IBM Plex Mono", monospace';
    ctx.fillText("Goroutines", 2, gTop - 4);

    ctx.fillStyle = "rgba(2,6,23,0.45)";
    ctx.fillRect(0, METRICS.axisHeight, plotLeft - 2, height - METRICS.axisHeight);
    ctx.strokeStyle = "rgba(219,228,238,0.10)";
    ctx.beginPath();
    ctx.moveTo(plotLeft - 0.5, METRICS.axisHeight - 18);
    ctx.lineTo(plotLeft - 0.5, height);
    ctx.stroke();
  }, [
    visibleStart,
    visibleSpan,
    fullMinStart,
    processorSegments,
    processorIds,
    numPs,
    gTop,
  ]);

  const renderRows = useCallback(() => {
    const canvas = rowsCanvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    const width = Math.max(320, container.clientWidth);
    const height = rowAreaHeight;
    const innerWidth = getInnerWidth(width);
    const dpr = window.devicePixelRatio || 1;

    if (canvas.width !== Math.floor(width * dpr)) canvas.width = Math.floor(width * dpr);
    if (canvas.height !== Math.floor(height * dpr)) canvas.height = Math.floor(height * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = BG_SECONDARY;
    ctx.fillRect(0, 0, width, height);

    ctx.fillStyle = "rgba(2,6,23,0.45)";
    ctx.fillRect(0, 0, plotLeft - 2, height);
    ctx.strokeStyle = "rgba(219,228,238,0.10)";
    ctx.beginPath();
    ctx.moveTo(plotLeft - 0.5, 0);
    ctx.lineTo(plotLeft - 0.5, height);
    ctx.stroke();

    for (let idx = firstVisibleIndex; idx <= lastVisibleIndex; idx++) {
      const g = goroutines[idx];
      const drawY = idx * METRICS.gRowH - rowScrollTop;
      const segs = byGoroutine.get(g.goroutine_id) ?? [];
      const isSelected = g.goroutine_id === selectedId;

      if (idx % 2 === 0) {
        ctx.fillStyle = "rgba(255,255,255,0.022)";
        ctx.fillRect(0, drawY, width, METRICS.gRowH);
      }
      ctx.fillStyle = isSelected ? COLOR_SELECTED : "rgba(219,228,238,0.60)";
      ctx.font = '10px "IBM Plex Mono", monospace';
      ctx.fillText(`G${g.goroutine_id}`, 4, drawY + 10);
      if (isSelected) {
        ctx.fillStyle = "rgba(96,165,250,0.12)";
        ctx.fillRect(0, drawY, width, METRICS.gRowH);
        ctx.fillStyle = "rgba(125,211,252,0.95)";
        ctx.fillRect(0, drawY, 3, METRICS.gRowH);
      }

      segs.forEach((seg) => {
        const rawX = plotLeft + ((seg.start_ns - visibleStart) / visibleSpan) * innerWidth;
        const rawX2 = plotLeft + ((seg.end_ns - visibleStart) / visibleSpan) * innerWidth;
        const cx = Math.max(plotLeft, Math.min(rawX, plotLeft + innerWidth));
        const cx2 = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
        const cw = Math.max(cx2 - cx, rawX2 > rawX ? 1 : 0);
        if (cw === 0) return;
        ctx.fillStyle = COLORS[seg.state] ?? COLOR_UNKNOWN;
        ctx.fillRect(cx, drawY + 1, cw, METRICS.gRowH - 2);
      });

      ctx.strokeStyle = "rgba(219,228,238,0.07)";
      ctx.beginPath();
      ctx.moveTo(0, drawY + METRICS.gRowH - 0.5);
      ctx.lineTo(width, drawY + METRICS.gRowH - 0.5);
      ctx.stroke();
    }
  }, [
    goroutines,
    segments,
    selectedId,
    visibleStart,
    visibleSpan,
    firstVisibleIndex,
    lastVisibleIndex,
    rowScrollTop,
    rowAreaHeight,
    byGoroutine,
  ]);

  useEffect(() => {
    renderAxis();
  }, [renderAxis]);

  useEffect(() => {
    renderRows();
  }, [renderRows]);

  useEffect(() => {
    const ro = new ResizeObserver(() => {
      renderAxis();
      renderRows();
    });
    const el = containerRef.current;
    if (el) ro.observe(el);
    return () => ro.disconnect();
  }, [renderAxis, renderRows]);

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      if (isDragging) return;
      const canvas = rowsCanvasRef.current;
      if (!canvas) return;
      const rect = canvas.getBoundingClientRect();
      const y = e.clientY - rect.top;
      const rowIndex = Math.floor((y + rowScrollTop) / METRICS.gRowH);
      if (rowIndex >= 0 && rowIndex < goroutines.length) {
        onSelectGoroutine(goroutines[rowIndex].goroutine_id, undefined);
      }
    },
    [goroutines, onSelectGoroutine, isDragging, rowScrollTop]
  );

  const handleWheel = useCallback(
    (e: React.WheelEvent) => {
      e.preventDefault();
      if (!onZoomPanChange || fullSpan <= 1) return;
      const rect = canvasRef.current?.getBoundingClientRect();
      if (!rect || !containerRef.current) return;
      const innerWidth = getInnerWidth(containerRef.current.clientWidth);
      const canvasX = e.clientX - rect.left;
      const fx = Math.max(0, Math.min(1, (canvasX - plotLeft) / innerWidth));
      const cursorNS = panOffsetNS + fx * visibleSpan;
      const zoomFactor = e.deltaY < 0 ? 1.3 : 1 / 1.3;
      const newZoom = Math.max(1, Math.min(500, zoomLevel * zoomFactor));
      const newVisibleSpan = fullSpan / newZoom;
      let newPan = cursorNS - fx * newVisibleSpan;
      newPan = Math.max(0, Math.min(fullSpan - newVisibleSpan, newPan));
      onZoomPanChange(newZoom, newPan);
    },
    [fullSpan, zoomLevel, panOffsetNS, visibleSpan, onZoomPanChange]
  );

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      dragStartX.current = e.clientX;
      dragStartPan.current = panOffsetNS;

      const onMove = (ev: MouseEvent) => {
        if (!onZoomPanChange || !containerRef.current) return;
        const dx = ev.clientX - dragStartX.current;
        const innerWidth = getInnerWidth(containerRef.current.clientWidth);
        const nsPerPx = visibleSpan / innerWidth;
        const newPan = dragStartPan.current - dx * nsPerPx;
        const maxPan = Math.max(0, fullSpan - visibleSpan);
        onZoomPanChange(zoomLevel, Math.max(0, Math.min(maxPan, newPan)));
      };
      const onUp = () => {
        setIsDragging(false);
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
      };
      setIsDragging(true);
      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
    },
    [panOffsetNS, visibleSpan, fullSpan, zoomLevel, onZoomPanChange]
  );

  const handleMouseLeave = useCallback(() => {
    setIsDragging(false);
  }, []);

  return (
    <div ref={containerRef} className="timeline-canvas-container timeline-heatmap-canvas">
      <canvas
        ref={canvasRef}
        className="timeline-canvas timeline-canvas-axis"
        onWheel={handleWheel}
        style={{ cursor: zoomLevel > 1 ? (isDragging ? "grabbing" : "grab") : "default" }}
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
              cursor: zoomLevel > 1 ? (isDragging ? "grabbing" : "grab") : "pointer",
            }}
            onClick={handleClick}
            onMouseDown={handleMouseDown}
            onMouseLeave={handleMouseLeave}
          />
        </div>
      </div>
    </div>
  );
}
