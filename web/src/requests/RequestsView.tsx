import { useEffect, useState, useCallback } from "react";
import type { RequestGroup } from "../api/client";
import { fetchRequestGroups } from "../api/client";

type Props = {
  /** Called when the user clicks a request — filters goroutine list to its IDs. */
  onSelectRequest?: (ids: Set<number>) => void;
};

function formatDuration(ns: number): string {
  if (ns <= 0) return "—";
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(1)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(0)}µs`;
  return `${ns}ns`;
}

const STATE_COLORS: Record<string, string> = {
  RUNNING:  "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING:  "#f59e0b",
  BLOCKED:  "#f43f5e",
  SYSCALL:  "#4da6ff",
  DONE:     "#4b5563",
};

function StateBar({ breakdown, total }: { breakdown: Record<string, number>; total: number }) {
  if (total === 0) return null;
  const entries = Object.entries(breakdown).filter(([, v]) => v > 0);
  return (
    <div className="req-state-bar" title={entries.map(([s, n]) => `${s}: ${n}`).join(" · ")}>
      {entries.map(([state, count]) => (
        <div
          key={state}
          className="req-state-bar-segment"
          style={{ width: `${(count / total) * 100}%`, background: STATE_COLORS[state] ?? "#64748b" }}
        />
      ))}
    </div>
  );
}

export function RequestsView({ onSelectRequest }: Props) {
  const [groups, setGroups] = useState<RequestGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [search, setSearch] = useState("");

  const reload = useCallback(() => {
    setLoading(true);
    fetchRequestGroups()
      .then((res) => setGroups(res.groups))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  const handleSelect = useCallback((rg: RequestGroup) => {
    setSelectedId((prev) => (prev === rg.request_id ? null : rg.request_id));
    onSelectRequest?.(new Set(rg.goroutine_ids));
  }, [onSelectRequest]);

  const filtered = search.trim()
    ? groups.filter(
        (g) =>
          g.request_id.toLowerCase().includes(search.toLowerCase()) ||
          (g.url ?? "").toLowerCase().includes(search.toLowerCase()) ||
          (g.method ?? "").toLowerCase().includes(search.toLowerCase())
      )
    : groups;

  if (loading) {
    return <div className="req-empty">Loading request groups…</div>;
  }

  if (groups.length === 0) {
    return (
      <div className="req-empty">
        No HTTP request groups found.
        <span className="req-empty-hint">
          Request groups appear when goroutines carry{" "}
          <code>http.request_id</code>, <code>request_id</code>, or{" "}
          <code>trace_id</code> pprof labels — or when a{" "}
          <code>net/http.(*conn).serve</code> frame is present in the stack.
        </span>
      </div>
    );
  }

  return (
    <div className="req-view">
      <div className="req-toolbar">
        <input
          className="req-search"
          type="text"
          placeholder="Filter by ID, URL, method…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <span className="req-count">{filtered.length} of {groups.length} requests</span>
        <button type="button" className="req-refresh-btn" onClick={reload} title="Refresh">
          ↻
        </button>
      </div>
      <div className="req-list">
        {filtered.map((rg) => (
          <div
            key={rg.request_id}
            className={`req-row ${selectedId === rg.request_id ? "req-row--selected" : ""}`}
            onClick={() => handleSelect(rg)}
          >
            <div className="req-row-header">
              {rg.method && <span className="req-method">{rg.method}</span>}
              <span className="req-url" title={rg.url ?? rg.request_id}>
                {rg.url ?? rg.request_id}
              </span>
              <span className={`req-source req-source--${rg.source}`}>{rg.source}</span>
            </div>
            <div className="req-row-meta">
              <span className="req-meta-item" title="Duration">{formatDuration(rg.duration_ns)}</span>
              <span className="req-meta-item" title="Goroutine count">{rg.goroutine_count}G</span>
              <StateBar breakdown={rg.state_breakdown} total={rg.goroutine_count} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
