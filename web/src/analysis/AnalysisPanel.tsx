import { lazy, Suspense } from "react";
import type { Goroutine, TimelineSegment } from "../api/client";
import type { FiltersState } from "../filters/url";
import { SmartInsights } from "../insights/SmartInsights";
import { Hotspots } from "../inspector/Hotspots";
import { ResourceGraph } from "../resource-graph/ResourceGraph";
import { DeadlockHints } from "../inspector/DeadlockHints";
import { GoroutineGroups } from "../groups/GoroutineGroups";
import { ContentionHeatmap } from "./ContentionHeatmap";
import { RequestsView } from "../requests/RequestsView";
import { CodeAnalysis } from "./CodeAnalysis";
import type { DeadlockHint } from "../api/client";

const DependencyGraph = lazy(() =>
  import("../graph/DependencyGraph").then((m) => ({ default: m.DependencyGraph }))
);

export type AnalysisTabId =
  | "insights"
  | "hotspots"
  | "resources"
  | "deadlock"
  | "groups"
  | "graph"
  | "heatmap"
  | "requests"
  | "code";

const TABS: { id: AnalysisTabId; label: string }[] = [
  { id: "insights",  label: "Insights"  },
  { id: "hotspots",  label: "Hotspots"  },
  { id: "resources", label: "Resources" },
  { id: "deadlock",  label: "Deadlock"  },
  { id: "groups",    label: "Groups"    },
  { id: "graph",     label: "Graph"     },
  { id: "heatmap",   label: "Heatmap"   },
  { id: "requests",  label: "Requests"  },
  { id: "code",      label: "Code"      },
];

interface AnalysisPanelProps {
  tab: AnalysisTabId;
  open: boolean;
  onTabChange: (id: AnalysisTabId) => void;
  onToggleOpen: () => void;
  dataRevision: number;
  goroutines: Goroutine[];
  selectedId: number | null;
  resources: { from_goroutine_id: number; to_goroutine_id: number; resource_id?: string }[];
  contention: { resource_id: string; peak_waiters: number; segment_count: number; total_wait_ns: number; avg_wait_ns: number }[];
  deadlockHints: DeadlockHint[];
  timelineSegments: TimelineSegment[];
  hotspots: ReturnType<typeof import("../inspector/Hotspots").computeSpawnHotspots>;
  filters: FiltersState;
  onSelectGoroutine: (id: number) => void;
  onFilterByHotspot: (ids: number[]) => void;
  onClearHotspotFilter: () => void;
  onSetFilters: (updater: (f: FiltersState) => FiltersState) => void;
  onSetScrubTime: (ns: number) => void;
  onHighlightRequest: (ids: Set<number>) => void;
}

export function AnalysisPanel({
  tab,
  open,
  onTabChange,
  onToggleOpen,
  dataRevision,
  goroutines,
  selectedId,
  resources,
  contention,
  deadlockHints,
  timelineSegments,
  hotspots,
  filters,
  onSelectGoroutine,
  onFilterByHotspot,
  onClearHotspotFilter,
  onSetFilters,
  onSetScrubTime,
  onHighlightRequest,
}: AnalysisPanelProps) {
  return (
    <section className="analysis-panel">
      <div className="analysis-panel-header">
        <div className="analysis-tabs">
          {TABS.map(({ id, label }) => (
            <button
              key={id}
              type="button"
              className={`analysis-tab ${tab === id ? "active" : ""}`}
              onClick={() => {
                if (tab === id && open) {
                  onToggleOpen();
                } else {
                  onTabChange(id);
                  if (!open) onToggleOpen();
                }
              }}
            >
              {label}
            </button>
          ))}
        </div>
        <button
          type="button"
          className="analysis-collapse-btn"
          onClick={onToggleOpen}
          title={open ? "Collapse analysis panel" : "Expand analysis panel"}
          aria-expanded={open}
        >
          {open ? "▾" : "▴"}
        </button>
      </div>

      {open && (
        <div className="analysis-panel-body">
          {tab === "insights" && (
            <SmartInsights refreshKey={dataRevision} onSelectGoroutine={onSelectGoroutine} />
          )}
          {tab === "hotspots" && (
            <Hotspots
              hotspots={hotspots}
              activeHotspotIds={filters.hotspotIds ?? null}
              onFilterByHotspot={onFilterByHotspot}
              onClearHotspotFilter={onClearHotspotFilter}
            />
          )}
          {tab === "resources" && (
            <ResourceGraph
              resources={resources}
              contention={contention}
              selectedId={selectedId}
              onSelectGoroutine={onSelectGoroutine}
            />
          )}
          {tab === "deadlock" && (
            <DeadlockHints hints={deadlockHints} onSelectGoroutine={onSelectGoroutine} />
          )}
          {tab === "groups" && (
            <GoroutineGroups onSelectGoroutine={onSelectGoroutine} />
          )}
          {tab === "graph" && (
            <Suspense fallback={null}>
              <DependencyGraph
                goroutines={goroutines}
                selectedId={selectedId}
                onSelectGoroutine={onSelectGoroutine}
              />
            </Suspense>
          )}
          {tab === "heatmap" && (
            <ContentionHeatmap
              segments={timelineSegments}
              onSelectResource={(id, bucketMidNS) => {
                onSetFilters((f) => ({ ...f, search: id }));
                onSetScrubTime(bucketMidNS);
              }}
            />
          )}
          {tab === "requests" && (
            <RequestsView onSelectRequest={onHighlightRequest} />
          )}
          {tab === "code" && (
            <CodeAnalysis />
          )}
        </div>
      )}
    </section>
  );
}
