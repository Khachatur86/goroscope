import { useEffect, useState } from "react";
import type { Goroutine, TimelineSegment, ProcessorSegment } from "../api/client";
import { fetchTimeline, fetchProcessorTimeline } from "../api/client";
import { TimelineCanvas } from "./TimelineCanvas";
import { TimelineHeatmapCanvas } from "./TimelineHeatmapCanvas";
import { MinimapCanvas } from "./MinimapCanvas";

type FiltersState = {
  state: string;
  reason: string;
  resource: string;
  search: string;
};

type Props = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelectGoroutine: (id: number) => void;
  filters: FiltersState;
  zoomToSelected?: boolean;
  viewMode?: "lanes" | "heatmap";
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
}: Props) {
  const [segments, setSegments] = useState<TimelineSegment[]>([]);
  const [processorSegments, setProcessorSegments] = useState<ProcessorSegment[]>([]);
  const [canvasZoomLevel, setCanvasZoomLevel] = useState(1);
  const [canvasPanOffsetNS, setCanvasPanOffsetNS] = useState(0);

  useEffect(() => {
    fetchTimeline({
      state: filters.state !== "ALL" ? filters.state : undefined,
      reason: filters.reason || undefined,
      search: filters.search || undefined,
    })
      .then((data) => setSegments(Array.isArray(data) ? data : []))
      .catch(() => setSegments([]));
  }, [goroutines, filters.state, filters.reason, filters.search]);

  useEffect(() => {
    fetchProcessorTimeline()
      .then(setProcessorSegments)
      .catch(() => setProcessorSegments([]));
  }, []);

  const filteredSegments = (segments ?? []).filter((seg) =>
    (goroutines ?? []).some((g) => g.goroutine_id === seg.goroutine_id)
  );

  const fullMinStart = Math.min(...filteredSegments.map((s) => s.start_ns));
  const fullMaxEnd = Math.max(...filteredSegments.map((s) => s.end_ns));
  const fullSpan = Math.max(fullMaxEnd - fullMinStart, 1);
  const isHeatmap = viewMode === "heatmap";

  // Sync canvas zoom/pan when zoomToSelected or selected goroutine changes
  useEffect(() => {
    if (zoomToSelected && selectedId && filteredSegments.length > 0) {
      const selectedSegs = filteredSegments.filter((s) => s.goroutine_id === selectedId);
      if (selectedSegs.length > 0) {
        const minStart = Math.min(...selectedSegs.map((s) => s.start_ns));
        const maxEnd = Math.max(...selectedSegs.map((s) => s.end_ns));
        const padding = Math.max((maxEnd - minStart) * 0.1, 1);
        const visibleStart = Math.max(fullMinStart, minStart - padding);
        const visibleEnd = Math.min(fullMaxEnd, maxEnd + padding);
        const visibleSpan = visibleEnd - visibleStart;
        setCanvasPanOffsetNS(visibleStart - fullMinStart);
        setCanvasZoomLevel(fullSpan / visibleSpan);
        return;
      }
    }
    setCanvasZoomLevel(1);
    setCanvasPanOffsetNS(0);
  }, [zoomToSelected, selectedId, fullMinStart, fullSpan, filteredSegments]);

  if (filteredSegments.length === 0) {
    return (
      <div className="timeline-placeholder">
        Select a goroutine to see timeline segments.
      </div>
    );
  }

  const canvasVisibleSpan = fullSpan / canvasZoomLevel;
  const showMinimapCanvas = true;

  const visibleGoroutines = goroutines.slice(0, 50);

  return (
    <div className={`timeline-simple ${isHeatmap ? "timeline-heatmap" : ""}`}>
      <div className="timeline-legend">
        {Object.entries(COLORS).map(([state, color]) => (
          <span key={state} className="legend-chip" style={{ background: color }}>
            {state}
          </span>
        ))}
      </div>
      {isHeatmap ? (
        <div className="timeline-canvas-wrapper">
          <TimelineHeatmapCanvas
            goroutines={visibleGoroutines}
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
            goroutines={visibleGoroutines}
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
