import { useState, useCallback, useMemo, useRef, useEffect } from "react";
import { FixedSizeList, type ListChildComponentProps } from "react-window";
import type { Goroutine, TimelineSegment, CompareResponse, GoroutineDelta } from "../api/client";
import { fetchCompare } from "../api/client";
import { Timeline } from "../timeline/Timeline";

type CompareData = CompareResponse;

/** Unified row: goroutine or null if absent in this capture. */
type UnifiedRow = { id: number; baseline: Goroutine | null; compare: Goroutine | null };

function buildUnifiedRows(baseline: Goroutine[], compare: Goroutine[]): UnifiedRow[] {
  const ids = new Set<number>();
  baseline.forEach((g) => ids.add(g.goroutine_id));
  compare.forEach((g) => ids.add(g.goroutine_id));
  const sorted = Array.from(ids).sort((a, b) => a - b);
  const baseMap = new Map(baseline.map((g) => [g.goroutine_id, g]));
  const compMap = new Map(compare.map((g) => [g.goroutine_id, g]));
  return sorted.map((id) => ({
    id,
    baseline: baseMap.get(id) ?? null,
    compare: compMap.get(id) ?? null,
  }));
}

type ComparePanelProps = {
  title: string;
  rows: UnifiedRow[];
  getGoroutine: (row: UnifiedRow) => Goroutine | null;
  goroutines: Goroutine[];
  segments: TimelineSegment[];
  selectedId: number | null;
  onSelect: (id: number) => void;
  onSelectSegment: (id: number, segment?: TimelineSegment) => void;
  diffByGid: Record<string, GoroutineDelta>;
  isBaseline: boolean;
  listRef?: React.RefObject<FixedSizeList>;
  onScroll?: (info: { scrollOffset: number; scrollUpdateWasRequested: boolean }) => void;
};

const ROW_HEIGHT = 44;
const LIST_HEIGHT = 280;

function GoroutineRow({
  index,
  style,
  data,
}: ListChildComponentProps<{
  rows: UnifiedRow[];
  getGoroutine: (row: UnifiedRow) => Goroutine | null;
  selectedId: number | null;
  onSelect: (id: number) => void;
  diffByGid: Record<string, GoroutineDelta>;
  isBaseline: boolean;
}>) {
  const row = data.rows[index];
  const g = data.getGoroutine(row);
  const delta = data.diffByGid[String(row.id)];
  const statusClass = delta
    ? `diff-${delta.status}`
    : g
      ? data.isBaseline
        ? "diff-only-baseline"
        : "diff-only-compare"
      : "diff-absent";

  return (
    <div style={style}>
      <button
        type="button"
        className={`lane-item compare-lane-item ${data.selectedId === row.id ? "active" : ""} ${statusClass}`}
        onClick={() => data.onSelect(row.id)}
      >
        {g ? (
          <>
            <span className={`state-pill ${g.state}`}>{g.state}</span>
            <span className="lane-item-title">G{g.goroutine_id}</span>
            {delta && (
              <span className={`diff-badge diff-badge--${delta.status}`} title={`Wait Δ: ${(delta.wait_delta_ns / 1e6).toFixed(2)}ms`}>
                {delta.status}
              </span>
            )}
            <span className="lane-item-meta">{g.labels?.function ?? g.reason ?? "—"}</span>
          </>
        ) : (
          <>
            <span className="state-pill diff-absent-pill">—</span>
            <span className="lane-item-title">G{row.id}</span>
            <span className="lane-item-meta">not in {data.isBaseline ? "baseline" : "compare"}</span>
          </>
        )}
      </button>
    </div>
  );
}

function ComparePanel({
  title,
  rows,
  getGoroutine,
  goroutines,
  segments,
  selectedId,
  onSelect,
  onSelectSegment,
  diffByGid,
  isBaseline,
  listRef,
  onScroll,
}: ComparePanelProps) {
  return (
    <div className="compare-panel">
      <h3 className="compare-panel-title">{title}</h3>
      <div className="compare-goroutine-list">
        {rows.length === 0 ? (
          <p className="empty-message">No goroutines</p>
        ) : (
          <FixedSizeList
            ref={listRef}
            height={LIST_HEIGHT}
            itemCount={rows.length}
            itemSize={ROW_HEIGHT}
            width="100%"
            onScroll={onScroll}
            itemData={{
              rows,
              getGoroutine,
              selectedId,
              onSelect,
              diffByGid,
              isBaseline,
            }}
          >
            {GoroutineRow}
          </FixedSizeList>
        )}
      </div>
      <div className="compare-timeline">
        <Timeline
          goroutines={goroutines}
          selectedId={selectedId}
          onSelectGoroutine={onSelectSegment}
          segmentsOverride={segments}
        />
      </div>
    </div>
  );
}

type CompareViewProps = {
  onClose: () => void;
};

export function CompareView({ onClose }: CompareViewProps) {
  const [fileA, setFileA] = useState<File | null>(null);
  const [fileB, setFileB] = useState<File | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [data, setData] = useState<CompareData | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [diffFilter, setDiffFilter] = useState<"all" | "improved" | "regressed" | "unchanged">("all");
  const baselineListRef = useRef<FixedSizeList>(null);
  const compareListRef = useRef<FixedSizeList>(null);

  // React Hooks must be called in a consistent order.
  // Previously this component returned early when `data === null`,
  // which caused a "Rendered more hooks than during the previous render" crash
  // once `data` was set.
  const safeData = (data ??
    ({
      baseline: { goroutines: [], timeline: [] },
      compare: { goroutines: [], timeline: [] },
      diff: { goroutine_deltas: {}, only_in_baseline: [], only_in_compare: [] },
    }) as CompareData);

  const handleBaselineScroll = useCallback(
    (info: { scrollOffset: number; scrollUpdateWasRequested?: boolean }) => {
      if (info.scrollUpdateWasRequested) return;
      compareListRef.current?.scrollTo(info.scrollOffset);
    },
    []
  );

  const handleCompareScroll = useCallback(
    (info: { scrollOffset: number; scrollUpdateWasRequested?: boolean }) => {
      if (info.scrollUpdateWasRequested) return;
      baselineListRef.current?.scrollTo(info.scrollOffset);
    },
    []
  );

  const handleFileA = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setFileA(f ?? null);
    setError(null);
    e.target.value = "";
  }, []);

  const handleFileB = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setFileB(f ?? null);
    setError(null);
    e.target.value = "";
  }, []);

  const handleCompare = useCallback(async () => {
    if (!fileA || !fileB) {
      setError("Select both files");
      return;
    }
    if (!fileA.name.endsWith(".gtrace") && !fileA.name.endsWith(".json")) {
      setError("File A must be .gtrace or .json");
      return;
    }
    if (!fileB.name.endsWith(".gtrace") && !fileB.name.endsWith(".json")) {
      setError("File B must be .gtrace or .json");
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const result = await fetchCompare(fileA, fileB);
      setData(result);
      const first = result.baseline.goroutines[0] ?? result.compare.goroutines[0];
      setSelectedId(first?.goroutine_id ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Compare failed");
    } finally {
      setLoading(false);
    }
  }, [fileA, fileB]);

  const handleReset = useCallback(() => {
    setData(null);
    setFileA(null);
    setFileB(null);
    setError(null);
    setSelectedId(null);
    setDiffFilter("all");
  }, []);

  const unifiedRows = useMemo(
    () => buildUnifiedRows(safeData.baseline.goroutines, safeData.compare.goroutines),
    [safeData.baseline.goroutines, safeData.compare.goroutines]
  );

  const filteredRows = useMemo(() => {
    if (diffFilter === "all") return unifiedRows;
    return unifiedRows.filter((row) => {
      const delta = safeData.diff.goroutine_deltas[String(row.id)];
      if (!delta) return false;
      return delta.status === diffFilter;
    });
  }, [unifiedRows, diffFilter, safeData.diff.goroutine_deltas]);

  const filteredBaselineGoroutines = useMemo(
    () =>
      safeData.baseline.goroutines.filter((g) =>
        filteredRows.some((r) => r.baseline?.goroutine_id === g.goroutine_id)
      ),
    [safeData.baseline.goroutines, filteredRows]
  );
  const filteredCompareGoroutines = useMemo(
    () =>
      safeData.compare.goroutines.filter((g) =>
        filteredRows.some((r) => r.compare?.goroutine_id === g.goroutine_id)
      ),
    [safeData.compare.goroutines, filteredRows]
  );

  useEffect(() => {
    const visible = filteredRows.some((r) => r.id === selectedId);
    if (!visible && filteredRows.length > 0) {
      setSelectedId(filteredRows[0].id);
    }
  }, [filteredRows, selectedId]);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (filteredRows.length === 0) return;
      const idx = selectedId ? filteredRows.findIndex((r) => r.id === selectedId) : -1;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        const next = idx < 0 ? 0 : Math.min(filteredRows.length - 1, idx + 1);
        setSelectedId(filteredRows[next].id);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        const next = idx <= 0 ? filteredRows.length - 1 : idx - 1;
        setSelectedId(filteredRows[next].id);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [filteredRows, selectedId]);

  useEffect(() => {
    if (!selectedId || filteredRows.length === 0) return;
    const idx = filteredRows.findIndex((r) => r.id === selectedId);
    if (idx < 0) return;
    baselineListRef.current?.scrollToItem(idx, "smart");
    compareListRef.current?.scrollToItem(idx, "smart");
  }, [selectedId, filteredRows]);

  const diffSummary = useMemo(() => {
    const deltas = Object.values(safeData.diff.goroutine_deltas);
    return {
      improved: deltas.filter((d) => d.status === "improved").length,
      regressed: deltas.filter((d) => d.status === "regressed").length,
      unchanged: deltas.filter((d) => d.status === "unchanged").length,
      onlyBaseline: safeData.diff.only_in_baseline?.length ?? 0,
      onlyCompare: safeData.diff.only_in_compare?.length ?? 0,
    };
  }, [safeData.diff.goroutine_deltas, safeData.diff.only_in_baseline, safeData.diff.only_in_compare]);

  if (!data) {
    return (
      <div className="compare-modal">
        <div className="compare-modal-content">
          <h2>Compare captures</h2>
          <p className="compare-modal-desc">Select two .gtrace files to compare baseline vs compare.</p>
          <div className="compare-file-inputs">
            <div className="compare-file-group">
              <label htmlFor="compare-file-a">Baseline (before)</label>
              <input
                id="compare-file-a"
                type="file"
                accept=".gtrace,.json"
                onChange={handleFileA}
              />
              <span className="compare-file-name">{fileA?.name ?? "—"}</span>
            </div>
            <div className="compare-file-group">
              <label htmlFor="compare-file-b">Compare (after)</label>
              <input
                id="compare-file-b"
                type="file"
                accept=".gtrace,.json"
                onChange={handleFileB}
              />
              <span className="compare-file-name">{fileB?.name ?? "—"}</span>
            </div>
          </div>
          {error && (
            <p className="compare-error" role="alert">
              {error}
            </p>
          )}
          <div className="compare-modal-actions">
            <button type="button" className="action-button secondary" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="action-button"
              onClick={handleCompare}
              disabled={loading || !fileA || !fileB}
            >
              {loading ? "Comparing…" : "Compare"}
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="compare-view">
      <div className="compare-view-header">
        <h2>Compare: baseline vs compare</h2>
        <div className="compare-summary">
          <button
            type="button"
            className={`compare-summary-item compare-summary-item--btn ${diffFilter === "all" ? "active" : ""}`}
            onClick={() => setDiffFilter("all")}
            title="Show all"
          >
            All
          </button>
          <button
            type="button"
            className={`compare-summary-item improved compare-summary-item--btn ${diffFilter === "improved" ? "active" : ""}`}
            onClick={() => setDiffFilter("improved")}
            title="Show only improved"
          >
            {diffSummary.improved} improved
          </button>
          <button
            type="button"
            className={`compare-summary-item regressed compare-summary-item--btn ${diffFilter === "regressed" ? "active" : ""}`}
            onClick={() => setDiffFilter("regressed")}
            title="Show only regressed"
          >
            {diffSummary.regressed} regressed
          </button>
          <button
            type="button"
            className={`compare-summary-item unchanged compare-summary-item--btn ${diffFilter === "unchanged" ? "active" : ""}`}
            onClick={() => setDiffFilter("unchanged")}
            title="Show only unchanged"
          >
            {diffSummary.unchanged} unchanged
          </button>
          {diffSummary.onlyBaseline > 0 && (
            <span className="compare-summary-item only-baseline" title="Goroutines only in baseline">
              {diffSummary.onlyBaseline} only baseline
            </span>
          )}
          {diffSummary.onlyCompare > 0 && (
            <span className="compare-summary-item only-compare" title="Goroutines only in compare">
              {diffSummary.onlyCompare} only compare
            </span>
          )}
        </div>
        <div className="compare-view-actions">
          <span className="compare-legend">
            <span className="diff-badge diff-badge--improved">improved</span>
            <span className="diff-badge diff-badge--regressed">regressed</span>
            <span className="diff-badge diff-badge--unchanged">unchanged</span>
          </span>
          <button type="button" className="action-button secondary" onClick={handleReset}>
            New compare
          </button>
          <button type="button" className="action-button secondary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
      <div className="compare-split">
        <ComparePanel
          title="Baseline"
          rows={filteredRows}
          getGoroutine={(r) => r.baseline}
          goroutines={filteredBaselineGoroutines}
          segments={safeData.baseline.timeline}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onSelectSegment={(id) => setSelectedId(id)}
          diffByGid={data.diff.goroutine_deltas}
          isBaseline
          listRef={baselineListRef}
          onScroll={handleBaselineScroll}
        />
        <ComparePanel
          title="Compare"
          rows={filteredRows}
          getGoroutine={(r) => r.compare}
          goroutines={filteredCompareGoroutines}
          segments={safeData.compare.timeline}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onSelectSegment={(id) => setSelectedId(id)}
          diffByGid={data.diff.goroutine_deltas}
          isBaseline={false}
          listRef={compareListRef}
          onScroll={handleCompareScroll}
        />
      </div>
    </div>
  );
}
