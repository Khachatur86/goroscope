import type { TimelineSegment } from "../api/client";

export const LIFETIME_COLORS: Record<string, string> = {
  RUNNING: "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING: "#f59e0b",
  BLOCKED: "#f43f5e",
  SYSCALL: "#4da6ff",
  DONE: "#4b5563",
};

/** Thin colour strip at the bottom of a goroutine list row showing its full lifecycle. */
export function LifetimeBar({ segments }: { segments: TimelineSegment[] | undefined }) {
  if (!segments || segments.length === 0) {
    return <div className="lifetime-bar lifetime-bar--empty" />;
  }
  let minStart = Infinity;
  let maxEnd = -Infinity;
  for (const s of segments) {
    if (s.start_ns < minStart) minStart = s.start_ns;
    if (s.end_ns > maxEnd) maxEnd = s.end_ns;
  }
  if (minStart === Infinity) minStart = 0;
  if (maxEnd === -Infinity) maxEnd = 1;
  const span = Math.max(maxEnd - minStart, 1);
  const sorted = [...segments].sort((a, b) => a.start_ns - b.start_ns);

  const stops: string[] = [];
  let cursor = 0;
  for (const seg of sorted) {
    const x1 = ((seg.start_ns - minStart) / span) * 100;
    const x2 = ((seg.end_ns - minStart) / span) * 100;
    if (x1 > cursor + 0.01) {
      stops.push(`#1e293b ${cursor.toFixed(2)}%`, `#1e293b ${x1.toFixed(2)}%`);
    }
    const color = LIFETIME_COLORS[seg.state] ?? "#94a3b8";
    stops.push(`${color} ${x1.toFixed(2)}%`, `${color} ${x2.toFixed(2)}%`);
    cursor = x2;
  }
  if (cursor < 99.99) {
    stops.push(`#1e293b ${cursor.toFixed(2)}%`, `#1e293b 100%`);
  }

  return (
    <div
      className="lifetime-bar"
      style={{ background: `linear-gradient(to right, ${stops.join(", ")})` }}
      title={`${segments.length} segments`}
    />
  );
}
