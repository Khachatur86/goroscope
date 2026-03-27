import type { Session, DeadlockHint } from "../api/client";
import type { FiltersState } from "../filters/url";
import { ThemeSwitcher } from "../theme/ThemeSwitcher";

interface TopbarProps {
  session: Session | null;
  goroutineCount: number;
  filteredCount: number;
  insights: { long_blocked_count: number; leak_candidates_count?: number };
  deadlockHints: DeadlockHint[];
  streamStatus: "connecting" | "live" | "disconnected";
  replayUploading: boolean;
  filters: FiltersState;
  onLongBlockedClick: () => void;
  onLeakClick: () => void;
  onDeadlockClick: () => void;
  onCopyLink: () => void;
  onRefresh: () => void;
  onOpenCapture: () => void;
  onCompare: () => void;
  onOpenPalette: () => void;
}

export function Topbar({
  session,
  goroutineCount,
  filteredCount,
  insights,
  deadlockHints,
  streamStatus,
  replayUploading,
  filters,
  onLongBlockedClick,
  onLeakClick,
  onDeadlockClick,
  onCopyLink,
  onRefresh,
  onOpenCapture,
  onCompare,
  onOpenPalette,
}: TopbarProps) {
  return (
    <header className="topbar">
      <div className="topbar-brand">
        <span className="topbar-title">Goroscope</span>
        <span className="topbar-legend">
          <span className="legend-chip running">RUN</span>
          <span className="legend-chip runnable">RUNNABLE</span>
          <span className="legend-chip waiting">WAIT</span>
          <span className="legend-chip blocked">BLOCK</span>
          <span className="legend-chip syscall">SYSCALL</span>
          <span className="legend-chip done">DONE</span>
        </span>
      </div>

      <div className="topbar-stats">
        <span className="topbar-stat" title={`Session: ${session?.name}`}>
          <span className="topbar-stat-label">Session</span>
          <strong>{session?.name ?? "—"}</strong>
        </span>
        <span className="topbar-stat-sep" />
        <span className="topbar-stat">
          <span className="topbar-stat-label">Goroutines</span>
          <strong>
            {filteredCount === goroutineCount
              ? goroutineCount
              : `${filteredCount}/${goroutineCount}`}
          </strong>
        </span>
        <span className="topbar-stat-sep" />
        <span
          className={`topbar-stat topbar-stat-btn ${filters.minWaitNs ? "active" : ""}`}
          role="button"
          tabIndex={0}
          title="Filter to long-blocked goroutines (≥1s)"
          onClick={onLongBlockedClick}
          onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onLongBlockedClick()}
        >
          <span className="topbar-stat-label">Long blocked</span>
          <strong>{insights.long_blocked_count}</strong>
        </span>
        <span className="topbar-stat-sep" />
        <span
          className={`topbar-stat topbar-stat-btn ${filters.showLeakOnly ? "active" : ""}`}
          role="button"
          tabIndex={0}
          title="Filter to leak candidates (≥30s)"
          onClick={onLeakClick}
          onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onLeakClick()}
        >
          <span className="topbar-stat-label">Leaks</span>
          <strong>{insights.leak_candidates_count ?? 0}</strong>
        </span>
        {deadlockHints.length > 0 && (
          <>
            <span className="topbar-stat-sep" />
            <span
              className="topbar-stat topbar-stat-btn topbar-stat-warn"
              role="button"
              tabIndex={0}
              title="View deadlock hints"
              onClick={onDeadlockClick}
              onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onDeadlockClick()}
            >
              <span className="topbar-stat-label">Deadlock</span>
              <strong>{deadlockHints.length}</strong>
            </span>
          </>
        )}
      </div>

      <div className="topbar-actions">
        <span
          className={`stream-status stream-status--${streamStatus}`}
          title={`Stream: ${streamStatus}`}
        >
          ● {streamStatus}
        </span>
        <button
          type="button"
          className="action-button palette-trigger"
          onClick={onOpenPalette}
          title="Command palette (⌘K)"
        >
          ⌘K
        </button>
        <button id="copy-link-btn" type="button" className="action-button secondary" onClick={onCopyLink}>
          Link
        </button>
        <button type="button" className="action-button" onClick={onRefresh}>
          Refresh
        </button>
        <button
          type="button"
          className="action-button secondary"
          onClick={onOpenCapture}
          disabled={replayUploading}
          title="Open .gtrace capture file (or drag-and-drop)"
        >
          {replayUploading ? "Loading…" : "Open"}
        </button>
        <button
          type="button"
          className="action-button secondary"
          onClick={onCompare}
          title="Compare two .gtrace captures"
        >
          Compare
        </button>
        <a
          href="/api/v1/replay/export"
          download
          className="action-button secondary"
          title="Download current session as .gtrace file"
        >
          Export
        </a>
        <ThemeSwitcher />
      </div>
    </header>
  );
}
