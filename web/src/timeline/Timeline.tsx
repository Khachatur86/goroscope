import { useCallback, useEffect, useMemo, useRef, useState, forwardRef, useImperativeHandle } from "react";
import type { Goroutine, TimelineSegment, ProcessorSegment } from "../api/client";
import { fetchTimeline, fetchProcessorTimeline } from "../api/client";
import { TimelineCanvas } from "./TimelineCanvas";
import type { TimelineCanvasHandle } from "./TimelineCanvas";
import { TimelineHeatmapCanvas } from "./TimelineHeatmapCanvas";
import { MinimapCanvas } from "./MinimapCanvas";
import { MetricsChart } from "./MetricsChart";

/** Lazy-load batch size: fetch this many goroutines' segments per request. */
const SEGMENT_BATCH_SIZE = 150;

/**
 * Historical state snapshot for one goroutine at the scrub time.
 * State and optional reason reflect the segment active at that moment.
 */
export type ScrubSnapshot = {
  goroutine_id: number;
  state: string;
  reason?: string;
};

type Props = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelectGoroutine: (id: number, segment?: TimelineSegment) => void;
  zoomToSelected?: boolean;
  viewMode?: "lanes" | "heatmap";
  /** When provided, use these segments instead of fetching from API (e.g. for Compare mode). */
  segmentsOverride?: TimelineSegment[] | null;
  /** When set, goroutines NOT in this set are dimmed in the timeline. */
  highlightedIds?: Set<number> | null;
  /** Fired when brush selection changes — null means cleared. */
  onBrushFilterChange?: (ids: Set<number> | null) => void;
  /**
   * Time-scrubber: absolute NS timestamp of the user-selected moment.
   * Draws an amber cursor on the timeline axis and goroutine rows.
   */
  scrubTimeNS?: number | null;
  /** Fired when the user clicks the axis (set) or double-clicks (clear). */
  onScrubChange?: (timeNS: number | null) => void;
  /**
   * Called whenever the scrub snapshot recomputes (scrubTimeNS or segments changed).
   * Passes one entry per goroutine with its historical state at scrubTimeNS.
   */
  onScrubSnapshot?: (snapshot: ScrubSnapshot[]) => void;
  /**
   * Called whenever the filtered segments array changes (new data, filter change, etc.).
   * Used by the goroutine list to render per-row lifetime bars without a separate fetch.
   */
  onSegmentsChange?: (segments: TimelineSegment[]) => void;
};

const COLORS: Record<string, string> = {
  RUNNING: "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING: "#f59e0b",
  BLOCKED: "#f43f5e",
  SYSCALL: "#4da6ff",
  DONE: "#4b5563",
};

/** Imperative handle exposed by Timeline via ref. */
export type TimelineHandle = {
  exportPng: () => void;
  exportGif: (nFrames?: number, fpsHint?: number, onDone?: () => void) => void;
};

export const Timeline = forwardRef<TimelineHandle, Props>(function Timeline({
  goroutines,
  selectedId,
  onSelectGoroutine,
  zoomToSelected = false,
  viewMode = "lanes",
  segmentsOverride,
  highlightedIds,
  onBrushFilterChange,
  scrubTimeNS,
  onScrubChange,
  onScrubSnapshot,
  onSegmentsChange,
}: Props, ref) {
  // segmentMap: goroutine_id → segments. Populated lazily as visible range changes.
  const [segmentMap, setSegmentMap] = useState<Map<number, TimelineSegment[]>>(new Map());
  // Track which goroutine IDs have already been fetched so we don't re-request them.
  const loadedGoroutineIds = useRef(new Set<number>());
  const [processorSegments, setProcessorSegments] = useState<ProcessorSegment[]>([]);
  const [canvasZoomLevel, setCanvasZoomLevel] = useState(1);
  const [canvasPanOffsetNS, setCanvasPanOffsetNS] = useState(0);
  const [brushMode, setBrushMode] = useState(false);
  const [brushRange, setBrushRange] = useState<[number, number] | null>(null);

  const timelineCanvasRef = useRef<TimelineCanvasHandle>(null);
  useImperativeHandle(ref, () => ({
    exportPng: () => timelineCanvasRef.current?.exportPng(),
    exportGif: (nFrames, fpsHint, onDone) =>
      timelineCanvasRef.current?.exportGif(nFrames, fpsHint, onDone),
  }), []);

  // When segmentsOverride is provided (e.g. Compare mode), bypass lazy-loading entirely.
  const useOverride = segmentsOverride !== undefined;

  // Load segments for a batch of goroutine IDs that haven't been fetched yet.
  const loadSegmentsBatch = useCallback(async (ids: number[]) => {
    const toLoad = ids.filter((id) => !loadedGoroutineIds.current.has(id));
    if (toLoad.length === 0) return;
    // Mark as loaded optimistically to prevent duplicate in-flight requests.
    toLoad.forEach((id) => loadedGoroutineIds.current.add(id));
    try {
      const data = await fetchTimeline({ goroutineIds: toLoad });
      if (data.length === 0) return;
      setSegmentMap((prev) => {
        const next = new Map(prev);
        for (const seg of data) {
          const list = next.get(seg.goroutine_id);
          if (list) list.push(seg);
          else next.set(seg.goroutine_id, [seg]);
        }
        return next;
      });
    } catch {
      // Roll back optimistic marks so the batch can be retried.
      toLoad.forEach((id) => loadedGoroutineIds.current.delete(id));
    }
  }, []);

  // When goroutine list changes (new session, filters, live reload) reset the segment cache.
  useEffect(() => {
    if (useOverride) return;
    setSegmentMap(new Map());
    loadedGoroutineIds.current = new Set();
    // Eagerly load the first batch so the timeline isn't blank on initial render.
    const firstBatch = goroutines.slice(0, SEGMENT_BATCH_SIZE).map((g) => g.goroutine_id);
    if (firstBatch.length > 0) loadSegmentsBatch(firstBatch);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [goroutines, useOverride]);

  // Re-fetch processor segments whenever goroutines are refreshed so that the
  // GMP strip is populated during live streaming, not only after the traced
  // process exits and the batch EOF flush runs.
  useEffect(() => {
    fetchProcessorTimeline()
      .then(setProcessorSegments)
      .catch(() => setProcessorSegments([]));
  }, [goroutines]);

  // Handle visible range change from TimelineCanvas — load missing segments.
  const handleVisibleRangeChange = useCallback(
    (firstIndex: number, lastIndex: number) => {
      if (useOverride) return;
      // Add buffer: load SEGMENT_BATCH_SIZE rows above and below the visible range.
      const bufStart = Math.max(0, firstIndex - SEGMENT_BATCH_SIZE);
      const bufEnd = Math.min(goroutines.length - 1, lastIndex + SEGMENT_BATCH_SIZE);
      const ids = goroutines.slice(bufStart, bufEnd + 1).map((g) => g.goroutine_id);
      loadSegmentsBatch(ids);
    },
    [goroutines, useOverride, loadSegmentsBatch]
  );

  // Build a flat segments array from the map (for scrub snapshot + minimap).
  const segments: TimelineSegment[] = useMemo(() => {
    if (useOverride) return segmentsOverride ?? [];
    const arr: TimelineSegment[] = [];
    for (const segs of segmentMap.values()) {
      for (const s of segs) arr.push(s);
    }
    return arr;
  }, [segmentMap, useOverride, segmentsOverride]);

  // Pre-build goroutine ID set for O(1) membership checks.
  const goroutineIdSet = useMemo(
    () => new Set(goroutines.map((g) => g.goroutine_id)),
    [goroutines]
  );

  const filteredSegments = useMemo(
    () => segments.filter((seg) => goroutineIdSet.has(seg.goroutine_id)),
    [segments, goroutineIdSet]
  );

  // Compute the historical state of each goroutine at scrubTimeNS.
  // Uses the pre-built segmentMap for O(goroutines) instead of O(goroutines × segments).
  const scrubSnapshot = useMemo<ScrubSnapshot[]>(() => {
    if (scrubTimeNS == null) return [];
    const result: ScrubSnapshot[] = [];
    // Iterate over loaded goroutines (not all segment-map keys) so we only scan relevant IDs.
    for (const g of goroutines) {
      const segs = (segmentMap.get(g.goroutine_id) ?? []).slice().sort((a, b) => a.start_ns - b.start_ns);
      if (segs.length === 0) continue;
      // Segment that contains scrubTimeNS.
      const active = segs.find((s) => s.start_ns <= scrubTimeNS && s.end_ns > scrubTimeNS);
      if (active) {
        result.push({ goroutine_id: g.goroutine_id, state: active.state, reason: active.reason || undefined });
        continue;
      }
      // No covering segment — use the last segment that ended before T.
      let lastBefore: TimelineSegment | undefined;
      for (const s of segs) {
        if (s.end_ns <= scrubTimeNS) lastBefore = s;
        else break;
      }
      if (lastBefore) {
        result.push({ goroutine_id: g.goroutine_id, state: lastBefore.state, reason: lastBefore.reason || undefined });
      }
    }
    return result;
  }, [scrubTimeNS, segmentMap, goroutines]);

  // Propagate snapshot to parent whenever it changes.
  useEffect(() => {
    onScrubSnapshot?.(scrubSnapshot);
  }, [scrubSnapshot, onScrubSnapshot]);

  // Propagate filtered segments so the goroutine sidebar can render lifetime bars.
  useEffect(() => {
    onSegmentsChange?.(filteredSegments);
  }, [filteredSegments, onSegmentsChange]);

  let fullMinStart = Infinity;
  let fullMaxEnd = -Infinity;
  for (const s of filteredSegments) {
    if (s.start_ns < fullMinStart) fullMinStart = s.start_ns;
    if (s.end_ns > fullMaxEnd) fullMaxEnd = s.end_ns;
  }
  if (fullMinStart === Infinity) fullMinStart = 0;
  if (fullMaxEnd === -Infinity) fullMaxEnd = 1;
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
      const segs = segmentMap.get(selectedId) ?? [];
      if (segs.length > 0) {
        let minStart = Infinity;
        let maxEnd = -Infinity;
        for (const s of segs) {
          if (s.start_ns < minStart) minStart = s.start_ns;
          if (s.end_ns > maxEnd) maxEnd = s.end_ns;
        }
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
          {scrubTimeNS != null && (
            <span className="scrub-indicator" title="Click axis to move · double-click or ESC to clear">
              ⏱ Scrubbing — ESC to clear
            </span>
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
            ref={timelineCanvasRef}
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
            scrubTimeNS={scrubTimeNS}
            onScrubChange={onScrubChange}
            onVisibleRangeChange={handleVisibleRangeChange}
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
});
