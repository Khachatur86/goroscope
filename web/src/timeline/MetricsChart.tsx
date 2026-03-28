import { useMemo } from "react";
import type { TimelineSegment } from "../api/client";
import { STATE_COLORS, COLOR_RANGE } from "../theme/tokens";

const BLOCKED_STATES = new Set(["BLOCKED", "WAITING", "SYSCALL"]);

type Props = {
  segments: TimelineSegment[];
  width?: number;
  height?: number;
  /** When set, draws a highlighted rect over the selected NS range. */
  highlightRange?: [number, number] | null;
};

export function MetricsChart({ segments, width = 280, height = 60, highlightRange }: Props) {
  const { buckets, minNS, maxNS } = useMemo(() => {
    if (segments.length === 0) {
      return { buckets: [] as { total: number; blocked: number }[], minNS: 0, maxNS: 0 };
    }
    const min = Math.min(...segments.map((s) => s.start_ns));
    const max = Math.max(...segments.map((s) => s.end_ns));
    const span = Math.max(max - min, 1);
    const bucketCount = 40;
    const bucketNs = span / bucketCount;
    const buckets: { total: Set<number>; blocked: Set<number> }[] = Array.from(
      { length: bucketCount },
      () => ({ total: new Set<number>(), blocked: new Set<number>() })
    );

    for (const seg of segments) {
      const startBucket = Math.max(0, Math.floor((seg.start_ns - min) / bucketNs));
      const endBucket = Math.min(
        bucketCount - 1,
        Math.floor((seg.end_ns - min) / bucketNs)
      );
      for (let b = startBucket; b <= endBucket; b++) {
        buckets[b].total.add(seg.goroutine_id);
        if (BLOCKED_STATES.has(seg.state)) {
          buckets[b].blocked.add(seg.goroutine_id);
        }
      }
    }

    const bucketCounts = buckets.map((b) => ({
      total: b.total.size,
      blocked: b.blocked.size,
    }));
    return { buckets: bucketCounts, minNS: min, maxNS: max };
  }, [segments]);

  if (buckets.length === 0) return null;

  const maxTotal = Math.max(1, ...buckets.map((b) => b.total));
  const padding = { top: 4, right: 4, bottom: 16, left: 4 };
  const chartW = width - padding.left - padding.right;
  const chartH = height - padding.top - padding.bottom;
  const totalSpan = Math.max(maxNS - minNS, 1);

  const toX = (i: number) => padding.left + (i / (buckets.length - 1 || 1)) * chartW;
  const toYTotal = (v: number) =>
    padding.top + chartH - (v / maxTotal) * chartH;
  const toYBlocked = (v: number) =>
    padding.top + chartH - (v / maxTotal) * chartH;

  const totalPath = buckets
    .map((b, i) => `${i === 0 ? "M" : "L"} ${toX(i)} ${toYTotal(b.total)}`)
    .join(" ");
  const blockedPath = buckets
    .map((b, i) => `${i === 0 ? "M" : "L"} ${toX(i)} ${toYBlocked(b.blocked)}`)
    .join(" ");

  // Compute highlight rect in SVG coords
  let hlX: number | null = null;
  let hlW: number | null = null;
  if (highlightRange) {
    const [startNS, endNS] = highlightRange;
    const x1 = padding.left + ((startNS - minNS) / totalSpan) * chartW;
    const x2 = padding.left + ((endNS - minNS) / totalSpan) * chartW;
    hlX = Math.max(padding.left, Math.min(x1, padding.left + chartW));
    hlW = Math.min(padding.left + chartW, x2) - hlX;
  }

  return (
    <div className="metrics-chart">
      <svg width={width} height={height} aria-hidden>
        <defs>
          <linearGradient id="metrics-total" x1="0" y1="1" x2="0" y2="0">
            <stop offset="0" stopColor={STATE_COLORS.RUNNING} stopOpacity="0.3" />
            <stop offset="1" stopColor={STATE_COLORS.RUNNING} stopOpacity="0" />
          </linearGradient>
          <linearGradient id="metrics-blocked" x1="0" y1="1" x2="0" y2="0">
            <stop offset="0" stopColor={STATE_COLORS.BLOCKED} stopOpacity="0.4" />
            <stop offset="1" stopColor={STATE_COLORS.BLOCKED} stopOpacity="0" />
          </linearGradient>
        </defs>
        <path
          d={`${totalPath} L ${toX(buckets.length - 1)} ${padding.top + chartH} L ${padding.left} ${padding.top + chartH} Z`}
          fill="url(#metrics-total)"
        />
        <path d={totalPath} fill="none" stroke={STATE_COLORS.RUNNING} strokeWidth="1.5" />
        <path
          d={`${blockedPath} L ${toX(buckets.length - 1)} ${padding.top + chartH} L ${padding.left} ${padding.top + chartH} Z`}
          fill="url(#metrics-blocked)"
        />
        <path d={blockedPath} fill="none" stroke={STATE_COLORS.BLOCKED} strokeWidth="1.5" />
        {hlX !== null && hlW !== null && hlW > 0 && (
          <>
            <rect
              x={hlX}
              y={padding.top}
              width={hlW}
              height={chartH}
              fill="rgba(56, 189, 248, 0.15)"
            />
            <line x1={hlX} y1={padding.top} x2={hlX} y2={padding.top + chartH}
              stroke="rgba(56, 189, 248, 0.7)" strokeWidth="1.5" />
            <line x1={hlX + hlW} y1={padding.top} x2={hlX + hlW} y2={padding.top + chartH}
              stroke="rgba(56, 189, 248, 0.7)" strokeWidth="1.5" />
          </>
        )}
      </svg>
      <div className="metrics-chart-legend">
        <span style={{ color: STATE_COLORS.RUNNING }}>●</span> active
        <span style={{ color: STATE_COLORS.BLOCKED, marginLeft: "0.75rem" }}>●</span> blocked
        {highlightRange && (
          <span style={{ color: COLOR_RANGE, marginLeft: "0.75rem" }}>⌖ range active</span>
        )}
      </div>
    </div>
  );
}
