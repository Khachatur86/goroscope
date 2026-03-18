import { useState, useMemo } from "react";

type ResourceEdge = {
  from_goroutine_id: number;
  to_goroutine_id: number;
  resource_id?: string;
};

type ResourceContention = {
  resource_id: string;
  peak_waiters: number;
  segment_count: number;
  total_wait_ns: number;
  avg_wait_ns: number;
};

function formatDuration(ns: number): string {
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(2)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(2)}µs`;
  return `${ns}ns`;
}

type Props = {
  resources: ResourceEdge[];
  contention: ResourceContention[];
  selectedId: number | null;
  onSelectGoroutine: (id: number) => void;
};

type ContentionSort = "peak" | "avg";

export function ResourceGraph({ resources, contention, selectedId, onSelectGoroutine }: Props) {
  const [expanded, setExpanded] = useState(false);
  const [view, setView] = useState<"graph" | "contention">("graph");
  const [contentionSort, setContentionSort] = useState<ContentionSort>("peak");

  const sortedContention = useMemo(() => {
    const copy = [...contention];
    if (contentionSort === "peak") {
      copy.sort((a, b) => b.peak_waiters - a.peak_waiters);
    } else {
      copy.sort((a, b) => b.avg_wait_ns - a.avg_wait_ns);
    }
    return copy;
  }, [contention, contentionSort]);

  const hasData = resources.length > 0 || contention.length > 0;
  if (!hasData) return null;

  const maxPeak = contention.length > 0
    ? Math.max(...contention.map((c) => c.peak_waiters), 1)
    : 1;

  return (
    <div className="resource-section">
      <button
        type="button"
        className="section-toggle"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        Resource Graph{" "}
        <span className="section-count">
          ({view === "graph" ? resources.length : contention.length})
        </span>
      </button>
      {expanded && (
        <>
          <div className="resource-graph-view-toggle">
            <button
              type="button"
              className={`resource-graph-view-btn ${view === "graph" ? "active" : ""}`}
              onClick={() => setView("graph")}
            >
              Edges
            </button>
            <button
              type="button"
              className={`resource-graph-view-btn ${view === "contention" ? "active" : ""}`}
              onClick={() => setView("contention")}
              title="Peak concurrent waiters and average wait duration per resource"
            >
              Contention
            </button>
          </div>
          {view === "graph" ? (
            <div className="resource-graph-table">
              <table className="resource-graph-table-inner">
                <thead>
                  <tr>
                    <th>From</th>
                    <th>To</th>
                    <th>Resource</th>
                  </tr>
                </thead>
                <tbody>
                  {resources.map((edge, i) => {
                    const involvesSelected =
                      selectedId !== null &&
                      (edge.from_goroutine_id === selectedId || edge.to_goroutine_id === selectedId);
                    return (
                      <tr
                        key={i}
                        className={involvesSelected ? "resource-graph-row resource-graph-row-highlight" : "resource-graph-row"}
                      >
                        <td>
                          <button
                            type="button"
                            className="resource-graph-gid"
                            onClick={() => onSelectGoroutine(edge.from_goroutine_id)}
                          >
                            G{edge.from_goroutine_id}
                          </button>
                        </td>
                        <td>
                          <button
                            type="button"
                            className="resource-graph-gid"
                            onClick={() => onSelectGoroutine(edge.to_goroutine_id)}
                          >
                            G{edge.to_goroutine_id}
                          </button>
                        </td>
                        <td className="resource-graph-resource">{edge.resource_id ?? "—"}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="resource-contention-list">
              {contention.length === 0 ? (
                <p className="resource-contention-empty">No contention data (no segments with resource_id)</p>
              ) : (
                <table className="resource-graph-table-inner">
                  <thead>
                    <tr>
                      <th>Resource</th>
                      <th>
                        <button
                          type="button"
                          className={`resource-contention-sort ${contentionSort === "peak" ? "active" : ""}`}
                          onClick={() => setContentionSort("peak")}
                        >
                          Peak waiters
                        </button>
                      </th>
                      <th>
                        <button
                          type="button"
                          className={`resource-contention-sort ${contentionSort === "avg" ? "active" : ""}`}
                          onClick={() => setContentionSort("avg")}
                        >
                          Avg wait
                        </button>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {sortedContention.map((c, i) => (
                      <tr key={i} className="resource-graph-row">
                        <td className="resource-graph-resource">{c.resource_id}</td>
                        <td>
                          <div className="resource-contention-bar-wrap">
                            <div
                              className="resource-contention-bar"
                              style={{ width: `${(c.peak_waiters / maxPeak) * 100}%` }}
                            />
                            <span className="resource-contention-bar-label">{c.peak_waiters}</span>
                          </div>
                        </td>
                        <td className="resource-contention-avg">{formatDuration(c.avg_wait_ns)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
