import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Goroutine, TimelineSegment, ProcessorSegment } from "../api/client";
import { fetchTimeline, fetchProcessorTimeline } from "../api/client";
import { TimelineCanvas } from "./TimelineCanvas";
import { TimelineHeatmapCanvas } from "./TimelineHeatmapCanvas";
import { MinimapCanvas } from "./MinimapCanvas";
import { MetricsChart } from "./MetricsChart";

type FiltersState = {
  state: string;
  reason: string;
  resource: string;
  search: string;
  labelFilter?: string;
};

type Props = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelectGoroutine: (id: number, segment?: TimelineSegment) => void;
  filters: FiltersState;
  zoomToSelected?: boolean;
  viewMode?: "lanes" | "heatmap";
  /** When provided, use these segments instead of fetching from API (e.g. for Compare mode). */
  segmentsOverride?: TimelineSegment[] | null;
  /** When set, goroutines NOT in this set are dimmed in the timeline. */
  highlightedIds?: Set<number> | null;
  /** Fired when brush selection changes — null means cleared. */
  onBrushFilterChange?: (ids: Set<number> | null) => void;
};

const COLORS: Record<string, string> = {
  RUNNING: "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING: "#f59e0b",
  BLOCKED: "#f43f5e",
  SYSCALL: "#4da6ff",
  DONE: "#4b5563",
};

export function Timeline({
  goroutines,
  selectedId,
  onSelectGoroutine,
  filters,
  zoomToSelected = false,
  viewMode = "lanes",
  segmentsOverride,
  highlightedIds,
  onBrushFilterChange,
}: Props) {
  const [segments, setSegments] = useState<TimelineSegment[]>([]);
  const [processorSegments, setProcessorSegments] = useState<ProcessorSegment[]>([]);
  const [canvasZoomLevel, setCanvasZoomLevel] = useState(1);
  const [canvasPanOffsetNS, setCanvasPanOffsetNS] = useState(0);
  const [brushMode, setBrushMode] = useState(false);
  const [brushRange, setBrushRange] = useState<[number, number] | null>(null);

  useEffect(() => {
    if (segmentsOverride !== undefined) {
      setSegments(segmentsOverride ?? []);
      return;
    }
    fetchTimeline({
      state: filters.state !== "ALL" ? filters.state : undefined,
      reason: filters.reason || undefined,
      search: filters.search || undefined,
      label: filters.labelFilter || undefined,
    })
      .then((data) => setSegments(Array.isArray(data) ? data : []))
      .catch(() => setSegments([]));
  }, [goroutines, filters.state, filters.reason, filters.search, segmentsOverride]);

  useEffect(() => {
    fetchProcessorTimeline()
      .then(setProcessorSegments)
      .catch(() => setProcessorSegments([]));
  }, []);

  const filteredSegments = useMemo(
    () =>
      (segments ?? []).filter((seg) =>
        (goroutines ?? []).some((g) => g.goroutine_id === seg.goroutine_id)
      ),
    [segments, goroutines]
  );

  const fullMinStart = Math.min(...filteredSegments.map((s) => s.start_ns));
  const fullMaxEnd = Math.max(...filteredSegments.map((s) => s.end_ns));
  const fullSpan = Math.max(fullMaxEnd - fullMinStart, 1);
  const isHeatmap = viewMode === "heatmap";

  // Keep refs to latest computed values so the zoom effect can read them
  // without including them as deps (which would re-fire on every poll cycle).
  const filteredSegmentsRef = useRef(filteredSegments);
  filteredSegmentsRef.current = filteredSegments;
  const fullMinStartRef = useRef(fullMinStart);
  fullMinStartRef.current = fullMinStart;
  const fullMaxEndRef = useRef(fullMaxEnd);
  fullMaxEndRef.current = fullMaxEnd;
  const fullSpanRef = useRef(fullSpan);
  fullSpanRef.current = fullSpan;

  // Sync canvas zoom/pan only when the user explicitly changes zoomToSelected
  // or selects a different goroutine — not on background poll updates.
  useEffect(() => {
    if (zoomToSelected && selectedId) {
      const segs = filteredSegmentsRef.current;
      const selectedSegs = segs.filter((s) => s.goroutine_id === selectedId);
      if (selectedSegs.length > 0) {
        const minStart = Math.min(...selectedSegs.map((s) => s.start_ns));
        const maxEnd = Math.max(...selectedSegs.map((s) => s.end_ns));
        const padding = Math.max((maxEnd - minStart) * 0.1, 1);
        const visibleStart = Math.max(fullMinStartRef.current, minStart - padding);
        const visibleEnd = Math.min(fullMaxEndRef.current, maxEnd + padding);
        const visibleSpan = visibleEnd - visibleStart;
        setCanvasPanOffsetNS(visibleStart - fullMinStartRef.current);
        setCanvasZoomLevel(fullSpanRef.current / visibleSpan);
        return;
      }
    }
    setCanvasZoomLevel(1);
    setCanvasPanOffsetNS(0);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [zoomToSelected, selectedId]);

  const handleBrushChange = useCallback(
    (range: [number, number] | null) => {
      setBrushRange(range);
      if (range === null) {
        onBrushFilterChange?.(null);
        return;
      }
      const [startNS, endNS] = range;
      // goroutines with at least one segment overlapping [startNS, endNS]
      const activeIds = new Set<number>();
      for (const seg of filteredSegmentsRef.current) {
        if (seg.end_ns >= startNS && seg.start_ns <= endNS) {
          activeIds.add(seg.goroutine_id);
        }
      }
      onBrushFilterChange?.(activeIds);
    },
    [onBrushFilterChange]
  );

  const clearBrush = () => {
    setBrushRange(null);
    setBrushMode(false);
    onBrushFilterChange?.(null);
  };

  if (filteredSegments.length === 0) {
    return (
      <div className="timeline-placeholder">
        Select a goroutine to see timeline segments.
      </div>
    );
  }

  const canvasVisibleSpan = fullSpan / canvasZoomLevel;
  const showMinimapCanvas = true;

  return (
    <div className={`timeline-simple ${isHeatmap ? "timeline-heatmap" : ""}`}>
      <div className="timeline-legend">
        {Object.entries(COLORS).map(([state, color]) => (
          <span key={state} className="legend-chip" style={{ background: color }}>
            {state}
          </span>
        ))}
        <div className="timeline-brush-controls">
          <button
            type="button"
            className={`timeline-control-button timeline-brush-toggle ${brushMode ? "active" : ""}`}
            onClick={() => {
              const next = !brushMode;
              setBrushMode(next);
              if (!next) clearBrush();
            }}
            title="Select a time range to filter goroutines (drag on timeline)"
            aria-pressed={brushMode}
          >
            ⌖ Select range
          </button>
          {brushRange && (
            <button
              type="button"
              className="timeline-control-button timeline-brush-clear"
              onClick={clearBrush}
              title="Clear time range selection"
            >
              ✕ Clear range
            </button>
          )}
        </div>
      </div>
      <MetricsChart segments={filteredSegments} highlightRange={brushRange} />
      {isHeatmap ? (
        <div className="timeline-canvas-wrapper">
          <TimelineHeatmapCanvas
            goroutines={goroutines}
            segments={filteredSegments}
            processorSegments={processorSegments}
            selectedId={selectedId}
            onSelectGoroutine={onSelectGoroutine}
            zoomLevel={canvasZoomLevel}
            panOffsetNS={canvasPanOffsetNS}
            fullMinStart={fullMinStart}
            fullSpan={fullSpan}
            onZoomPanChange={(zl, pan) => {
              setCanvasZoomLevel(zl);
              setCanvasPanOffsetNS(pan);
            }}
          />
        </div>
      ) : (
        <div className="timeline-canvas-wrapper">
          <TimelineCanvas
            goroutines={goroutines}
            segments={filteredSegments}
            processorSegments={processorSegments}
            selectedId={selectedId}
            onSelectGoroutine={onSelectGoroutine}
            zoomToSelected={zoomToSelected ?? false}
            zoomLevel={canvasZoomLevel}
            panOffsetNS={canvasPanOffsetNS}
            onZoomPanChange={(zl, pan) => {
              setCanvasZoomLevel(zl);
              setCanvasPanOffsetNS(pan);
            }}
            highlightedIds={highlightedIds}
            brushMode={brushMode}
            brushRange={brushRange}
            onBrushChange={handleBrushChange}
          />
        </div>
      )}
      {showMinimapCanvas && (
        <MinimapCanvas
          fullSpan={fullSpan}
          visibleSpan={canvasVisibleSpan}
          panOffsetNS={canvasPanOffsetNS}
          onPanChange={setCanvasPanOffsetNS}
        />
      )}
    </div>
  );
}
