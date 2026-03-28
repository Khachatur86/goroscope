import { useEffect, useRef, useState, useCallback, useMemo, forwardRef, useImperativeHandle } from "react";
import type { Goroutine, TimelineSegment, ProcessorSegment } from "../api/client";
import { encodeAnimatedGIF, type GifFrame } from "../gif/encoder";
import type { Bookmark } from "./bookmarks";
import { STATE_COLORS as COLORS, COLOR_UNKNOWN, COLOR_SELECTED, COLOR_SCRUBBER,
  BG_BASE, BG_SECONDARY, COLOR_AXIS_TEXT, TEXT_PRIMARY, TEXT_SECONDARY } from "../theme/tokens";

// ── Annotation storage ────────────────────────────────────────────────────────

/** A user note anchored to the start of a specific timeline segment. */
type Annotation = {
  goroutineId: number;
  startNS: number;
  text: string;
};

/** Popover state while editing or creating an annotation. */
type PopoverState = {
  /** Viewport coordinates for placement. */
  clientX: number;
  clientY: number;
  goroutineId: number;
  startNS: number;
};

const ANNOTATION_KEY = "goroscope:annotations";

function loadAnnotations(): Annotation[] {
  try {
    return JSON.parse(localStorage.getItem(ANNOTATION_KEY) ?? "[]") as Annotation[];
  } catch {
    return [];
  }
}

function persistAnnotations(anns: Annotation[]): void {
  localStorage.setItem(ANNOTATION_KEY, JSON.stringify(anns));
}

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

/** Imperative handle exposed by TimelineCanvas via ref. */
export type TimelineCanvasHandle = {
  /** Composites axis + rows canvases into a PNG and triggers a download. */
  exportPng: () => void;
  /**
   * Renders the current timeline as an animated GIF with a sweeping cursor.
   * @param nFrames  Number of animation frames (default 24).
   * @param fpsHint  Target playback speed (default 12 fps → 8 cs delay).
   * @param onDone   Called when encoding is complete (success or error).
   */
  exportGif: (nFrames?: number, fpsHint?: number, onDone?: () => void) => void;
};

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
  /**
   * Fired (debounced ~100 ms) when the visible goroutine row range changes due to scrolling.
   * Used by the parent to lazy-load segments for the visible slice.
   */
  onVisibleRangeChange?: (firstIndex: number, lastIndex: number) => void;
  /** When true, draw ▲ (born) and ▼ (died) lifecycle markers on goroutine rows (G-3). */
  showLifecycleMarkers?: boolean;
  /** Named time bookmarks drawn as violet dashed lines (U-4). */
  bookmarks?: Bookmark[];
  /** Fired when the user double-clicks the axis to add a bookmark. */
  onAddBookmarkRequest?: (timeNS: number) => void;
  /** Fired when the user clicks Delete on a hovered bookmark tooltip. */
  onDeleteBookmark?: (id: string) => void;
};

export const TimelineCanvas = forwardRef<TimelineCanvasHandle, Props>(function TimelineCanvas({
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
  onVisibleRangeChange,
  showLifecycleMarkers = false,
  bookmarks,
  onAddBookmarkRequest,
  onDeleteBookmark,
}: Props, ref) {
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

  // Pre-build segment lookup for O(1) per-row access instead of O(total_segments).
  // This is the critical optimisation for large traces (100k goroutines × N segments).
  const segmentsByGoroutine = useMemo(() => {
    const map = new Map<number, TimelineSegment[]>();
    for (const seg of segments) {
      const list = map.get(seg.goroutine_id);
      if (list) list.push(seg);
      else map.set(seg.goroutine_id, [seg]);
    }
    return map;
  }, [segments]);

  // ── Export ────────────────────────────────────────────────────────────────────
  // Composites axis canvas + rows canvas into a single PNG and downloads it.
  const exportPng = useCallback(() => {
    const axisCanvas = canvasRef.current;
    const rowsCanvas = rowsCanvasRef.current;
    if (!axisCanvas || !rowsCanvas) return;

    const dpr = window.devicePixelRatio || 1;
    const W = axisCanvas.clientWidth;
    const axisH = axisCanvas.clientHeight;
    const rowsH = rowsCanvas.clientHeight;
    const footerH = 22;
    const totalH = axisH + rowsH + footerH;

    const out = document.createElement("canvas");
    out.width = Math.round(W * dpr);
    out.height = Math.round(totalH * dpr);

    const ctx = out.getContext("2d");
    if (!ctx) return;
    ctx.scale(dpr, dpr);

    ctx.fillStyle = BG_BASE;
    ctx.fillRect(0, 0, W, totalH);

    ctx.drawImage(axisCanvas, 0, 0, W, axisH);
    ctx.drawImage(rowsCanvas, 0, axisH, W, rowsH);

    // Footer strip
    ctx.fillStyle = BG_BASE;
    ctx.fillRect(0, axisH + rowsH, W, footerH);
    ctx.fillStyle = COLOR_AXIS_TEXT;
    ctx.font = "10px monospace";
    ctx.textBaseline = "middle";
    ctx.fillText(
      `Goroscope  ·  ${new Date().toLocaleString()}  ·  ${goroutines.length} goroutines`,
      8,
      axisH + rowsH + footerH / 2
    );

    out.toBlob((blob) => {
      if (!blob) return;
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `goroscope-${Date.now()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    }, "image/png");
  }, [goroutines.length]);

  // ── GIF export ─────────────────────────────────────────────────────────────
  // Sweeps a cursor line across the composite image to produce an animation
  // that shows the goroutine timeline progressing through time.
  const exportGif = useCallback((
    nFrames = 24,
    fpsHint = 12,
    onDone?: () => void,
  ) => {
    const axisCanvas = canvasRef.current;
    const rowsCanvas = rowsCanvasRef.current;
    if (!axisCanvas || !rowsCanvas) { onDone?.(); return; }

    // Dimensions — use logical (1×) pixels so the GIF is a reasonable size.
    const W      = axisCanvas.clientWidth;
    const axisH  = axisCanvas.clientHeight;
    const rowsH  = rowsCanvas.clientHeight;
    const footerH = 18;
    const totalH = axisH + rowsH + footerH;

    if (W <= 0 || totalH <= 0) { onDone?.(); return; }

    // Composite static content (axis + rows + footer) onto an off-screen canvas.
    const base = document.createElement("canvas");
    base.width  = W;
    base.height = totalH;
    const bctx = base.getContext("2d");
    if (!bctx) { onDone?.(); return; }

    bctx.fillStyle = BG_BASE;
    bctx.fillRect(0, 0, W, totalH);
    bctx.drawImage(axisCanvas, 0, 0, W, axisH);
    bctx.drawImage(rowsCanvas, 0, axisH, W, rowsH);

    // Footer
    bctx.fillStyle = BG_BASE;
    bctx.fillRect(0, axisH + rowsH, W, footerH);
    bctx.fillStyle = COLOR_AXIS_TEXT;
    bctx.font = "9px monospace";
    bctx.textBaseline = "middle";
    bctx.fillText(
      `Goroscope · ${goroutines.length} goroutines`,
      8,
      axisH + rowsH + footerH / 2,
    );

    const baseData = bctx.getImageData(0, 0, W, totalH);
    const delayCs  = Math.max(1, Math.round(100 / fpsHint));

    // Build frames asynchronously via setTimeout to keep UI responsive.
    const frames: GifFrame[] = [];
    let fi = 0;

    const buildNextFrame = () => {
      if (fi >= nFrames) {
        // All frames collected — encode and download.
        try {
          const gif  = encodeAnimatedGIF(frames, W, totalH);
          // Copy into a plain Uint8Array to satisfy Blob's strict ArrayBuffer typing.
          const copy = new Uint8Array(gif.length);
          copy.set(gif);
          const blob = new Blob([copy], { type: "image/gif" });
          const url  = URL.createObjectURL(blob);
          const a    = document.createElement("a");
          a.href     = url;
          a.download = `goroscope-${Date.now()}.gif`;
          a.click();
          URL.revokeObjectURL(url);
        } catch (e) {
          console.error("GIF encoding failed:", e);
        }
        onDone?.();
        return;
      }

      // Copy base pixels.
      const data = new Uint8ClampedArray(baseData.data);

      // Draw cursor: 2px-wide bright cyan vertical line in the rows area.
      const xCursor = Math.round((fi / Math.max(nFrames - 1, 1)) * (W - 2));
      const CURSOR_R = 16, CURSOR_G = 207, CURSOR_B = 184; // STATE_COLORS.RUNNING

      for (let y = axisH; y < axisH + rowsH; y++) {
        for (let dx = 0; dx < 2; dx++) {
          const x   = Math.min(xCursor + dx, W - 1);
          const idx = (y * W + x) * 4;
          data[idx]     = CURSOR_R;
          data[idx + 1] = CURSOR_G;
          data[idx + 2] = CURSOR_B;
          data[idx + 3] = 255;
        }
      }

      frames.push({ pixels: data, delayCs });
      fi++;
      setTimeout(buildNextFrame, 0); // yield to UI thread between frames
    };

    buildNextFrame();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [goroutines.length]);

  useImperativeHandle(ref, () => ({ exportPng, exportGif }), [exportPng, exportGif]);

  // Brush drag state (pixels from canvas left edge → converted to NS on commit)
  const [brushDragStartPx, setBrushDragStartPx] = useState<number | null>(null);
  const [brushDragCurrentPx, setBrushDragCurrentPx] = useState<number | null>(null);

  // Ghost cursor: NS position of mouse hover over the axis (not committed).
  // Stored in state so renderAxis reacts to it; axis redraws are fast enough
  // for per-pixel mouse tracking.
  const [ghostTimeNS, setGhostTimeNS] = useState<number | null>(null);

  // Hovered bookmark for tooltip display.
  const [hoveredBookmark, setHoveredBookmark] = useState<{ bookmark: Bookmark; clientX: number; clientY: number } | null>(null);

  // Annotations: persisted to localStorage; displayed as amber pins on rows canvas.
  const [annotations, setAnnotations] = useState<Annotation[]>(loadAnnotations);
  const [popover, setPopover] = useState<PopoverState | null>(null);
  const [popoverDraft, setPopoverDraft] = useState("");

  // Fast lookup: "goroutineId:startNS" → annotation text.
  const annotationMap = useMemo(
    () => new Map(annotations.map((a) => [`${a.goroutineId}:${a.startNS}`, a.text])),
    [annotations]
  );

  // Avoid spread operators on large arrays — they cause stack overflows beyond ~125k elements.
  let fullMinStart = Infinity;
  let fullMaxEnd = -Infinity;
  for (const s of segments) {
    if (s.start_ns < fullMinStart) fullMinStart = s.start_ns;
    if (s.end_ns > fullMaxEnd) fullMaxEnd = s.end_ns;
  }
  if (fullMinStart === Infinity) fullMinStart = 0;
  if (fullMaxEnd === -Infinity) fullMaxEnd = 1;
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
    ctx.fillStyle = BG_SECONDARY;
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
      ctx.fillStyle = TEXT_PRIMARY;
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
    // Bookmark lines: violet dashed verticals with name labels.
    if (bookmarks && bookmarks.length > 0) {
      for (const bm of bookmarks) {
        const ratio = (bm.timeNS - visibleStart) / visibleSpan;
        const bx = plotLeft + ratio * innerWidth;
        if (bx < plotLeft || bx > width - METRICS.rightPadding) continue;
        ctx.save();
        ctx.strokeStyle = "rgba(167, 139, 250, 0.85)";
        ctx.lineWidth = 1.5;
        ctx.setLineDash([3, 3]);
        ctx.beginPath();
        ctx.moveTo(bx, 0);
        ctx.lineTo(bx, height);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.font = 'bold 9px "IBM Plex Mono", monospace';
        const lbl = bm.name;
        const lw = ctx.measureText(lbl).width + 8;
        const lx = Math.min(bx + 3, width - METRICS.rightPadding - lw - 2);
        ctx.fillStyle = "rgba(0,0,0,0.65)";
        ctx.fillRect(lx - 2, 2, lw, 14);
        ctx.fillStyle = "rgba(196, 181, 253, 0.95)";
        ctx.fillText(lbl, lx + 2, 12);
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
    bookmarks,
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
    ctx.fillStyle = BG_SECONDARY;
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
      ctx.fillStyle = isSelected ? COLOR_SELECTED : TEXT_SECONDARY;
      ctx.font = '12px "IBM Plex Mono", monospace';
      ctx.fillText(`G${g.goroutine_id}`, 14, drawY + 12);
      ctx.fillStyle = isSelected ? "rgba(219, 228, 238, 0.74)" : "rgba(159, 179, 200, 0.46)";
      ctx.font = '11px "IBM Plex Mono", monospace';
      const fn = (g.labels?.function || "unknown").slice(0, 20);
      ctx.fillText(fn, 14, drawY + 23);

      // OTel correlation badge: small cyan "OT" pill when otel.trace_id label is set.
      if (g.labels?.["otel.trace_id"]) {
        const badgeX = METRICS.labelGutterWidth - 28;
        const badgeY = drawY + 7;
        ctx.fillStyle = "rgba(20, 184, 166, 0.90)";
        ctx.beginPath();
        (ctx as CanvasRenderingContext2D & { roundRect: typeof ctx.roundRect }).roundRect(badgeX, badgeY, 22, 12, 3);
        ctx.fill();
        ctx.fillStyle = "#fff";
        ctx.font = 'bold 8px "IBM Plex Mono", monospace';
        ctx.fillText("OT", badgeX + 3, badgeY + 9);
      }

      (segmentsByGoroutine.get(g.goroutine_id) ?? []).forEach((seg) => {
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

          ctx.fillStyle = COLORS[seg.state] ?? COLOR_UNKNOWN;
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

    // ── Lifecycle markers (G-3) ───────────────────────────────────────────────
    if (showLifecycleMarkers) {
      for (let index = firstVisibleIndex; index <= lastVisibleIndex; index++) {
        const g = goroutines[index];
        const drawY = index * METRICS.rowHeight - rowScrollTop;
        const markerCy = drawY + METRICS.rowHeight / 2;
        const markerH = 8;

        const drawTriangle = (cx: number, up: boolean, color: string) => {
          if (cx < plotLeft || cx > plotLeft + innerWidth) return;
          ctx.fillStyle = color;
          ctx.beginPath();
          if (up) {
            ctx.moveTo(cx, markerCy - markerH);
            ctx.lineTo(cx + markerH * 0.6, markerCy);
            ctx.lineTo(cx - markerH * 0.6, markerCy);
          } else {
            ctx.moveTo(cx, markerCy + markerH);
            ctx.lineTo(cx + markerH * 0.6, markerCy);
            ctx.lineTo(cx - markerH * 0.6, markerCy);
          }
          ctx.closePath();
          ctx.fill();
        };

        if (g.born_ns && g.born_ns > 0) {
          const bx = plotLeft + ((g.born_ns - visibleStart) / visibleSpan) * innerWidth;
          drawTriangle(bx, true, "rgba(16, 207, 184, 0.9)");
        }
        if (g.died_ns && g.died_ns > 0) {
          const dx = plotLeft + ((g.died_ns - visibleStart) / visibleSpan) * innerWidth;
          drawTriangle(dx, false, "rgba(100, 116, 139, 0.9)");
        }
      }
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

    // Bookmark lines in rows: violet semi-transparent dashed verticals.
    if (bookmarks && bookmarks.length > 0) {
      for (const bm of bookmarks) {
        const ratio = (bm.timeNS - visibleStart) / visibleSpan;
        const bx = plotLeft + ratio * innerWidth;
        if (bx < plotLeft || bx > width - METRICS.rightPadding) continue;
        ctx.save();
        ctx.strokeStyle = "rgba(167, 139, 250, 0.22)";
        ctx.lineWidth = 1;
        ctx.setLineDash([3, 3]);
        ctx.beginPath();
        ctx.moveTo(bx, 0);
        ctx.lineTo(bx, height);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.restore();
      }
    }

    // Annotation pins: small downward-pointing amber triangle above each annotated segment.
    for (const ann of annotations) {
      const gIdx = goroutines.findIndex((g) => g.goroutine_id === ann.goroutineId);
      if (gIdx < firstVisibleIndex || gIdx > lastVisibleIndex) continue;
      const px = plotLeft + ((ann.startNS - visibleStart) / visibleSpan) * innerWidth;
      if (px < plotLeft || px > plotLeft + innerWidth) continue;
      const drawY = gIdx * METRICS.rowHeight - rowScrollTop;
      const cx2 = px + 4; // slight right offset so pin sits on bar start
      ctx.save();
      ctx.fillStyle = COLOR_SCRUBBER;
      ctx.strokeStyle = "rgba(0,0,0,0.35)";
      ctx.lineWidth = 0.5;
      ctx.beginPath();
      ctx.moveTo(cx2 - 5, drawY + 1);
      ctx.lineTo(cx2 + 5, drawY + 1);
      ctx.lineTo(cx2, drawY + 9);
      ctx.closePath();
      ctx.fill();
      ctx.stroke();
      ctx.restore();
    }
  }, [
    goroutines,
    segmentsByGoroutine,
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
    annotations,
    showLifecycleMarkers,
    bookmarks,
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

  // Notify parent when the visible goroutine index range changes (debounced 120 ms).
  // The parent uses this to lazy-load segments for visible goroutines only.
  useEffect(() => {
    if (!onVisibleRangeChange) return;
    const id = setTimeout(() => {
      onVisibleRangeChange(firstVisibleIndex, lastVisibleIndex);
    }, 120);
    return () => clearTimeout(id);
  }, [firstVisibleIndex, lastVisibleIndex, onVisibleRangeChange]);

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

      const segs = segmentsByGoroutine.get(g.goroutine_id) ?? [];
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
    [goroutines, segmentsByGoroutine, visibleStart, visibleSpan, rowScrollTop]
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

  // ── Annotation handlers ───────────────────────────────────────────────────

  const handleContextMenu = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const seg = hitTest(e.clientX, e.clientY);
      if (!seg) return;
      setPopoverDraft(annotationMap.get(`${seg.goroutine_id}:${seg.start_ns}`) ?? "");
      setPopover({ clientX: e.clientX, clientY: e.clientY, goroutineId: seg.goroutine_id, startNS: seg.start_ns });
    },
    [hitTest, annotationMap]
  );

  const commitAnnotation = useCallback(
    (goroutineId: number, startNS: number, text: string) => {
      setAnnotations((prev) => {
        const filtered = prev.filter((a) => !(a.goroutineId === goroutineId && a.startNS === startNS));
        const next = text.trim() ? [...filtered, { goroutineId, startNS, text: text.trim() }] : filtered;
        persistAnnotations(next);
        return next;
      });
      setPopover(null);
    },
    []
  );

  // Close popover when the user clicks outside it.
  useEffect(() => {
    if (!popover) return;
    const onMouseDown = (e: MouseEvent) => {
      if (!(e.target as Element).closest(".annotation-popover")) {
        setPopover(null);
      }
    };
    window.addEventListener("mousedown", onMouseDown);
    return () => window.removeEventListener("mousedown", onMouseDown);
  }, [popover]);

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
      // Bookmark hover: 6px hit zone around each bookmark line.
      if (bookmarks && bookmarks.length > 0) {
        const hit = bookmarks.find((bm) => {
          const ratio = (bm.timeNS - visibleStart) / visibleSpan;
          const bx = plotLeft + ratio * innerWidth;
          return Math.abs(x - bx) <= 6;
        });
        setHoveredBookmark(hit ? { bookmark: hit, clientX: e.clientX, clientY: e.clientY } : null);
      } else {
        setHoveredBookmark(null);
      }
    },
    [visibleStart, visibleSpan, bookmarks]
  );

  const handleAxisMouseLeave = useCallback(() => {
    setGhostTimeNS(null);
    setHoveredBookmark(null);
  }, []);

  const handleAxisClick = useCallback(
    (e: React.MouseEvent) => {
      onScrubChange?.(axisXToNS(e.clientX));
    },
    [axisXToNS, onScrubChange]
  );

  const handleAxisDoubleClick = useCallback(
    (e: React.MouseEvent) => {
      if (onAddBookmarkRequest) {
        onAddBookmarkRequest(axisXToNS(e.clientX));
      } else {
        onScrubChange?.(null);
      }
    },
    [axisXToNS, onAddBookmarkRequest, onScrubChange]
  );

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
            onContextMenu={handleContextMenu}
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
          {(() => {
            const ann = annotationMap.get(`${hoveredSegment.goroutine_id}:${hoveredSegment.start_ns}`);
            return ann
              ? <span className="timeline-tooltip-annotation"> 📝 {ann}</span>
              : <span className="timeline-tooltip-hint"> · right-click to annotate</span>;
          })()}
        </div>
      )}
      {popover && (
        <div
          className="annotation-popover"
          style={{
            position: "fixed",
            left: Math.min(popover.clientX, window.innerWidth - 260),
            top: Math.min(popover.clientY, window.innerHeight - 160),
          }}
        >
          <div className="annotation-popover-title">
            📝 G{popover.goroutineId} · note
          </div>
          <textarea
            className="annotation-popover-input"
            value={popoverDraft}
            onChange={(e) => setPopoverDraft(e.target.value)}
            placeholder="Add a note…"
            rows={3}
            autoFocus
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                commitAnnotation(popover.goroutineId, popover.startNS, popoverDraft);
              }
              if (e.key === "Escape") setPopover(null);
            }}
          />
          <div className="annotation-popover-actions">
            <button
              type="button"
              className="annotation-btn annotation-btn-save"
              onClick={() => commitAnnotation(popover.goroutineId, popover.startNS, popoverDraft)}
            >
              Save
            </button>
            {annotationMap.has(`${popover.goroutineId}:${popover.startNS}`) && (
              <button
                type="button"
                className="annotation-btn annotation-btn-delete"
                onClick={() => commitAnnotation(popover.goroutineId, popover.startNS, "")}
              >
                Delete
              </button>
            )}
            <button
              type="button"
              className="annotation-btn annotation-btn-cancel"
              onClick={() => setPopover(null)}
            >
              Cancel
            </button>
            <span className="annotation-popover-hint">⌘↵ save · ESC cancel</span>
          </div>
        </div>
      )}
      {hoveredBookmark && (
        <div
          className="bookmark-tooltip"
          style={{
            position: "fixed",
            left: Math.min(hoveredBookmark.clientX + 10, window.innerWidth - 220),
            top: hoveredBookmark.clientY - 8,
          }}
        >
          <div className="bookmark-tooltip-name">{hoveredBookmark.bookmark.name}</div>
          <div className="bookmark-tooltip-time">
            T+{formatAxisLabel(hoveredBookmark.bookmark.timeNS - fullMinStart)}
          </div>
          {onDeleteBookmark && (
            <button
              type="button"
              className="bookmark-tooltip-delete"
              onMouseDown={(e) => {
                e.stopPropagation();
                onDeleteBookmark(hoveredBookmark.bookmark.id);
                setHoveredBookmark(null);
              }}
            >
              Delete
            </button>
          )}
        </div>
      )}
    </div>
  );
});
