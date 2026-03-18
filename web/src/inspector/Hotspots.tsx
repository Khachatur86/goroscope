import type { Goroutine } from "../api/client";

export type SpawnHotspot = {
  callSite: string;
  count: number;
  goroutineIds: number[];
};

export function computeSpawnHotspots(goroutines: Goroutine[]): SpawnHotspot[] {
  const byCallSite = new Map<string, number[]>();
  for (const g of goroutines) {
    const site = g.labels?.function ?? "(unknown)";
    const ids = byCallSite.get(site) ?? [];
    ids.push(g.goroutine_id);
    byCallSite.set(site, ids);
  }
  return Array.from(byCallSite.entries())
    .map(([callSite, goroutineIds]) => ({
      callSite,
      count: goroutineIds.length,
      goroutineIds,
    }))
    .sort((a, b) => b.count - a.count);
}

type Props = {
  hotspots: SpawnHotspot[];
  activeHotspotIds: number[] | null;
  onFilterByHotspot: (ids: number[]) => void;
  onClearHotspotFilter: () => void;
};

export function Hotspots({
  hotspots,
  activeHotspotIds,
  onFilterByHotspot,
  onClearHotspotFilter,
}: Props) {
  if (hotspots.length === 0) {
    return (
      <div className="inspector-section">
        <div className="inspector-label">Creation Hotspots</div>
        <p className="empty-message">No goroutines with spawn-site data.</p>
      </div>
    );
  }

  return (
    <div className="inspector-section">
      <div className="inspector-label inspector-label-row">
        <span>Creation Hotspots</span>
        {activeHotspotIds && activeHotspotIds.length > 0 && (
          <button
            type="button"
            className="inspector-clear-hotspot"
            onClick={onClearHotspotFilter}
          >
            Clear filter
          </button>
        )}
      </div>
      <p className="inspector-hint">Click a row to filter timeline to goroutines spawned from that call site.</p>
      <div className="hotspots-table">
        <table className="resource-graph-table-inner">
          <thead>
            <tr>
              <th>Call site</th>
              <th>Count</th>
            </tr>
          </thead>
          <tbody>
            {hotspots.map((h, i) => (
              <tr
                key={i}
                className={`hotspot-row ${activeHotspotIds && h.goroutineIds.length === activeHotspotIds.length && h.goroutineIds.every((id) => activeHotspotIds.includes(id)) ? "active" : ""}`}
                role="button"
                tabIndex={0}
                onClick={() => onFilterByHotspot(h.goroutineIds)}
                onKeyDown={(e) =>
                  (e.key === "Enter" || e.key === " ") && onFilterByHotspot(h.goroutineIds)
                }
              >
                <td className="hotspot-callsite">{h.callSite}</td>
                <td className="hotspot-count">{h.count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
