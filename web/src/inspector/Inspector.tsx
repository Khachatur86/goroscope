import { useEffect, useState } from "react";
import type { Goroutine, TimelineSegment, StackSnapshot } from "../api/client";
import { fetchStackAt, fetchPprofStacks } from "../api/client";
import { SpawnTree } from "./SpawnTree";
import { FlameGraph } from "./FlameGraph";

const OTEL_TRACE_ID_KEY = "otel.trace_id";
const OTEL_SPAN_ID_KEY = "otel.span_id";
const LS_JAEGER_URL = "goroscope:jaeger_url";
const LS_GRAFANA_URL = "goroscope:grafana_url";

function OTelSection({ labels }: { labels: Record<string, string> | undefined }) {
  const traceId = labels?.[OTEL_TRACE_ID_KEY];
  const spanId = labels?.[OTEL_SPAN_ID_KEY];
  const [jaegerUrl, setJaegerUrl] = useState(() => localStorage.getItem(LS_JAEGER_URL) ?? "");
  const [grafanaUrl, setGrafanaUrl] = useState(() => localStorage.getItem(LS_GRAFANA_URL) ?? "");
  const [configOpen, setConfigOpen] = useState(false);

  if (!traceId && !spanId) return null;

  const saveJaeger = (v: string) => {
    setJaegerUrl(v);
    if (v) localStorage.setItem(LS_JAEGER_URL, v);
    else localStorage.removeItem(LS_JAEGER_URL);
  };
  const saveGrafana = (v: string) => {
    setGrafanaUrl(v);
    if (v) localStorage.setItem(LS_GRAFANA_URL, v);
    else localStorage.removeItem(LS_GRAFANA_URL);
  };

  const jaegerHref = jaegerUrl && traceId
    ? `${jaegerUrl.replace(/\/$/, "")}/trace/${traceId}`
    : null;

  // Grafana Tempo explore URL (datasource=Tempo, query by traceId).
  const grafanaHref = grafanaUrl && traceId
    ? `${grafanaUrl.replace(/\/$/, "")}/explore?left=${encodeURIComponent(
        JSON.stringify({
          datasource: "Tempo",
          queries: [{ refId: "A", queryType: "traceql", query: `{.traceId="${traceId}"}` }],
        })
      )}`
    : null;

  const copyText = (v: string) => navigator.clipboard.writeText(v);

  return (
    <div className="inspector-section inspector-otel">
      <div className="inspector-otel-header">
        <span className="inspector-label">OpenTelemetry</span>
        <button
          type="button"
          className="otel-config-toggle"
          onClick={() => setConfigOpen((v) => !v)}
          title="Configure Jaeger / Grafana URLs"
        >
          ⚙
        </button>
      </div>

      {traceId && (
        <div className="otel-row">
          <span className="otel-key">trace_id</span>
          <code className="otel-val" title={traceId}>{traceId.slice(0, 16)}…</code>
          <button type="button" className="otel-copy" onClick={() => copyText(traceId)} title="Copy trace ID">⎘</button>
          {jaegerHref && (
            <a className="otel-link otel-link--jaeger" href={jaegerHref} target="_blank" rel="noreferrer">
              Jaeger
            </a>
          )}
          {grafanaHref && (
            <a className="otel-link otel-link--tempo" href={grafanaHref} target="_blank" rel="noreferrer">
              Tempo
            </a>
          )}
          {!jaegerHref && !grafanaHref && (
            <button type="button" className="otel-config-hint" onClick={() => setConfigOpen(true)}>
              configure links ↗
            </button>
          )}
        </div>
      )}

      {spanId && (
        <div className="otel-row">
          <span className="otel-key">span_id</span>
          <code className="otel-val" title={spanId}>{spanId}</code>
          <button type="button" className="otel-copy" onClick={() => copyText(spanId)} title="Copy span ID">⎘</button>
        </div>
      )}

      {configOpen && (
        <div className="otel-config">
          <label className="otel-config-label">
            Jaeger base URL
            <input
              className="otel-config-input"
              type="url"
              placeholder="http://localhost:16686"
              value={jaegerUrl}
              onChange={(e) => saveJaeger(e.target.value)}
            />
          </label>
          <label className="otel-config-label">
            Grafana base URL
            <input
              className="otel-config-input"
              type="url"
              placeholder="http://localhost:3000"
              value={grafanaUrl}
              onChange={(e) => saveGrafana(e.target.value)}
            />
          </label>
        </div>
      )}
    </div>
  );
}

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
  /** Substring from a "stack:<needle>" search — matching frames are highlighted. */
  stackFrameNeedle?: string;
  /** Whether the selected goroutine is pinned in the watchlist (U-3). */
  isPinned?: boolean;
  /** Current watchlist note for this goroutine (U-3). */
  pinnedNote?: string;
  /** Called when the user changes the watchlist note (U-3). */
  onSetNote?: (note: string) => void;
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

export function Inspector({ goroutine, goroutines, segmentOverride, onSelectGoroutine, onHighlightBranch, highlightActive, isScrubActive, stackFrameNeedle, isPinned, pinnedNote, onSetNote }: Props) {
  const [segmentStack, setSegmentStack] = useState<Goroutine["last_stack"] | null>(null);
  const [flameOpen, setFlameOpen] = useState(false);
  const [pprofOpen, setPprofOpen] = useState(false);
  const [pprofSamples, setPprofSamples] = useState<StackSnapshot[] | null>(null);

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

  useEffect(() => {
    if (!segmentOverride) {
      setPprofSamples(null);
      return;
    }
    let cancelled = false;
    fetchPprofStacks(segmentOverride.start_ns, segmentOverride.end_ns).then((stacks) => {
      if (!cancelled) setPprofSamples(stacks);
    });
    return () => { cancelled = true; };
  }, [segmentOverride?.start_ns, segmentOverride?.end_ns]);

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
        <div className={`badge badge--state ${state}`}>{state}</div>
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
      {isPinned && onSetNote && (
        <div className="inspector-section inspector-note-section">
          <div className="inspector-label">★ Watchlist note</div>
          <input
            type="text"
            className="inspector-note-input"
            value={pinnedNote ?? ""}
            maxLength={80}
            placeholder="Add a note…"
            onChange={(e) => onSetNote(e.target.value)}
          />
        </div>
      )}
      {goroutine.labels && Object.keys(goroutine.labels).length > 0 && (
        <div className="inspector-section">
          <div className="inspector-label">Labels</div>
          <div className="inspector-value inspector-labels">
            {Object.entries(goroutine.labels)
              .filter(([k]) => k !== "function" && !k.startsWith("otel."))
              .map(([k, v]) => (
                <span key={k} className="inspector-label-pair">
                  {k}={v}
                </span>
              ))}
          </div>
        </div>
      )}

      <OTelSection labels={goroutine.labels} />

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
          frames.map((frame, i) => {
            const needle = stackFrameNeedle?.toLowerCase();
            const isMatch = needle
              ? frame.func?.toLowerCase().includes(needle) ||
                (frame.file ?? "").toLowerCase().includes(needle)
              : false;
            return (
              <div
                key={i}
                className={`stack-frame${isMatch ? " stack-frame--match" : ""}`}
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
            );
          })
        ) : (
          <div className="empty-message">No stack snapshot yet.</div>
        )}
      </div>

      {/* ── CPU profile overlay (only when a segment is selected) ── */}
      {segmentOverride && (
        <div className="inspector-section">
          <button
            type="button"
            className="flame-toggle-btn"
            onClick={() => setPprofOpen((v) => !v)}
            aria-expanded={pprofOpen}
          >
            <span className="flame-toggle-icon">{pprofOpen ? "▾" : "▸"}</span>
            CPU profile @ segment
            <span className="flame-toggle-hint pprof-hint">
              {pprofOpen ? "hide" : `all goroutines · ${formatDuration(segmentOverride.end_ns - segmentOverride.start_ns)} window`}
            </span>
          </button>
          {pprofOpen && (
            <FlameGraph
              externalSamples={pprofSamples ?? []}
              emptyHint="No stack snapshots in this time window."
            />
          )}
        </div>
      )}

      {/* ── Flame graph ─────────────────────────────────────────── */}
      <div className="inspector-section">
        <button
          type="button"
          className="flame-toggle-btn"
          onClick={() => setFlameOpen((v) => !v)}
          aria-expanded={flameOpen}
        >
          <span className="flame-toggle-icon">{flameOpen ? "▾" : "▸"}</span>
          Flame graph
          <span className="flame-toggle-hint">
            {flameOpen ? "hide" : "aggregated samples"}
          </span>
        </button>
        {flameOpen && <FlameGraph goroutineId={goroutine.goroutine_id} />}
      </div>
    </div>
  );
}
