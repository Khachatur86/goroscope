import { useState, useEffect, useCallback } from "react";
import type { Insight, InsightSeverity } from "../api/client";
import { fetchSmartInsights } from "../api/client";

type Props = {
  /** Trigger a re-fetch whenever this value changes (e.g. engine data version). */
  refreshKey?: number | string;
  onSelectGoroutine?: (id: number) => void;
};

const SEVERITY_ICON: Record<InsightSeverity, string> = {
  critical: "🔴",
  warning:  "🟡",
  info:     "🔵",
};

const KIND_LABEL: Record<string, string> = {
  deadlock:       "Deadlock",
  leak:           "Goroutine leak",
  contention:     "Contention",
  blocking:       "Long blocking",
  goroutine_count: "High goroutine count",
};

function InsightCard({
  insight,
  onSelectGoroutine,
}: {
  insight: Insight;
  onSelectGoroutine?: (id: number) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const kindLabel = KIND_LABEL[insight.kind] ?? insight.kind;

  return (
    <div className={`insight-card insight-card--${insight.severity}`}>
      <button
        type="button"
        className="insight-card-header"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        <span className="insight-severity-icon" aria-label={insight.severity}>
          {SEVERITY_ICON[insight.severity]}
        </span>
        <span className="insight-kind-tag">{kindLabel}</span>
        <span className="insight-title">{insight.title}</span>
        <span className="insight-expand-icon">{expanded ? "▾" : "▸"}</span>
      </button>

      {expanded && (
        <div className="insight-card-body">
          <p className="insight-description">{insight.description}</p>
          <p className="insight-recommendation">
            <strong>Recommendation:</strong> {insight.recommendation}
          </p>
          {insight.goroutine_ids && insight.goroutine_ids.length > 0 && (
            <div className="insight-goroutines">
              <span className="insight-goroutines-label">Involved goroutines:</span>
              <div className="insight-goroutine-badges">
                {insight.goroutine_ids.slice(0, 15).map((id) => (
                  <button
                    key={id}
                    type="button"
                    className="groups-goroutine-id-badge"
                    onClick={() => onSelectGoroutine?.(id)}
                    title={`Inspect G${id}`}
                  >
                    G{id}
                  </button>
                ))}
                {insight.goroutine_ids.length > 15 && (
                  <span className="insight-goroutines-overflow">
                    +{insight.goroutine_ids.length - 15} more
                  </span>
                )}
              </div>
            </div>
          )}
          {insight.resource_ids && insight.resource_ids.length > 0 && (
            <div className="insight-resources">
              <span className="insight-goroutines-label">Resources:</span>
              {insight.resource_ids.map((r) => (
                <code key={r} className="insight-resource-tag">
                  {r}
                </code>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function SmartInsights({ refreshKey, onSelectGoroutine }: Props) {
  const [insights, setInsights] = useState<Insight[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await fetchSmartInsights();
      setInsights(data.insights ?? []);
      setTotal(data.total ?? 0);
      // Reset dismissed state if new critical insights appear.
      if ((data.insights ?? []).some((i) => i.severity === "critical")) {
        setDismissed(false);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  if (loading || total === 0 || dismissed) return null;

  const criticalCount = insights.filter((i) => i.severity === "critical").length;
  const warningCount = insights.filter((i) => i.severity === "warning").length;

  return (
    <section className="smart-insights-banner" aria-label="Smart Insights">
      <div className="smart-insights-header">
        <span className="smart-insights-title">
          {criticalCount > 0 && (
            <span className="smart-insights-badge badge-critical">{criticalCount} critical</span>
          )}
          {warningCount > 0 && (
            <span className="smart-insights-badge badge-warning">{warningCount} warning</span>
          )}
          <span className="smart-insights-label">Smart Insights</span>
        </span>
        <div className="smart-insights-actions">
          <button
            type="button"
            className="action-button secondary smart-insights-refresh"
            onClick={load}
            title="Refresh insights"
          >
            ↻
          </button>
          <button
            type="button"
            className="action-button secondary smart-insights-dismiss"
            onClick={() => setDismissed(true)}
            title="Dismiss"
          >
            ✕
          </button>
        </div>
      </div>
      <div className="smart-insights-list">
        {insights.map((insight) => (
          <InsightCard
            key={insight.id}
            insight={insight}
            onSelectGoroutine={onSelectGoroutine}
          />
        ))}
      </div>
    </section>
  );
}
