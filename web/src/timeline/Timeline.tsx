import { useEffect, useState } from "react";
import type { Goroutine, TimelineSegment } from "../api/client";
import { fetchTimeline } from "../api/client";

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
}: Props) {
  const [segments, setSegments] = useState<TimelineSegment[]>([]);

  useEffect(() => {
    fetchTimeline({
      state: filters.state !== "ALL" ? filters.state : undefined,
      reason: filters.reason || undefined,
      search: filters.search || undefined,
    })
      .then((data) => setSegments(Array.isArray(data) ? data : []))
      .catch(() => setSegments([]));
  }, [goroutines, filters.state, filters.reason, filters.search]);

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

  const minStart = Math.min(...filteredSegments.map((s) => s.start_ns));
  const maxEnd = Math.max(...filteredSegments.map((s) => s.end_ns));
  const span = maxEnd - minStart || 1;

  const byGoroutine = new Map<number, TimelineSegment[]>();
  for (const seg of filteredSegments) {
    const list = byGoroutine.get(seg.goroutine_id) ?? [];
    list.push(seg);
    byGoroutine.set(seg.goroutine_id, list);
  }

  const visibleGoroutines = goroutines.slice(0, 50);

  return (
    <div className="timeline-simple">
      <div className="timeline-legend">
        {Object.entries(COLORS).map(([state, color]) => (
          <span key={state} className="legend-chip" style={{ background: color }}>
            {state}
          </span>
        ))}
      </div>
      <div className="timeline-lanes">
        {visibleGoroutines.map((g) => {
          const segs = byGoroutine.get(g.goroutine_id) ?? [];
          const isSelected = g.goroutine_id === selectedId;

          return (
            <div
              key={g.goroutine_id}
              className={`timeline-lane ${isSelected ? "selected" : ""}`}
              onClick={() => onSelectGoroutine(g.goroutine_id)}
            >
              <div className="lane-label">
                G{g.goroutine_id} {g.labels?.function ?? ""}
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
    </div>
  );
}
