import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import { FixedSizeList, type ListChildComponentProps } from "react-window";
import type { Goroutine, Session, DeadlockHint, TimelineSegment } from "./api/client";
import type { ScrubSnapshot, TimelineHandle } from "./timeline/Timeline";
import {
  fetchCurrentSession,
  fetchGoroutines,
  fetchGoroutine,
  fetchTimeline,
  fetchResourceGraph,
  fetchResourceContention,
  fetchInsights,
  fetchDeadlockHints,
  uploadReplayCapture,
} from "./api/client";
import { Filters } from "./filters/Filters";
import { Inspector } from "./inspector/Inspector";
import { Hotspots, computeSpawnHotspots } from "./inspector/Hotspots";
import { DeadlockHints } from "./inspector/DeadlockHints";
import { Timeline } from "./timeline/Timeline";
import { CompareView } from "./compare/CompareView";
import { ResourceGraph } from "./resource-graph/ResourceGraph";
import { GoroutineGroups } from "./groups/GoroutineGroups";
import { SmartInsights } from "./insights/SmartInsights";
import { DependencyGraph } from "./graph/DependencyGraph";
import { distinctLabelPairs, filterAndSortGoroutines } from "./utils/goroutines";

/** Height of one row in the virtualised goroutine list (px). */
const GOROUTINE_ITEM_HEIGHT = 44;
/** Visible height of the virtualised goroutine list (px). */
const GOROUTINE_LIST_HEIGHT = 400;

const LIFETIME_COLORS: Record<string, string> = {
  RUNNING: "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING: "#f59e0b",
  BLOCKED: "#f43f5e",
  SYSCALL: "#4da6ff",
  DONE: "#4b5563",
};

/** Thin colour strip at the bottom of a goroutine list row showing its full lifecycle. */
function LifetimeBar({ segments }: { segments: TimelineSegment[] | undefined }) {
  if (!segments || segments.length === 0) {
    return <div className="lifetime-bar lifetime-bar--empty" />;
  }
  const minStart = Math.min(...segments.map((s) => s.start_ns));
  const maxEnd = Math.max(...segments.map((s) => s.end_ns));
  const span = Math.max(maxEnd - minStart, 1);
  const sorted = [...segments].sort((a, b) => a.start_ns - b.start_ns);

  // Build gradient stops, inserting gap colour for uncovered intervals.
  const stops: string[] = [];
  let cursor = 0;
  for (const seg of sorted) {
    const x1 = ((seg.start_ns - minStart) / span) * 100;
    const x2 = ((seg.end_ns - minStart) / span) * 100;
    if (x1 > cursor + 0.01) {
      // Brief gap between segments (scheduling latency, filter holes, etc.)
      stops.push(`#1e293b ${cursor.toFixed(2)}%`, `#1e293b ${x1.toFixed(2)}%`);
    }
    const color = LIFETIME_COLORS[seg.state] ?? "#94a3b8";
    stops.push(`${color} ${x1.toFixed(2)}%`, `${color} ${x2.toFixed(2)}%`);
    cursor = x2;
  }
  if (cursor < 99.99) {
    stops.push(`#1e293b ${cursor.toFixed(2)}%`, `#1e293b 100%`);
  }

  return (
    <div
      className="lifetime-bar"
      style={{ background: `linear-gradient(to right, ${stops.join(", ")})` }}
      title={`${segments.length} segments`}
    />
  );
}

type GoroutineRowData = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelect: (id: number) => void;
  segmentsByGoroutine: Map<number, TimelineSegment[]>;
};

function GoroutineRow({ index, style, data }: ListChildComponentProps<GoroutineRowData>) {
  const g = data.goroutines[index];
  return (
    <div style={style}>
      <button
        type="button"
        className={`lane-item ${data.selectedId === g.goroutine_id ? "active" : ""}`}
        onClick={() => data.onSelect(g.goroutine_id)}
      >
        <span className={`state-pill ${g.state}`}>{g.state}</span>
        <span className="lane-item-title">G{g.goroutine_id}</span>
        <span className="lane-item-meta">
          {g.labels?.function ?? g.reason ?? "—"}
        </span>
        <LifetimeBar segments={data.segmentsByGoroutine.get(g.goroutine_id)} />
      </button>
    </div>
  );
}

type FiltersState = {
  state: string;
  reason: string;
  resource: string;
  search: string;
  minWaitNs: string;
  sortMode: string;
  showLeakOnly?: boolean;
  hideRuntime?: boolean;
  hotspotIds?: number[] | null;
  labelFilter?: string;
};

function buildShareableURL(filters: FiltersState, selectedId: number | null): string {
  const params = new URLSearchParams();
  if (selectedId) params.set("goroutine", String(selectedId));
  if (filters.state && filters.state !== "ALL") params.set("state", filters.state);
  if (filters.reason) params.set("reason", filters.reason);
  if (filters.resource) params.set("resource", filters.resource);
  if (filters.search) params.set("search", filters.search);
  if (filters.labelFilter) params.set("label", filters.labelFilter);
  if (filters.showLeakOnly) params.set("leak", "1");
  if (filters.hideRuntime) params.set("hide_runtime", "1");
  const qs = params.toString();
  return qs ? `${window.location.origin}${window.location.pathname}?${qs}` : window.location.origin + window.location.pathname;
}

function parseFiltersFromURL(): Partial<FiltersState> {
  const params = new URLSearchParams(window.location.search);
  const out: Partial<FiltersState> = {};
  const state = params.get("state");
  if (state) out.state = state;
  const reason = params.get("reason");
  if (reason) out.reason = reason;
  const resource = params.get("resource");
  if (resource) out.resource = resource;
  const search = params.get("search");
  if (search) out.search = search;
  const label = params.get("label");
  if (label) out.labelFilter = label;
  if (params.get("leak") === "1") out.showLeakOnly = true;
  if (params.get("hide_runtime") === "1") out.hideRuntime = true;
  return out;
}

function parseGoroutineFromURL(): number | null {
  const params = new URLSearchParams(window.location.search);
  const id = params.get("goroutine");
  if (!id) return null;
  const n = parseInt(id, 10);
  return Number.isFinite(n) && n > 0 ? n : null;
}

export function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [goroutines, setGoroutines] = useState<Goroutine[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [selectedGoroutine, setSelectedGoroutine] = useState<Goroutine | null>(null);
  const [selectedSegment, setSelectedSegment] = useState<TimelineSegment | null>(null);
  const [resources, setResources] = useState<{ from_goroutine_id: number; to_goroutine_id: number; resource_id?: string }[]>([]);
  const [contention, setContention] = useState<{ resource_id: string; peak_waiters: number; segment_count: number; total_wait_ns: number; avg_wait_ns: number }[]>([]);
  const [insights, setInsights] = useState<{
    long_blocked_count: number;
    leak_candidates_count?: number;
  }>({ long_blocked_count: 0, leak_candidates_count: 0 });
  const [deadlockHints, setDeadlockHints] = useState<DeadlockHint[]>([]);
  const [relatedFocus, setRelatedFocus] = useState(false);
  const [zoomToSelected, setZoomToSelected] = useState(false);
  const [viewMode, setViewMode] = useState<"lanes" | "heatmap">("lanes");
  const [analysisTab, setAnalysisTab] = useState<"insights" | "hotspots" | "resources" | "deadlock" | "groups" | "graph">("insights");
  const [analysisOpen, setAnalysisOpen] = useState(true);
  const [brushFilterIds, setBrushFilterIds] = useState<Set<number> | null>(null);
  const [filters, setFilters] = useState<FiltersState>(() => {
    const fromUrl = parseFiltersFromURL();
    return {
      state: fromUrl.state ?? "ALL",
      reason: fromUrl.reason ?? "",
      resource: fromUrl.resource ?? "",
      search: fromUrl.search ?? "",
      minWaitNs: "",
      sortMode: "SUSPICIOUS",
      showLeakOnly: fromUrl.showLeakOnly ?? false,
      hideRuntime: fromUrl.hideRuntime ?? false,
      hotspotIds: null,
      labelFilter: fromUrl.labelFilter ?? "",
    };
  });

  // Time scrubber: declared early because scrubMap/listGoroutines useMemos reference them.
  const [scrubTimeNS, setScrubTimeNS] = useState<number | null>(null);
  const [scrubSnapshot, setScrubSnapshot] = useState<ScrubSnapshot[]>([]);

  // Segments from the Timeline component — used to draw per-row lifetime bars.
  const [timelineSegments, setTimelineSegments] = useState<TimelineSegment[]>([]);
  const segmentsByGoroutine = useMemo(() => {
    const map = new Map<number, TimelineSegment[]>();
    for (const seg of timelineSegments) {
      const list = map.get(seg.goroutine_id);
      if (list) list.push(seg);
      else map.set(seg.goroutine_id, [seg]);
    }
    return map;
  }, [timelineSegments]);

  // Memoised so that unrelated state changes (selectedId, inspectorTab, …)
  // do not trigger a full 200-goroutine re-sort on every render.
  const filteredGoroutines = useMemo(
    () => filterAndSortGoroutines(goroutines, filters),
    [goroutines, filters]
  );
  const hotspots = useMemo(() => computeSpawnHotspots(goroutines), [goroutines]);
  let displayGoroutines =
    selectedId && !filteredGoroutines.some((g) => g.goroutine_id === selectedId)
      ? (() => {
          const sel = goroutines.find((g) => g.goroutine_id === selectedId);
          return sel ? [sel, ...filteredGoroutines] : filteredGoroutines;
        })()
      : filteredGoroutines;

  if (relatedFocus && selectedId) {
    const relatedIds = new Set<number>([selectedId]);
    const selected = goroutines.find((g) => g.goroutine_id === selectedId);
    if (selected?.parent_id) relatedIds.add(selected.parent_id);
    goroutines.forEach((g) => {
      if (g.parent_id === selectedId) relatedIds.add(g.goroutine_id);
    });
    resources.forEach((e) => {
      if (e.from_goroutine_id === selectedId) relatedIds.add(e.to_goroutine_id);
      if (e.to_goroutine_id === selectedId) relatedIds.add(e.from_goroutine_id);
    });
    displayGoroutines = displayGoroutines.filter((g) => relatedIds.has(g.goroutine_id));
  }

  if (brushFilterIds !== null) {
    // Always keep the selected goroutine visible even if it has no segments in range
    displayGoroutines = displayGoroutines.filter(
      (g) => brushFilterIds.has(g.goroutine_id) || g.goroutine_id === selectedId
    );
  }

  // When the time scrubber is active, merge historical states from the snapshot
  // into the display list so the goroutine list reflects what each goroutine was
  // doing at the scrubbed moment rather than its current live state.
  const scrubMap = useMemo(
    () => new Map(scrubSnapshot.map((s) => [s.goroutine_id, s])),
    [scrubSnapshot]
  );

  // Synthetic TimelineSegment passed to Inspector when scrubbing.
  // start_ns = scrubTimeNS so fetchStackAt fetches the closest stack ≤ T.
  const scrubSegmentOverride = useMemo<TimelineSegment | null>(() => {
    if (scrubTimeNS == null || selectedId == null) return null;
    const snap = scrubMap.get(selectedId);
    return {
      goroutine_id: selectedId,
      start_ns: scrubTimeNS,
      end_ns: scrubTimeNS + 1,
      state: snap?.state ?? "",
      reason: snap?.reason ?? "",
      resource_id: "",
    };
  }, [scrubTimeNS, selectedId, scrubMap]);

  const listGoroutines = useMemo(
    () =>
      scrubTimeNS == null
        ? displayGoroutines
        : displayGoroutines.map((g) => {
            const snap = scrubMap.get(g.goroutine_id);
            if (!snap) return g;
            return { ...g, state: snap.state, reason: snap.reason ?? g.reason };
          }),
    [scrubTimeNS, displayGoroutines, scrubMap]
  );

  const initialUrlId = useRef(parseGoroutineFromURL());
  useEffect(() => {
    if (displayGoroutines.length === 0) {
      if (!initialUrlId.current) setSelectedId(null);
      return;
    }
    initialUrlId.current = null;
    const stillVisible = selectedId && displayGoroutines.some((g) => g.goroutine_id === selectedId);
    if (stillVisible) return;
    const preferred =
      filters.sortMode === "ID" || filters.sortMode === "WAIT_TIME"
        ? displayGoroutines.find((g) => g.state === "BLOCKED" || g.state === "WAITING") ?? displayGoroutines[0]
        : displayGoroutines[0];
    setSelectedId(preferred.goroutine_id);
  }, [displayGoroutines, filters.sortMode, selectedId]);

  const hasGoroutineInURL = parseGoroutineFromURL() !== null;

  // goroutineParams is shared between the full load and the live-refresh path.
  const goroutineParams = useMemo(
    () =>
      hasGoroutineInURL
        ? undefined
        : {
            state: filters.state !== "ALL" ? filters.state : undefined,
            reason: filters.reason || undefined,
            search: filters.search || undefined,
            min_wait_ns: filters.minWaitNs || undefined,
            label: filters.labelFilter || undefined,
          },
    [hasGoroutineInURL, filters.state, filters.reason, filters.search, filters.minWaitNs, filters.labelFilter]
  );

  // loadData fetches all endpoints (session, goroutines, resources, insights,
  // deadlock hints).  Used for initial load, manual Refresh, and filter changes.
  const loadData = useCallback(async () => {
    const [sess, gs, res, contentionData, ins, deadlock] = await Promise.all([
      fetchCurrentSession(),
      fetchGoroutines(goroutineParams),
      fetchResourceGraph(),
      fetchResourceContention(),
      fetchInsights(
        filters.minWaitNs || undefined,
        "30000000000"
      ),
      fetchDeadlockHints(),
    ]);
    setSession(sess ?? null);
    const gsSafe = Array.isArray(gs) ? gs : [];
    setGoroutines(gsSafe);
    const urlId = parseGoroutineFromURL();
    if (urlId && gsSafe.some((g) => g.goroutine_id === urlId)) {
      setSelectedId(urlId);
    }
    setResources(Array.isArray(res) ? res : []);
    setContention(Array.isArray(contentionData) ? contentionData : []);
    setInsights(ins ?? { long_blocked_count: 0, leak_candidates_count: 0 });
    setDeadlockHints(deadlock?.hints ?? []);
    setDataRevision((v) => v + 1);
  }, [goroutineParams, filters.minWaitNs]);

  // refreshLive fetches only goroutines — the hot path called on every SSE
  // "update" event.  Skipping the 5 slower endpoints (resources, contention,
  // insights, deadlock) reduces React renders per second and eliminates the
  // DOM jitter visible with 200+ live goroutines.  The slower endpoints are
  // kept fresh by the periodic full reload in loadData (every 5 s).
  const refreshLive = useCallback(async () => {
    const gs = await fetchGoroutines(goroutineParams).catch(() => null);
    if (!gs) return;
    const gsSafe = Array.isArray(gs) ? gs : [];
    setGoroutines(gsSafe);
    const urlId = parseGoroutineFromURL();
    if (urlId && gsSafe.some((g) => g.goroutine_id === urlId)) {
      setSelectedId(urlId);
    }
    setDataRevision((v) => v + 1);
  }, [goroutineParams]);

  useEffect(() => {
    // Initial full load on mount.
    loadData();

    // Periodic full refresh every 5 s keeps slow data (resources, insights,
    // deadlock hints) reasonably fresh without hammering the API on every
    // goroutine state change.
    const fullRefreshTimer = setInterval(loadData, 5000);

    let source: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let alive = true;

    const connect = () => {
      if (!alive) return;
      setStreamStatus("connecting");
      source = new EventSource("/api/v1/stream");

      source.addEventListener("connected", () => {
        setStreamStatus("live");
      });

      // SSE "update" uses the lightweight goroutines-only refresh so that
      // fast-changing live data (state, reason) does not pull 5 extra
      // endpoints on every 200 ms tick.
      source.addEventListener("update", () => {
        refreshLive();
      });

      source.onerror = () => {
        setStreamStatus("disconnected");
        source?.close();
        source = null;
        if (alive) {
          reconnectTimer = setTimeout(connect, 3000);
        }
      };
    };

    connect();

    return () => {
      alive = false;
      clearInterval(fullRefreshTimer);
      source?.close();
      if (reconnectTimer !== null) clearTimeout(reconnectTimer);
    };
  }, [loadData, refreshLive]);

  const initialGoroutineFromUrl = useRef<number | null>(parseGoroutineFromURL());
  useEffect(() => {
    const fromUrl = initialGoroutineFromUrl.current;
    if (!fromUrl) return;
    if (filteredGoroutines.some((g) => g.goroutine_id === fromUrl)) {
      setSelectedId(fromUrl);
      initialGoroutineFromUrl.current = null;
      return;
    }
    if (goroutines.some((g) => g.goroutine_id === fromUrl)) {
      setSelectedId(fromUrl);
      initialGoroutineFromUrl.current = null;
      return;
    }
    initialGoroutineFromUrl.current = null;
    fetchGoroutine(fromUrl).then((g) => {
      if (g) {
        setGoroutines((prev) =>
          prev.some((x) => x.goroutine_id === fromUrl) ? prev : [...prev, g]
        );
        setSelectedId(fromUrl);
      }
    });
  }, [filteredGoroutines, goroutines]);

  useEffect(() => {
    if (selectedId) {
      fetchGoroutine(selectedId).then(setSelectedGoroutine);
    } else {
      setSelectedGoroutine(null);
    }
  }, [selectedId]);

  useEffect(() => {
    if (!selectedId) return;
    const params = new URLSearchParams(window.location.search);
    params.set("goroutine", String(selectedId));
    if (filters.state !== "ALL") params.set("state", filters.state);
    if (filters.reason) params.set("reason", filters.reason);
    if (filters.resource) params.set("resource", filters.resource);
    if (filters.search) params.set("search", filters.search);
    if (filters.showLeakOnly) params.set("leak", "1");
    if (filters.hideRuntime) params.set("hide_runtime", "1");
    const qs = params.toString();
    const url = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
    window.history.replaceState(null, "", url);
  }, [selectedId, filters.state, filters.reason, filters.resource, filters.search, filters.showLeakOnly, filters.hideRuntime]);

  const handleSelect = (id: number) => {
    setSelectedId(id);
    setSelectedSegment(null);
    setHighlightedIds(null);
  };

  const handleSelectFromTimeline = (id: number, segment?: TimelineSegment) => {
    setSelectedId(id);
    setSelectedSegment(segment ?? null);
  };

  const handleJumpTo = (id: number) => {
    if (!goroutines.some((g) => g.goroutine_id === id)) {
      fetchGoroutine(id).then((g) => {
        if (g) {
          setGoroutines((prev) => (prev.some((x) => x.goroutine_id === id) ? prev : [...prev, g]));
          setSelectedId(id);
        }
      });
    } else {
      setSelectedId(id);
    }
  };


  const handleCopyLink = () => {
    const url = buildShareableURL(filters, selectedId);
    navigator.clipboard.writeText(url).then(() => {
      const btn = document.getElementById("copy-link-btn");
      if (btn) {
        const prev = btn.textContent;
        btn.textContent = "Copied!";
        setTimeout(() => { btn!.textContent = prev; }, 1500);
      }
    });
  };

  const handleLongBlockedClick = () => {
    setFilters((f) => ({ ...f, minWaitNs: "1000000000" }));
  };

  const handleLeakClick = () => {
    setFilters((f) => ({ ...f, showLeakOnly: true }));
  };

  const processReplayFile = useCallback(
    async (file: File) => {
      if (!file.name.endsWith(".gtrace") && !file.name.endsWith(".json")) {
        setReplayError("Please select a .gtrace or .json capture file.");
        return;
      }
      setReplayError(null);
      setReplayUploading(true);
      try {
        await uploadReplayCapture(file);
        await loadData();
      } catch (err) {
        setReplayError(err instanceof Error ? err.message : "Upload failed");
      } finally {
        setReplayUploading(false);
      }
    },
    [loadData]
  );

  const handleOpenCapture = () => captureInputRef.current?.click();

  const handleCaptureFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) processReplayFile(file);
    e.target.value = "";
  };

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    const file = e.dataTransfer.files?.[0];
    if (file) processReplayFile(file);
  };

  const jumpToInputRef = useRef<HTMLInputElement>(null);
  const timelinePanelRef = useRef<HTMLElement>(null);
  const timelineRef = useRef<TimelineHandle>(null);
  const captureInputRef = useRef<HTMLInputElement>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [streamStatus, setStreamStatus] = useState<"connecting" | "live" | "disconnected">("connecting");
  const [dataRevision, setDataRevision] = useState(0);
  const [replayUploading, setReplayUploading] = useState(false);
  const [replayError, setReplayError] = useState<string | null>(null);
  const [compareOpen, setCompareOpen] = useState(false);
  const [highlightedIds, setHighlightedIds] = useState<Set<number> | null>(null);

  // Direct canvas composite export — no html2canvas needed.
  const handleSavePng = useCallback(() => {
    timelineRef.current?.exportPng();
  }, []);

  const handleFullscreen = () => {
    const el = timelinePanelRef.current;
    if (!el) return;
    if (!document.fullscreenElement) {
      el.requestFullscreen().then(() => setIsFullscreen(true)).catch(() => {});
    } else {
      document.exitFullscreen().then(() => setIsFullscreen(false)).catch(() => {});
    }
  };

  useEffect(() => {
    const onFullscreenChange = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", onFullscreenChange);
  }, []);

  useEffect(() => {
    if (!compareOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setCompareOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [compareOpen]);

  const handleExportJson = async () => {
    const segs = await fetchTimeline({
      state: filters.state !== "ALL" ? filters.state : undefined,
      reason: filters.reason || undefined,
      search: filters.search || undefined,
      label: filters.labelFilter || undefined,
    }).catch(() => []);
    const filteredSegs = (segs ?? []).filter((s) =>
      filteredGoroutines.some((g) => g.goroutine_id === s.goroutine_id)
    );
    const payload = {
      goroutines: filteredGoroutines.length,
      segments: filteredSegs.length,
      timeline: filteredSegs,
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], {
      type: "application/json",
    });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `goroscope-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  const handleExportChromeTrace = async () => {
    const segs = await fetchTimeline({
      state: filters.state !== "ALL" ? filters.state : undefined,
      reason: filters.reason || undefined,
      search: filters.search || undefined,
      label: filters.labelFilter || undefined,
    }).catch(() => []);
    const filteredSegs = (segs ?? []).filter((s) =>
      filteredGoroutines.some((g) => g.goroutine_id === s.goroutine_id)
    );
    const minNs = filteredSegs.length > 0 ? Math.min(...filteredSegs.map((s) => s.start_ns)) : 0;
    const events = filteredSegs.map((s) => ({
      name: s.state,
      cat: "goroutine",
      ph: "X" as const,
      ts: (s.start_ns - minNs) / 1000,
      dur: (s.end_ns - s.start_ns) / 1000,
      pid: 0,
      tid: s.goroutine_id,
      args: {
        goroutine_id: s.goroutine_id,
        ...(s.reason && { reason: s.reason }),
        ...(s.resource_id && { resource_id: s.resource_id }),
      },
    }));
    const payload = { traceEvents: events };
    const blob = new Blob([JSON.stringify(payload)], {
      type: "application/json",
    });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `goroscope-trace-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (compareOpen) return;
      const active = document.activeElement;
      const isInput = active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement;
      if ((e.ctrlKey || e.metaKey) && e.key === "g") {
        e.preventDefault();
        jumpToInputRef.current?.focus();
        return;
      }
      // ESC clears the scrub cursor when active.
      if (e.key === "Escape" && scrubTimeNS != null) {
        e.preventDefault();
        setScrubTimeNS(null);
        return;
      }
      if (isInput) return;
      if (displayGoroutines.length === 0) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        const idx = selectedId
          ? displayGoroutines.findIndex((g) => g.goroutine_id === selectedId)
          : -1;
        const next = idx < 0 ? 0 : Math.min(displayGoroutines.length - 1, idx + 1);
        if (displayGoroutines[next]) setSelectedId(displayGoroutines[next].goroutine_id);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        const idx = selectedId
          ? displayGoroutines.findIndex((g) => g.goroutine_id === selectedId)
          : -1;
        const next = idx <= 0 ? displayGoroutines.length - 1 : idx - 1;
        if (displayGoroutines[next]) setSelectedId(displayGoroutines[next].goroutine_id);
        return;
      }
      if (e.key === "z" || e.key === "Z") {
        e.preventDefault();
        if (selectedId && displayGoroutines.some((g) => g.goroutine_id === selectedId)) {
          setZoomToSelected(true);
        }
        return;
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [selectedId, displayGoroutines, compareOpen, scrubTimeNS]);

  return (
    <div
      className="app"
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <input
        ref={captureInputRef}
        type="file"
        accept=".gtrace,.json"
        onChange={handleCaptureFileChange}
        style={{ display: "none" }}
        aria-hidden
      />

      {/* ── Compact top bar ──────────────────────────────────────────────── */}
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
              {filteredGoroutines.length === goroutines.length
                ? goroutines.length
                : `${filteredGoroutines.length}/${goroutines.length}`}
            </strong>
          </span>
          <span className="topbar-stat-sep" />
          <span
            className={`topbar-stat topbar-stat-btn ${filters.minWaitNs ? "active" : ""}`}
            role="button"
            tabIndex={0}
            title="Filter to long-blocked goroutines (≥1s)"
            onClick={handleLongBlockedClick}
            onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && handleLongBlockedClick()}
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
            onClick={handleLeakClick}
            onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && handleLeakClick()}
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
                onClick={() => { setAnalysisTab("deadlock"); setAnalysisOpen(true); }}
                onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && setAnalysisTab("deadlock")}
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
          <button id="copy-link-btn" type="button" className="action-button secondary" onClick={handleCopyLink}>
            Link
          </button>
          <button type="button" className="action-button" onClick={loadData}>
            Refresh
          </button>
          <button
            type="button"
            className="action-button secondary"
            onClick={handleOpenCapture}
            disabled={replayUploading}
            title="Open .gtrace capture file (or drag-and-drop)"
          >
            {replayUploading ? "Loading…" : "Open"}
          </button>
          <button
            type="button"
            className="action-button secondary"
            onClick={() => setCompareOpen(true)}
            title="Compare two .gtrace captures"
          >
            Compare
          </button>
        </div>
      </header>

      {compareOpen && (
        <div className="compare-overlay">
          <CompareView onClose={() => setCompareOpen(false)} />
        </div>
      )}
      {replayError && (
        <div className="replay-error" role="alert">
          {replayError}
        </div>
      )}

      <main className="workspace">
        <aside className="panel lane-panel">
          <div className="panel-header">
            <h2>Goroutines</h2>
            <p className="goroutine-count-label">
              {goroutines.length > 0
                ? filteredGoroutines.length === goroutines.length
                  ? `${goroutines.length} goroutines`
                  : `${filteredGoroutines.length} of ${goroutines.length} goroutines`
                : ""}
              {brushFilterIds !== null && (
                <span className="brush-filter-badge" title="Filtered by time range selection">
                  ⌖ range
                </span>
              )}
            </p>
          </div>
          <Filters
            filters={filters}
            onFiltersChange={setFilters}
            onJumpTo={handleJumpTo}
            jumpToInputRef={jumpToInputRef}
            distinctLabelPairs={distinctLabelPairs(goroutines)}
          />
          {scrubTimeNS != null && (
            <div className="scrub-list-banner">
              ⏱ Time snapshot active · ESC to clear
            </div>
          )}
          <div className="goroutine-list">
            {listGoroutines.length === 0 ? (
              <p className="empty-message">No goroutines match the current filters.</p>
            ) : (
              <FixedSizeList
                height={GOROUTINE_LIST_HEIGHT}
                itemCount={listGoroutines.length}
                itemSize={GOROUTINE_ITEM_HEIGHT}
                width="100%"
                itemData={{ goroutines: listGoroutines, selectedId, onSelect: handleSelect, segmentsByGoroutine }}
              >
                {GoroutineRow}
              </FixedSizeList>
            )}
          </div>
        </aside>

        <section ref={timelinePanelRef} className="panel timeline-panel">
          <div className="timeline-controls">
            <h2>Timeline</h2>
            <button
              type="button"
              className={`timeline-control-button focus-related-button ${relatedFocus ? "active" : ""}`}
              onClick={() => setRelatedFocus((v) => !v)}
              disabled={selectedId === null}
              title="Focus on selected goroutine and related (parent, children, resource edges)"
              aria-pressed={relatedFocus}
            >
              Related focus
            </button>
            <button
              type="button"
              className="timeline-control-button"
              onClick={() => setZoomToSelected(true)}
              disabled={selectedId === null}
              title="Zoom timeline to selected goroutine (Z)"
            >
              Zoom to G
            </button>
            {zoomToSelected && (
              <button
                type="button"
                className="timeline-control-button reset-zoom-button"
                onClick={() => setZoomToSelected(false)}
                title="Reset zoom"
              >
                Reset zoom
              </button>
            )}
            <button type="button" className="timeline-control-button" onClick={handleSavePng} title="Save timeline as PNG">
              Save PNG
            </button>
            <button type="button" className="timeline-control-button" onClick={handleExportJson} title="Export timeline as JSON">
              Export JSON
            </button>
            <button type="button" className="timeline-control-button" onClick={handleExportChromeTrace} title="Export for chrome://tracing">
              Export Trace
            </button>
            <button
              type="button"
              className="timeline-control-button"
              onClick={handleFullscreen}
              title="Fullscreen timeline"
              aria-pressed={isFullscreen}
            >
              ⛶
            </button>
            <button
              type="button"
              className={`timeline-control-button view-toggle-button ${viewMode === "heatmap" ? "active" : ""}`}
              onClick={() => setViewMode((v) => (v === "lanes" ? "heatmap" : "lanes"))}
              title="Toggle lanes / heatmap view"
              aria-pressed={viewMode === "heatmap"}
            >
              ⊞ Heatmap
            </button>
          </div>
          <Timeline
            ref={timelineRef}
            goroutines={displayGoroutines}
            selectedId={selectedId}
            onSelectGoroutine={handleSelectFromTimeline}
            filters={filters}
            zoomToSelected={zoomToSelected}
            viewMode={viewMode}
            highlightedIds={highlightedIds}
            onBrushFilterChange={setBrushFilterIds}
            scrubTimeNS={scrubTimeNS}
            onScrubChange={setScrubTimeNS}
            onScrubSnapshot={setScrubSnapshot}
            onSegmentsChange={setTimelineSegments}
          />
        </section>

        <aside className="panel inspector-panel">
          <div className="inspector-panel-header">
            <h2>Inspector</h2>
          </div>
          <Inspector
            goroutine={selectedGoroutine}
            goroutines={goroutines}
            segmentOverride={scrubSegmentOverride ?? selectedSegment}
            isScrubActive={scrubTimeNS != null}
            onSelectGoroutine={handleSelect}
            onHighlightBranch={setHighlightedIds}
            highlightActive={highlightedIds !== null}
          />
        </aside>
      </main>

      {/* ── Analysis panel (session-wide) ──────────────────────────────── */}
      <section className="analysis-panel">
        <div className="analysis-panel-header">
          <div className="analysis-tabs">
            {(
              [
                { id: "insights",  label: "Insights"  },
                { id: "hotspots",  label: "Hotspots"  },
                { id: "resources", label: "Resources" },
                { id: "deadlock",  label: "Deadlock"  },
                { id: "groups",    label: "Groups"    },
                { id: "graph",     label: "Graph"     },
              ] as const
            ).map(({ id, label }) => (
              <button
                key={id}
                type="button"
                className={`analysis-tab ${analysisTab === id ? "active" : ""}`}
                onClick={() => {
                  if (analysisTab === id && analysisOpen) {
                    setAnalysisOpen(false);
                  } else {
                    setAnalysisTab(id);
                    setAnalysisOpen(true);
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
            onClick={() => setAnalysisOpen((v) => !v)}
            title={analysisOpen ? "Collapse analysis panel" : "Expand analysis panel"}
            aria-expanded={analysisOpen}
          >
            {analysisOpen ? "▾" : "▴"}
          </button>
        </div>

        {analysisOpen && (
          <div className="analysis-panel-body">
            {analysisTab === "insights" && (
              <SmartInsights refreshKey={dataRevision} onSelectGoroutine={handleSelect} />
            )}
            {analysisTab === "hotspots" && (
              <Hotspots
                hotspots={hotspots}
                activeHotspotIds={filters.hotspotIds ?? null}
                onFilterByHotspot={(ids) =>
                  setFilters((f) => ({ ...f, hotspotIds: ids }))
                }
                onClearHotspotFilter={() =>
                  setFilters((f) => ({ ...f, hotspotIds: null }))
                }
              />
            )}
            {analysisTab === "resources" && (
              <ResourceGraph
                resources={resources}
                contention={contention}
                selectedId={selectedId}
                onSelectGoroutine={handleSelect}
              />
            )}
            {analysisTab === "deadlock" && (
              <DeadlockHints hints={deadlockHints} onSelectGoroutine={handleSelect} />
            )}
            {analysisTab === "groups" && (
              <GoroutineGroups onSelectGoroutine={handleSelect} />
            )}
            {analysisTab === "graph" && (
              <DependencyGraph
                goroutines={goroutines}
                selectedId={selectedId}
                onSelectGoroutine={handleSelect}
              />
            )}
          </div>
        )}
      </section>
    </div>
  );
}
