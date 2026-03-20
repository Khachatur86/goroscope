import { useEffect, useState } from "react";
import type { Goroutine, TimelineSegment } from "../api/client";
import { fetchStackAt } from "../api/client";
import { SpawnTree } from "./SpawnTree";

type Props = {
  goroutine: Goroutine | null;
  goroutines: Goroutine[];
  segmentOverride?: TimelineSegment | null;
  onSelectGoroutine?: (id: number) => void;
  onHighlightBranch?: (ids: Set<number> | null) => void;
  highlightActive?: boolean;
  /**
   * When true the segmentOverride was synthesised from the time scrubber
   * rather than a user click. Changes badge text and stack section label.
   */
  isScrubActive?: boolean;
};

function formatDuration(ns: number): string {
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(2)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(2)}µs`;
  return `${ns}ns`;
}

function formatTimestamp(s?: string): string {
  if (!s) return "—";
  try {
    const d = new Date(s);
    return d.toLocaleTimeString();
  } catch {
    return s;
  }
}

export function Inspector({ goroutine, goroutines, segmentOverride, onSelectGoroutine, onHighlightBranch, highlightActive, isScrubActive }: Props) {
  const [segmentStack, setSegmentStack] = useState<Goroutine["last_stack"] | null>(null);

  useEffect(() => {
    if (!segmentOverride || !goroutine) {
      setSegmentStack(null);
      return;
    }
    let cancelled = false;
    fetchStackAt(goroutine.goroutine_id, segmentOverride.start_ns).then((res) => {
      if (!cancelled && res?.frames) {
        setSegmentStack({ frames: res.frames });
      } else {
        setSegmentStack(null);
      }
    });
    return () => { cancelled = true; };
  }, [segmentOverride, goroutine?.goroutine_id]);

  if (!goroutine) {
    return (
      <div className="inspector empty">Pick a goroutine to inspect.</div>
    );
  }

  const frames = (segmentOverride && segmentStack ? segmentStack.frames : goroutine.last_stack?.frames) ?? [];

  const copyStack = () => {
    const text = frames
      .map((f) => `${f.func}\n\t${f.file || "?"}:${f.line ?? 0}`)
      .join("\n");
    navigator.clipboard.writeText(`goroutine #${goroutine.goroutine_id}\n\n${text}`);
  };

  const copyId = () => {
    navigator.clipboard.writeText(String(goroutine.goroutine_id));
  };

  const goroutinesList = goroutines ?? [];

  const state = segmentOverride?.state ?? goroutine.state;
  const reason = segmentOverride?.reason ?? goroutine.reason;
  const resource = segmentOverride?.resource_id ?? goroutine.resource_id;

  return (
    <div className="inspector">
      <div className="inspector-section">
        <div className={`state-pill ${state}`}>{state}</div>
        {segmentOverride && (
          <span
            className={`inspector-segment-hint${isScrubActive ? " inspector-segment-hint--scrub" : ""}`}
            title={isScrubActive ? "Historical state at scrub point" : "State at clicked segment"}
          >
            {isScrubActive ? "⏱ scrub" : "@ segment"}
          </span>
        )}
      </div>
      <div className="inspector-section inspector-grid">
        <div>
          <div className="inspector-label">Goroutine</div>
          <div className="inspector-value inspector-goroutine-id">
            #{goroutine.goroutine_id}
            <button type="button" className="inspector-copy-id" onClick={copyId} title="Copy goroutine ID">
              ⎘
            </button>
          </div>
        </div>
        <div>
          <div className="inspector-label">Wait Time</div>
          <div className="inspector-value">
            {segmentOverride
              ? formatDuration(segmentOverride.end_ns - segmentOverride.start_ns)
              : formatDuration(goroutine.wait_ns ?? 0)}
          </div>
        </div>
        <div>
          <div className="inspector-label">Reason</div>
          <div className="inspector-value">{reason ?? "—"}</div>
        </div>
        <div>
          <div className="inspector-label">Resource</div>
          <div className="inspector-value">{resource ?? "—"}</div>
        </div>
        <div>
          <div className="inspector-label">Created</div>
          <div className="inspector-value">{formatTimestamp(goroutine.created_at)}</div>
        </div>
        <div>
          <div className="inspector-label">Last Seen</div>
          <div className="inspector-value">{formatTimestamp(goroutine.last_seen_at)}</div>
        </div>
      </div>
      <div className="inspector-section">
        <div className="inspector-label">Function</div>
        <div className="inspector-value">{goroutine.labels?.function ?? "—"}</div>
      </div>
      {goroutine.labels && Object.keys(goroutine.labels).length > 0 && (
        <div className="inspector-section">
          <div className="inspector-label">Labels</div>
          <div className="inspector-value inspector-labels">
            {Object.entries(goroutine.labels)
              .filter(([k]) => k !== "function")
              .map(([k, v]) => (
                <span key={k} className="inspector-label-pair">
                  {k}={v}
                </span>
              ))}
          </div>
        </div>
      )}

      <div className="inspector-section">
        <div className="inspector-label">Spawn Tree</div>
        <SpawnTree
          goroutine={goroutine}
          allGoroutines={goroutinesList}
          onSelectGoroutine={onSelectGoroutine}
          onHighlightBranch={onHighlightBranch}
          highlightActive={highlightActive}
        />
      </div>

      <div className="inspector-section">
        <div className="inspector-stack-header">
          <span className="inspector-label">
            {segmentOverride && segmentStack
              ? isScrubActive
                ? "Stack at ⏱ scrub point"
                : "Stack at segment"
              : "Latest Stack"}
          </span>
          {frames.length > 0 && (
            <button type="button" className="inspector-copy-stack" onClick={copyStack}>
              Copy
            </button>
          )}
        </div>
        {frames.length > 0 ? (
          frames.map((frame, i) => (
            <div
              key={i}
              className="stack-frame"
              data-file={frame.file}
              data-line={frame.line}
              onClick={() => {
                if (frame.file && window.parent !== window) {
                  window.parent.postMessage(
                    { type: "goroscope:openFile", file: frame.file, line: frame.line },
                    "*"
                  );
                }
              }}
              role={frame.file ? "button" : undefined}
              tabIndex={frame.file ? 0 : undefined}
            >
              <div className="stack-func">{frame.func}</div>
              <div className="stack-path">
                {frame.file}:{frame.line}
              </div>
            </div>
          ))
        ) : (
          <div className="empty-message">No stack snapshot yet.</div>
        )}
      </div>
    </div>
  );
}
