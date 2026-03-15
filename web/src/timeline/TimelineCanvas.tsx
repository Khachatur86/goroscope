import { useEffect, useRef, useState, useCallback } from "react";
import type { Goroutine, TimelineSegment } from "../api/client";

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

const METRICS = {
  axisHeight: 38,
  rowHeight: 28,
  labelGutterWidth: 182,
  leftPadding: 14,
  rightPadding: 18,
};

type Props = {
  goroutines: Goroutine[];
  segments: TimelineSegment[];
  selectedId: number | null;
  onSelectGoroutine: (id: number) => void;
  zoomToSelected: boolean;
  onHoverSegment?: (seg: TimelineSegment | null) => void;
};

export function TimelineCanvas({
  goroutines,
  segments,
  selectedId,
  onSelectGoroutine,
  zoomToSelected,
  onHoverSegment,
}: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [zoomLevel, setZoomLevel] = useState(1);
  const [panOffsetNS, setPanOffsetNS] = useState(0);
  const [isDragging, setIsDragging] = useState(false);
  const [hasDragged, setHasDragged] = useState(false);
  const dragStartX = useRef(0);
  const dragStartPanNS = useRef(0);
  const [hoveredSegment, setHoveredSegment] = useState<TimelineSegment | null>(null);
  const [tooltipPos, setTooltipPos] = useState({ x: 0, y: 0 });

  const fullMinStart = Math.min(...segments.map((s) => s.start_ns));
  const fullMaxEnd = Math.max(...segments.map((s) => s.end_ns));
  const fullSpan = Math.max(fullMaxEnd - fullMinStart, 1);

  // Sync zoom/pan when zoomToSelected changes
  useEffect(() => {
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
  }, [zoomToSelected, selectedId, fullMinStart, fullSpan, segments]);

  const visibleSpan = fullSpan / zoomLevel;
  const visibleStart = fullMinStart + Math.max(0, Math.min(fullSpan - visibleSpan, panOffsetNS));

  const plotLeft = METRICS.labelGutterWidth + METRICS.leftPadding;
  const getInnerWidth = (width: number) =>
    Math.max(1, width - METRICS.labelGutterWidth - METRICS.leftPadding - METRICS.rightPadding);

  const render = useCallback(() => {
    const canvas = canvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    const width = Math.max(320, container.clientWidth);
    const height = Math.max(220, METRICS.axisHeight + goroutines.length * METRICS.rowHeight + 16);
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

    // Axis
    ctx.strokeStyle = "rgba(219, 228, 238, 0.14)";
    ctx.beginPath();
    ctx.moveTo(plotLeft, METRICS.axisHeight - 10);
    ctx.lineTo(width - METRICS.rightPadding, METRICS.axisHeight - 10);
    ctx.stroke();

    for (let i = 0; i < 5; i++) {
      const ratio = i / 4;
      const x = plotLeft + ratio * innerWidth;
      const value = visibleStart + ratio * visibleSpan;
      ctx.strokeStyle = "rgba(219, 228, 238, 0.12)";
      ctx.beginPath();
      ctx.moveTo(x, METRICS.axisHeight - 18);
      ctx.lineTo(x, height - 16);
      ctx.stroke();
      ctx.fillStyle = "#dbe4ee";
      ctx.font = '11px "IBM Plex Mono", monospace';
      ctx.fillText(formatDuration(value - fullMinStart), x + 6, 20);
    }

    // Gutter
    ctx.fillStyle = "rgba(2, 6, 23, 0.48)";
    ctx.fillRect(0, METRICS.axisHeight, plotLeft - 8, height - METRICS.axisHeight);
    ctx.strokeStyle = "rgba(219, 228, 238, 0.10)";
    ctx.beginPath();
    ctx.moveTo(plotLeft - 0.5, METRICS.axisHeight - 18);
    ctx.lineTo(plotLeft - 0.5, height - 16);
    ctx.stroke();

    // Rows and segments
    goroutines.forEach((g, index) => {
      const y = METRICS.axisHeight + index * METRICS.rowHeight;
      const isSelected = g.goroutine_id === selectedId;

      if (index % 2 === 0) {
        ctx.fillStyle = "rgba(255, 255, 255, 0.028)";
        ctx.fillRect(0, y, width, METRICS.rowHeight);
      }
      if (isSelected) {
        ctx.fillStyle = "rgba(96, 165, 250, 0.10)";
        ctx.fillRect(0, y, width, METRICS.rowHeight);
        ctx.fillStyle = "rgba(125, 211, 252, 0.95)";
        ctx.fillRect(0, y + 2, 4, METRICS.rowHeight - 4);
      }
      ctx.strokeStyle = "rgba(219, 228, 238, 0.13)";
      ctx.beginPath();
      ctx.moveTo(0, y + METRICS.rowHeight - 0.5);
      ctx.lineTo(width - METRICS.rightPadding, y + METRICS.rowHeight - 0.5);
      ctx.stroke();

      ctx.fillStyle = isSelected ? "#f8fafc" : "#9fb3c8";
      ctx.font = '12px "IBM Plex Mono", monospace';
      ctx.fillText(`G${g.goroutine_id}`, 14, y + 12);
      ctx.fillStyle = isSelected ? "rgba(219, 228, 238, 0.74)" : "rgba(159, 179, 200, 0.46)";
      ctx.font = '11px "IBM Plex Mono", monospace';
      const fn = (g.labels?.function || "unknown").slice(0, 20);
      ctx.fillText(fn, 14, y + 23);

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
          const barY = y + 4;
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
    });
  }, [
    goroutines,
    segments,
    selectedId,
    visibleStart,
    visibleSpan,
    fullMinStart,
    hoveredSegment,
  ]);

  useEffect(() => {
    render();
  }, [render]);

  useEffect(() => {
    const resizeObserver = new ResizeObserver(() => render());
    const el = containerRef.current;
    if (el) resizeObserver.observe(el);
    return () => resizeObserver.disconnect();
  }, [render]);

  const hitTest = useCallback(
    (clientX: number, clientY: number): TimelineSegment | null => {
      const canvas = canvasRef.current;
      const container = containerRef.current;
      if (!canvas || !container) return null;

      const rect = canvas.getBoundingClientRect();
      const x = clientX - rect.left;
      const y = clientY - rect.top;
      const width = container.clientWidth;
      const innerWidth = getInnerWidth(width);

      const rowIndex = Math.floor((y - METRICS.axisHeight) / METRICS.rowHeight);
      if (rowIndex < 0 || rowIndex >= goroutines.length) return null;
      const g = goroutines[rowIndex];

      const segs = segments.filter((s) => s.goroutine_id === g.goroutine_id);
      for (const seg of segs) {
        const rawX = plotLeft + ((seg.start_ns - visibleStart) / visibleSpan) * innerWidth;
        const rawX2 = plotLeft + ((seg.end_ns - visibleStart) / visibleSpan) * innerWidth;
        const cx = Math.max(plotLeft, Math.min(rawX, plotLeft + innerWidth));
        const cx2 = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
        const barY = METRICS.axisHeight + rowIndex * METRICS.rowHeight + 4;
        if (x >= cx && x <= cx2 && y >= barY && y <= barY + 20) return seg;
      }
      return null;
    },
    [goroutines, segments, visibleStart, visibleSpan]
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

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      setHoveredSegment(null);
      onHoverSegment?.(null);
      setIsDragging(true);
      setHasDragged(false);
      dragStartX.current = e.clientX;
      dragStartPanNS.current = panOffsetNS;
    },
    [panOffsetNS, onHoverSegment]
  );

  const handleMouseMove = useCallback(
    (e: React.MouseEvent) => {
      if (isDragging) {
        setHasDragged(true);
        const dx = e.clientX - dragStartX.current;
        const container = containerRef.current;
        if (!container) return;
        const innerWidth = getInnerWidth(container.clientWidth);
        const nsPerPx = visibleSpan / innerWidth;
        const newPan = dragStartPanNS.current - dx * nsPerPx;
        setPanOffsetNS(Math.max(0, Math.min(fullSpan - visibleSpan, newPan)));
      } else {
        const seg = hitTest(e.clientX, e.clientY);
        setHoveredSegment(seg);
        setTooltipPos({ x: e.clientX, y: e.clientY });
        onHoverSegment?.(seg ?? null);
      }
    },
    [isDragging, visibleSpan, fullSpan, hitTest, onHoverSegment]
  );

  const handleMouseUp = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      if (isDragging && !hasDragged) {
        const seg = hitTest(e.clientX, e.clientY);
        if (seg) {
          onSelectGoroutine(seg.goroutine_id);
        } else {
          const rowIndex = Math.floor(
            (e.clientY - (canvasRef.current?.getBoundingClientRect().top ?? 0) - METRICS.axisHeight) /
              METRICS.rowHeight
          );
          if (rowIndex >= 0 && rowIndex < goroutines.length) {
            onSelectGoroutine(goroutines[rowIndex].goroutine_id);
          }
        }
      }
      setIsDragging(false);
      setHasDragged(false);
    },
    [isDragging, hasDragged, hitTest, goroutines, onSelectGoroutine]
  );

  const handleMouseLeave = useCallback(() => {
    setHoveredSegment(null);
    onHoverSegment?.(null);
    if (isDragging) setIsDragging(false);
  }, [onHoverSegment, isDragging]);

  return (
    <div ref={containerRef} className="timeline-canvas-container">
      <canvas
        ref={canvasRef}
        className="timeline-canvas"
        onWheel={handleWheel}
        onMouseDown={handleMouseDown}
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseLeave}
        style={{ cursor: zoomLevel > 1 ? (isDragging ? "grabbing" : "grab") : "pointer" }}
      />
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
