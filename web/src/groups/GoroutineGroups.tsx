import { useState, useEffect, useCallback } from "react";
import type { GoroutineGroup, GroupByField } from "../api/client";
import { fetchGoroutineGroups } from "../api/client";

type Props = {
  /** Called when the user clicks a goroutine ID badge inside a group row. */
  onSelectGoroutine?: (id: number) => void;
};

/** Format nanoseconds as a human-readable duration string (µs / ms / s). */
function fmtNs(ns: number): string {
  if (ns <= 0) return "—";
  if (ns < 1_000) return `${ns}ns`;
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(1)}µs`;
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`;
  return `${(ns / 1_000_000_000).toFixed(2)}s`;
}

const GROUP_BY_OPTIONS: { value: GroupByField; label: string }[] = [
  { value: "function", label: "Function" },
  { value: "package", label: "Package" },
  { value: "parent_id", label: "Parent goroutine" },
  { value: "label", label: "Label" },
];

const STATE_ORDER = ["RUNNING", "RUNNABLE", "BLOCKED", "WAITING", "SYSCALL", "DONE"];

function StatePills({ states }: { states: Record<string, number> }) {
  return (
    <span className="groups-state-pills">
      {STATE_ORDER.filter((s) => (states[s] ?? 0) > 0).map((s) => (
        <span key={s} className={`badge badge--state ${s}`} title={`${s}: ${states[s]}`}>
          {states[s]}
        </span>
      ))}
    </span>
  );
}

function GroupRow({
  group,
  onSelectGoroutine,
}: {
  group: GoroutineGroup;
  onSelectGoroutine?: (id: number) => void;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="groups-row">
      <button
        type="button"
        className="groups-row-header"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        <span className="groups-expand-icon">{expanded ? "▾" : "▸"}</span>
        <span className="groups-key" title={group.key}>
          {group.key}
        </span>
        <span className="groups-count">{group.count}</span>
        <StatePills states={group.states} />
        <span className="groups-metric" title="Average wait time">
          avg {fmtNs(group.avg_wait_ns)}
        </span>
        <span className="groups-metric" title="Total CPU time (RUNNING segments)">
          cpu {fmtNs(group.total_cpu_ns)}
        </span>
      </button>

      {expanded && (
        <div className="groups-row-body">
          <table className="groups-metrics-table">
            <tbody>
              <tr>
                <td>Max wait</td>
                <td>{fmtNs(group.max_wait_ns)}</td>
                <td>Total wait</td>
                <td>{fmtNs(group.total_wait_ns)}</td>
              </tr>
              <tr>
                <td>Avg wait</td>
                <td>{fmtNs(group.avg_wait_ns)}</td>
                <td>Total CPU</td>
                <td>{fmtNs(group.total_cpu_ns)}</td>
              </tr>
            </tbody>
          </table>
          <div className="groups-goroutine-ids">
            {group.goroutine_ids.map((id) => (
              <button
                key={id}
                type="button"
                className="groups-goroutine-id-badge"
                onClick={() => onSelectGoroutine?.(id)}
                title={`Select G${id}`}
              >
                G{id}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function GoroutineGroups({ onSelectGoroutine }: Props) {
  const [groupBy, setGroupBy] = useState<GroupByField>("function");
  const [labelKey, setLabelKey] = useState("");
  const [groups, setGroups] = useState<GoroutineGroup[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchGoroutineGroups({
        by: groupBy,
        ...(groupBy === "label" && labelKey ? { label_key: labelKey } : {}),
      });
      setGroups(data.groups ?? []);
      setTotal(data.total ?? 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load groups");
      setGroups([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
  }, [groupBy, labelKey]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <div className="groups-panel">
      <div className="groups-controls">
        <label className="groups-control-label" htmlFor="groups-by-select">
          Group by
        </label>
        <select
          id="groups-by-select"
          className="groups-by-select"
          value={groupBy}
          onChange={(e) => setGroupBy(e.target.value as GroupByField)}
        >
          {GROUP_BY_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        {groupBy === "label" && (
          <input
            type="text"
            className="groups-label-key-input"
            placeholder="label key (e.g. request_id)"
            value={labelKey}
            onChange={(e) => setLabelKey(e.target.value)}
            aria-label="Label key to group by"
          />
        )}
        <button type="button" className="btn btn--secondary groups-refresh-btn" onClick={load}>
          ↻
        </button>
      </div>

      {loading && <p className="groups-status">Loading…</p>}
      {error && <p className="groups-status groups-error">{error}</p>}

      {!loading && !error && (
        <>
          <p className="groups-summary">
            {total} {total === 1 ? "group" : "groups"}
          </p>
          <div className="groups-list">
            {groups.length === 0 ? (
              <p className="empty-message">No goroutines to group.</p>
            ) : (
              groups.map((g) => (
                <GroupRow
                  key={`${g.by}:${g.key}`}
                  group={g}
                  onSelectGoroutine={onSelectGoroutine}
                />
              ))
            )}
          </div>
        </>
      )}
    </div>
  );
}
