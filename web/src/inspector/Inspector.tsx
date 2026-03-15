import type { Goroutine } from "../api/client";

type Props = {
  goroutine: Goroutine | null;
  onSelectGoroutine?: (id: number) => void;
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

export function Inspector({ goroutine }: Props) {
  if (!goroutine) {
    return (
      <div className="inspector empty">Pick a goroutine to inspect.</div>
    );
  }

  const frames = goroutine.last_stack?.frames ?? [];

  return (
    <div className="inspector">
      <div className="inspector-section">
        <div className="state-pill">{goroutine.state}</div>
      </div>
      <div className="inspector-grid">
        <div>
          <div className="inspector-label">Goroutine</div>
          <div className="inspector-value">#{goroutine.goroutine_id}</div>
        </div>
        <div>
          <div className="inspector-label">Wait Time</div>
          <div className="inspector-value">
            {formatDuration(goroutine.wait_ns ?? 0)}
          </div>
        </div>
        <div>
          <div className="inspector-label">Reason</div>
          <div className="inspector-value">{goroutine.reason ?? "—"}</div>
        </div>
        <div>
          <div className="inspector-label">Resource</div>
          <div className="inspector-value">{goroutine.resource_id ?? "—"}</div>
        </div>
        <div>
          <div className="inspector-label">Created</div>
          <div className="inspector-value">
            {formatTimestamp(goroutine.created_at)}
          </div>
        </div>
        <div>
          <div className="inspector-label">Last Seen</div>
          <div className="inspector-value">
            {formatTimestamp(goroutine.last_seen_at)}
          </div>
        </div>
      </div>
      <div className="inspector-section">
        <div className="inspector-label">Function</div>
        <div className="inspector-value">
          {goroutine.labels?.function ?? "—"}
        </div>
      </div>
      <div className="inspector-section">
        <div className="inspector-label">Latest Stack</div>
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
