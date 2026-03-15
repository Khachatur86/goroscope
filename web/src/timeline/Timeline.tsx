import { useEffect, useState } from "react";
import type { Goroutine, TimelineSegment, ProcessorSegment } from "../api/client";
import { fetchTimeline, fetchProcessorTimeline } from "../api/client";

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

function formatDuration(ns: number): string {
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(2)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(2)}µs`;
  return `${ns}ns`;
}

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

  if (filteredSegments.length === 0) {
    return (
      <div className="timeline-placeholder">
        Select a goroutine to see timeline segments.
      </div>
    );
  }

  const fullMinStart = Math.min(...filteredSegments.map((s) => s.start_ns));
  const fullMaxEnd = Math.max(...filteredSegments.map((s) => s.end_ns));

  let minStart = fullMinStart;
  let maxEnd = fullMaxEnd;
  if (zoomToSelected && selectedId) {
    const selectedSegs = filteredSegments.filter((s) => s.goroutine_id === selectedId);
    if (selectedSegs.length > 0) {
      minStart = Math.min(...selectedSegs.map((s) => s.start_ns));
      maxEnd = Math.max(...selectedSegs.map((s) => s.end_ns));
      const padding = Math.max((maxEnd - minStart) * 0.1, 1);
      minStart = Math.max(fullMinStart, minStart - padding);
      maxEnd = Math.min(fullMaxEnd, maxEnd + padding);
    }
  }

  const span = maxEnd - minStart || 1;
  const fullSpan = fullMaxEnd - fullMinStart || 1;
  const showMinimap = zoomToSelected && fullSpan > span * 1.1;

  const byGoroutine = new Map<number, TimelineSegment[]>();
  for (const seg of filteredSegments) {
    const list = byGoroutine.get(seg.goroutine_id) ?? [];
    list.push(seg);
    byGoroutine.set(seg.goroutine_id, list);
  }

  const visibleGoroutines = goroutines.slice(0, 50);
  const isHeatmap = viewMode === "heatmap";

  const processorIds = [...new Set(processorSegments.map((s) => s.processor_id))].sort((a, b) => a - b);
  const showGmpStrip = isHeatmap && processorIds.length > 0;

  const goroutineHue = (id: number) => {
    const hues = [195, 30, 270, 140, 355, 60, 310, 170, 80, 230, 15, 330];
    return hues[Number(id) % hues.length];
  };

  const axisTicks = [0, 0.25, 0.5, 0.75, 1].map((ratio) => ({
    left: ratio * 100,
    label: formatDuration(Math.round(ratio * span)),
  }));

  return (
    <div className={`timeline-simple ${isHeatmap ? "timeline-heatmap" : ""}`}>
      <div className="timeline-legend">
        {Object.entries(COLORS).map(([state, color]) => (
          <span key={state} className="legend-chip" style={{ background: color }}>
            {state}
          </span>
        ))}
      </div>
      <div className={`timeline-axis ${isHeatmap ? "timeline-axis-heatmap" : ""}`}>
        <div className="timeline-axis-label" />
        <div className="timeline-axis-track">
          {axisTicks.map((tick, i) => (
            <div
              key={i}
              className="timeline-axis-tick"
              style={{ left: `${tick.left}%` }}
              title={tick.label}
            >
              <span className="timeline-axis-tick-label">{tick.label}</span>
            </div>
          ))}
        </div>
      </div>
      {showGmpStrip && (
        <div className="gmp-strip">
          <div className="gmp-strip-label">GMP</div>
          <div className="gmp-strip-rows">
            {processorIds.map((pid) => {
              const pSegs = processorSegments
                .filter((s) => s.processor_id === pid)
                .filter((s) => s.start_ns < maxEnd && s.end_ns > minStart);
              return (
                <div key={pid} className="gmp-strip-row">
                  <div className="gmp-strip-row-label">P{pid}</div>
                  <div className="gmp-strip-row-segments">
                    {pSegs.map((seg, i) => (
                      <div
                        key={i}
                        className="gmp-strip-segment"
                        style={{
                          left: `${((seg.start_ns - minStart) / span) * 100}%`,
                          width: `${((seg.end_ns - seg.start_ns) / span) * 100}%`,
                          backgroundColor: `hsl(${goroutineHue(seg.goroutine_id)}, 70%, 58%)`,
                        }}
                        title={`G${seg.goroutine_id}`}
                        onClick={(e) => {
                          e.stopPropagation();
                          onSelectGoroutine(seg.goroutine_id);
                        }}
                      />
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
      <div className={`timeline-lanes ${isHeatmap ? "timeline-lanes-heatmap" : ""}`}>
        {visibleGoroutines.map((g) => {
          const segs = byGoroutine.get(g.goroutine_id) ?? [];
          const isSelected = g.goroutine_id === selectedId;

          return (
            <div
              key={g.goroutine_id}
              className={`timeline-lane ${isSelected ? "selected" : ""} ${isHeatmap ? "timeline-lane-heatmap" : ""}`}
              onClick={() => onSelectGoroutine(g.goroutine_id)}
            >
              <div className="lane-label">
                G{g.goroutine_id}{isHeatmap ? "" : ` ${g.labels?.function ?? ""}`}
              </div>
              <div className="lane-segments">
                {segs.map((seg, i) => (
                  <div
                    key={i}
                    className="lane-segment"
                    style={{
                      left: `${((seg.start_ns - minStart) / span) * 100}%`,
                      width: `${((seg.end_ns - seg.start_ns) / span) * 100}%`,
                      backgroundColor: COLORS[seg.state] ?? "#666",
                    }}
                    title={`${seg.state} ${formatDuration(seg.end_ns - seg.start_ns)}`}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>
      {showMinimap && (
        <div className="timeline-minimap" title="Zoomed viewport in full trace">
          <div className="timeline-minimap-track">
            <div
              className="timeline-minimap-viewport"
              style={{
                left: `${((minStart - fullMinStart) / fullSpan) * 100}%`,
                width: `${(span / fullSpan) * 100}%`,
              }}
            />
          </div>
        </div>
      )}
    </div>
  );
}
