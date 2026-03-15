import { useState } from "react";

type ResourceEdge = {
  from_goroutine_id: number;
  to_goroutine_id: number;
  resource_id?: string;
};

type Props = {
  resources: ResourceEdge[];
  selectedId: number | null;
  onSelectGoroutine: (id: number) => void;
};

export function ResourceGraph({ resources, selectedId, onSelectGoroutine }: Props) {
  const [expanded, setExpanded] = useState(false);

  if (resources.length === 0) return null;

  return (
    <div className="resource-section">
      <button
        type="button"
        className="section-toggle"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        Resource Graph <span className="section-count">({resources.length})</span>
      </button>
      {expanded && (
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
      )}
    </div>
  );
}
