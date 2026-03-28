import { useEffect, useState, useCallback, useMemo, useRef, lazy, Suspense, startTransition } from "react";
import { FixedSizeList } from "react-window";
import type { Goroutine, Session, DeadlockHint, TimelineSegment, SampleInfo } from "./api/client";
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
import type { FiltersState } from "./filters/url";
import { buildShareableURL, parseFiltersFromURL, parseGoroutineFromURL } from "./filters/url";
import { Inspector } from "./inspector/Inspector";
import { computeSpawnHotspots } from "./inspector/Hotspots";
import { Timeline } from "./timeline/Timeline";
const CompareView = lazy(() => import("./compare/CompareView").then((m) => ({ default: m.CompareView })));
import { CommandPalette, type Command } from "./palette/CommandPalette";
import { distinctLabelPairs, filterAndSortGoroutines } from "./utils/goroutines";
import { usePanelResize, PanelDivider } from "./panels/PanelDivider";
import { GoroutineRow } from "./goroutine-list/GoroutineRow";
import { loadPinned, savePinned } from "./goroutine-list/pinned";
import type { PinnedMap } from "./goroutine-list/pinned";
import { usePlayback } from "./timeline/usePlayback";
import { Topbar } from "./topbar/Topbar";
import { AnalysisPanel } from "./analysis/AnalysisPanel";
import type { AnalysisTabId } from "./analysis/AnalysisPanel";

// ── Panel resize constants ────────────────────────────────────────────────────
const LS_LANE_WIDTH = "goroscope:laneWidth";
const LS_INSPECTOR_WIDTH = "goroscope:inspectorWidth";
const LANE_WIDTH_DEFAULT = 280;
const INSPECTOR_WIDTH_DEFAULT = 300;

/** Height of one row in the virtualised goroutine list (px). */
const GOROUTINE_ITEM_HEIGHT = 44;
/** Visible height of the virtualised goroutine list (px). */
const GOROUTINE_LIST_HEIGHT = 400;

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
  const [sampleInfo, setSampleInfo] = useState<SampleInfo | null>(null);
  const [relatedFocus, setRelatedFocus] = useState(false);
  const [zoomToSelected, setZoomToSelected] = useState(false);
  const [viewMode, setViewMode] = useState<"lanes" | "heatmap">("lanes");
  const [analysisTab, setAnalysisTab] = useState<AnalysisTabId>("insights");
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

  // Panel resize (U-1).
  const { width: laneWidth, startDrag: startLaneDrag } = usePanelResize(LS_LANE_WIDTH, LANE_WIDTH_DEFAULT);
  const { width: inspectorWidth, startDrag: startInspectorDrag } = usePanelResize(LS_INSPECTOR_WIDTH, INSPECTOR_WIDTH_DEFAULT);

  // Goroutine watchlist (U-3).
  const [pinned, setPinned] = useState<PinnedMap>(() => loadPinned());

  const handleTogglePin = useCallback((id: number) => {
    setPinned((prev) => {
      const next = new Map(prev);
      if (next.has(id)) next.delete(id);
      else next.set(id, "");
      savePinned(next);
      return next;
    });
  }, []);

  const handleSetNote = useCallback((id: number, note: string) => {
    setPinned((prev) => {
      if (!prev.has(id)) return prev;
      const next = new Map(prev);
      next.set(id, note.slice(0, 80));
      savePinned(next);
      return next;
    });
  }, []);

  // Time scrubber.
  const [scrubTimeNS, setScrubTimeNS] = useState<number | null>(null);
  const [scrubSnapshot, setScrubSnapshot] = useState<ScrubSnapshot[]>([]);

  // Playback controls (U-2).
  const [isPlaying, setIsPlaying] = useState(false);
  const [playSpeed, setPlaySpeed] = useState<1 | 2 | 4>(1);

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

  const { traceMinNS, traceMaxNS } = useMemo(() => {
    let lo = Infinity;
    let hi = -Infinity;
    for (const s of timelineSegments) {
      if (s.start_ns < lo) lo = s.start_ns;
      if (s.end_ns > hi) hi = s.end_ns;
    }
    return { traceMinNS: lo === Infinity ? 0 : lo, traceMaxNS: hi === -Infinity ? 0 : hi };
  }, [timelineSegments]);

  usePlayback({
    isPlaying,
    playSpeed,
    traceMinNS,
    traceMaxNS,
    scrubTimeNS,
    onScrubChange: setScrubTimeNS,
    onStop: () => setIsPlaying(false),
  });

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
    displayGoroutines = displayGoroutines.filter(
      (g) => brushFilterIds.has(g.goroutine_id) || g.goroutine_id === selectedId
    );
  }

  if (pinned.size > 0) {
    const pinnedInList = displayGoroutines.filter((g) => pinned.has(g.goroutine_id));
    const rest = displayGoroutines.filter((g) => !pinned.has(g.goroutine_id));
    const pinnedNotVisible = [...pinned.keys()]
      .filter((id) => !displayGoroutines.some((g) => g.goroutine_id === id))
      .map((id) => goroutines.find((g) => g.goroutine_id === id))
      .filter((g): g is Goroutine => g !== undefined);
    displayGoroutines = [...pinnedNotVisible, ...pinnedInList, ...rest];
  }

  const scrubMap = useMemo(
    () => new Map(scrubSnapshot.map((s) => [s.goroutine_id, s])),
    [scrubSnapshot]
  );

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

  const stackFrameNeedle = filters.search.toLowerCase().startsWith("stack:")
    ? filters.search.slice("stack:".length).trim()
    : null;

  const goroutineParams = useMemo(
    () =>
      hasGoroutineInURL
        ? undefined
        : {
            state: filters.state !== "ALL" ? filters.state : undefined,
            reason: filters.reason || undefined,
            search: stackFrameNeedle !== null ? undefined : filters.search || undefined,
            stack_frame: stackFrameNeedle || undefined,
            min_wait_ns: filters.minWaitNs || undefined,
            label: filters.labelFilter || undefined,
          },
    [hasGoroutineInURL, filters.state, filters.reason, filters.search, stackFrameNeedle, filters.minWaitNs, filters.labelFilter]
  );

  // Only replace the goroutines array when content has actually changed.
  // Polling returns identical data most of the time; keeping the same reference
  // prevents useMemo / useEffect hooks that depend on `goroutines` from firing
  // and stops the Timeline canvas + goroutine list from flickering every 5 s.
  const stableSetGoroutines = useCallback((next: Goroutine[]) => {
    setGoroutines((prev) => {
      if (prev.length !== next.length) return next;
      for (let i = 0; i < prev.length; i++) {
        const a = prev[i], b = next[i];
        if (
          a.goroutine_id !== b.goroutine_id ||
          a.state !== b.state ||
          a.reason !== b.reason ||
          a.wait_ns !== b.wait_ns
        ) return next;
      }
      return prev; // same content — keep stable reference
    });
  }, []);

  const loadData = useCallback(async () => {
    const [sess, gs, res, contentionData, ins, deadlock] = await Promise.all([
      fetchCurrentSession(),
      fetchGoroutines(goroutineParams),
      fetchResourceGraph(),
      fetchResourceContention(),
      fetchInsights(filters.minWaitNs || undefined, "30000000000"),
      fetchDeadlockHints(),
    ]);
    startTransition(() => {
      setSession(sess ?? null);
      const gsSafe = gs?.goroutines ?? [];
      stableSetGoroutines(gsSafe);
      setSampleInfo(gs?.sampleInfo ?? null);
      const urlId = parseGoroutineFromURL();
      if (urlId && gsSafe.some((g) => g.goroutine_id === urlId)) {
        setSelectedId(urlId);
      }
      setResources(Array.isArray(res) ? res : []);
      setContention(Array.isArray(contentionData) ? contentionData : []);
      setInsights(ins ?? { long_blocked_count: 0, leak_candidates_count: 0 });
      setDeadlockHints(deadlock?.hints ?? []);
    });
  }, [goroutineParams, filters.minWaitNs, stableSetGoroutines]);

  const refreshLive = useCallback(async () => {
    const gs = await fetchGoroutines(goroutineParams).catch(() => null);
    if (!gs) return;
    const gsSafe = gs.goroutines;
    // Mark SSE-triggered updates as non-urgent so React can yield to user interactions.
    startTransition(() => {
      stableSetGoroutines(gsSafe);
      setSampleInfo(gs.sampleInfo);
      const urlId = parseGoroutineFromURL();
      if (urlId && gsSafe.some((g) => g.goroutine_id === urlId)) {
        setSelectedId(urlId);
      }
    });
  }, [goroutineParams, stableSetGoroutines]);

  useEffect(() => {
    loadData();
    const fullRefreshTimer = setInterval(loadData, 5000);

    let source: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let alive = true;

    const connect = () => {
      if (!alive) return;
      setStreamStatus("connecting");
      source = new EventSource("/api/v1/stream");
      source.addEventListener("connected", () => { setStreamStatus("live"); });
      source.addEventListener("update", () => { refreshLive(); });
      source.onerror = () => {
        setStreamStatus("disconnected");
        source?.close();
        source = null;
        if (alive) reconnectTimer = setTimeout(connect, 3000);
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
  const [replayUploading, setReplayUploading] = useState(false);
  const [replayError, setReplayError] = useState<string | null>(null);
  const [compareOpen, setCompareOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [gifExporting, setGifExporting] = useState(false);
  const [highlightedIds, setHighlightedIds] = useState<Set<number> | null>(null);

  // Stable itemData reference for FixedSizeList: only changes when the underlying
  // data changes. Without this, an inline object literal creates a new reference
  // every render and forces all visible GoroutineRow components to re-render even
  // when nothing about them changed, causing visible flicker in the left panel.
  const goroutineListItemData = useMemo(
    () => ({ goroutines: listGoroutines, selectedId, onSelect: handleSelect, segmentsByGoroutine, pinned, onTogglePin: handleTogglePin }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [listGoroutines, selectedId, segmentsByGoroutine, pinned, handleTogglePin]
  );

  const handleSavePng = useCallback(() => {
    timelineRef.current?.exportPng();
  }, []);

  const handleSaveGif = useCallback(() => {
    if (gifExporting) return;
    setGifExporting(true);
    timelineRef.current?.exportGif(24, 12, () => setGifExporting(false));
  }, [gifExporting]);

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

  // ── Global keyboard shortcuts ────────────────────────────────────────────
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setPaletteOpen((v) => !v);
        return;
      }
      const tag = (document.activeElement?.tagName ?? "").toLowerCase();
      if (tag === "input" || tag === "textarea" || paletteOpen) return;

      switch (e.key) {
        case "Escape":
          setSelectedId(null);
          setSelectedGoroutine(null);
          setSelectedSegment(null);
          break;
        case "r":
          if (!e.metaKey && !e.ctrlKey) loadData();
          break;
        case "z":
          if (selectedId !== null) setZoomToSelected(true);
          break;
        case "f":
          if (selectedId !== null) setRelatedFocus((v) => !v);
          break;
        case "p":
          handleSavePng();
          break;
        case "1": setAnalysisTab("insights");  setAnalysisOpen(true); break;
        case "2": setAnalysisTab("hotspots");  setAnalysisOpen(true); break;
        case "3": setAnalysisTab("resources"); setAnalysisOpen(true); break;
        case "4": setAnalysisTab("deadlock");  setAnalysisOpen(true); break;
        case "5": setAnalysisTab("groups");    setAnalysisOpen(true); break;
        case "6": setAnalysisTab("graph");     setAnalysisOpen(true); break;
        case "7": setAnalysisTab("heatmap");   setAnalysisOpen(true); break;
        case "`": setAnalysisOpen((v) => !v);  break;
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paletteOpen, selectedId, handleSavePng]);

  // ── Arrow-key / scrub keyboard shortcuts ────────────────────────────────
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
      if (e.key === "Escape" && scrubTimeNS != null) {
        e.preventDefault();
        setScrubTimeNS(null);
        return;
      }
      if (isInput) return;
      if (displayGoroutines.length === 0) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        const idx = selectedId ? displayGoroutines.findIndex((g) => g.goroutine_id === selectedId) : -1;
        const next = idx < 0 ? 0 : Math.min(displayGoroutines.length - 1, idx + 1);
        if (displayGoroutines[next]) setSelectedId(displayGoroutines[next].goroutine_id);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        const idx = selectedId ? displayGoroutines.findIndex((g) => g.goroutine_id === selectedId) : -1;
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

  // ── Command palette commands ─────────────────────────────────────────────
  const paletteCommands = useMemo<Command[]>(() => [
    { id: "tab-insights",   group: "Analysis tabs", icon: "💡", label: "Open Insights tab",   hint: "1", action: () => { setAnalysisTab("insights");  setAnalysisOpen(true); } },
    { id: "tab-hotspots",   group: "Analysis tabs", icon: "🔥", label: "Open Hotspots tab",   hint: "2", action: () => { setAnalysisTab("hotspots");  setAnalysisOpen(true); } },
    { id: "tab-resources",  group: "Analysis tabs", icon: "🔗", label: "Open Resources tab",  hint: "3", action: () => { setAnalysisTab("resources"); setAnalysisOpen(true); } },
    { id: "tab-deadlock",   group: "Analysis tabs", icon: "🔴", label: "Open Deadlock tab",   hint: "4", action: () => { setAnalysisTab("deadlock");  setAnalysisOpen(true); } },
    { id: "tab-groups",     group: "Analysis tabs", icon: "📦", label: "Open Groups tab",     hint: "5", action: () => { setAnalysisTab("groups");    setAnalysisOpen(true); } },
    { id: "tab-graph",      group: "Analysis tabs", icon: "🕸️", label: "Open Graph tab",      hint: "6", action: () => { setAnalysisTab("graph");     setAnalysisOpen(true); } },
    { id: "tab-heatmap",    group: "Analysis tabs", icon: "🌡️", label: "Open Heatmap tab",    hint: "7", action: () => { setAnalysisTab("heatmap");   setAnalysisOpen(true); } },
    { id: "zoom-selected",  group: "Timeline", icon: "🔎", label: "Zoom to selected goroutine", hint: "Z", keywords: ["zoom"], action: () => { if (selectedId !== null) setZoomToSelected(true); } },
    { id: "reset-zoom",     group: "Timeline", icon: "↩",  label: "Reset timeline zoom",                 keywords: ["zoom", "reset"], action: () => setZoomToSelected(false) },
    { id: "related-focus",  group: "Timeline", icon: "👁",  label: "Toggle related-focus",      hint: "F", keywords: ["focus", "related", "filter"], action: () => { if (selectedId !== null) setRelatedFocus((v) => !v); } },
    { id: "save-png",       group: "Timeline", icon: "🖼", label: "Save timeline as PNG",       hint: "P", keywords: ["export", "image", "screenshot"], action: handleSavePng },
    { id: "save-gif",       group: "Timeline", icon: "🎞", label: "Export timeline as GIF",              keywords: ["export", "gif", "animation", "animated"], action: handleSaveGif },
    { id: "refresh",        group: "Data",     icon: "♻",  label: "Refresh data",               hint: "R", keywords: ["reload", "refresh"], action: loadData },
    { id: "open-capture",   group: "Data",     icon: "📂", label: "Open .gtrace capture",                 keywords: ["open", "file", "upload", "trace"], action: () => captureInputRef.current?.click() },
    { id: "compare",        group: "Data",     icon: "⚖",  label: "Compare two captures",                keywords: ["diff", "compare", "traces"], action: () => setCompareOpen(true) },
    { id: "toggle-analysis",group: "View",     icon: "📊", label: "Toggle analysis panel",      hint: "`", keywords: ["collapse", "hide", "panel"], action: () => setAnalysisOpen((v) => !v) },
    { id: "clear-selection",group: "View",     icon: "✕",  label: "Clear selection",             hint: "Esc", keywords: ["deselect", "clear"], action: () => { setSelectedId(null); setSelectedGoroutine(null); setSelectedSegment(null); } },
  // eslint-disable-next-line react-hooks/exhaustive-deps
  ], [selectedId, handleSavePng, handleSaveGif]);

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
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
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
    const blob = new Blob([JSON.stringify({ traceEvents: events })], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `goroscope-trace-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  return (
    <div className="app" onDragOver={handleDragOver} onDrop={handleDrop}>
      <input
        ref={captureInputRef}
        type="file"
        accept=".gtrace,.json"
        onChange={handleCaptureFileChange}
        style={{ display: "none" }}
        aria-hidden
      />

      <Topbar
        session={session}
        goroutineCount={goroutines.length}
        filteredCount={filteredGoroutines.length}
        insights={insights}
        deadlockHints={deadlockHints}
        streamStatus={streamStatus}
        replayUploading={replayUploading}
        filters={filters}
        onLongBlockedClick={() => setFilters((f) => ({ ...f, minWaitNs: "1000000000" }))}
        onLeakClick={() => setFilters((f) => ({ ...f, showLeakOnly: true }))}
        onDeadlockClick={() => { setAnalysisTab("deadlock"); setAnalysisOpen(true); }}
        onCopyLink={handleCopyLink}
        onRefresh={loadData}
        onOpenCapture={handleOpenCapture}
        onCompare={() => setCompareOpen(true)}
        onOpenPalette={() => setPaletteOpen(true)}
      />

      {compareOpen && (
        <div className="compare-overlay">
          <Suspense fallback={null}>
            <CompareView onClose={() => setCompareOpen(false)} />
          </Suspense>
        </div>
      )}
      {replayError && (
        <div className="replay-error" role="alert">
          {replayError}
        </div>
      )}
      {sampleInfo && (
        <div className="sample-warning" role="status">
          <span className="sample-warning-icon">&#9888;</span>
          {sampleInfo.warning}
          <button
            className="sample-warning-dismiss"
            onClick={() => setSampleInfo(null)}
            aria-label="Dismiss sampling warning"
          >
            &#10005;
          </button>
        </div>
      )}

      <main className="workspace">
        <aside className="panel lane-panel" style={{ width: laneWidth, flexShrink: 0 }}>
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
                itemData={goroutineListItemData}
              >
                {GoroutineRow}
              </FixedSizeList>
            )}
          </div>
        </aside>

        <PanelDivider onMouseDown={(e) => startLaneDrag(e, "right")} />

        <section ref={timelinePanelRef} className="panel timeline-panel">
          <div className="timeline-controls">
            <h2>Timeline</h2>
            <button
              type="button"
              className={`btn btn--ghost focus-related-button ${relatedFocus ? "active" : ""}`}
              onClick={() => setRelatedFocus((v) => !v)}
              disabled={selectedId === null}
              title="Focus on selected goroutine and related (parent, children, resource edges)"
              aria-pressed={relatedFocus}
            >
              Related focus
            </button>
            <button
              type="button"
              className="btn btn--ghost"
              onClick={() => setZoomToSelected(true)}
              disabled={selectedId === null}
              title="Zoom timeline to selected goroutine (Z)"
            >
              Zoom to G
            </button>
            {zoomToSelected && (
              <button
                type="button"
                className="btn btn--ghost reset-zoom-button"
                onClick={() => setZoomToSelected(false)}
                title="Reset zoom"
              >
                Reset zoom
              </button>
            )}
            <button type="button" className="btn btn--ghost" onClick={handleSavePng} title="Save timeline as PNG (P)">
              Save PNG
            </button>
            <button
              type="button"
              className={`btn btn--ghost gif-export-btn ${gifExporting ? "gif-export-btn--busy" : ""}`}
              onClick={handleSaveGif}
              disabled={gifExporting}
              title="Export timeline as animated GIF"
            >
              {gifExporting ? "GIF…" : "Save GIF"}
            </button>
            <button type="button" className="btn btn--ghost" onClick={handleExportJson} title="Export timeline as JSON">
              Export JSON
            </button>
            <button type="button" className="btn btn--ghost" onClick={handleExportChromeTrace} title="Export for chrome://tracing">
              Export Trace
            </button>
            <button
              type="button"
              className="btn btn--ghost"
              onClick={handleFullscreen}
              title="Fullscreen timeline"
              aria-pressed={isFullscreen}
            >
              ⛶
            </button>
            <button
              type="button"
              className={`btn btn--ghost view-toggle-button ${viewMode === "heatmap" ? "active" : ""}`}
              onClick={() => setViewMode((v) => (v === "lanes" ? "heatmap" : "lanes"))}
              title="Toggle lanes / heatmap view"
              aria-pressed={viewMode === "heatmap"}
            >
              ⊞ Heatmap
            </button>

            {/* ── Playback controls (U-2) ── */}
            {traceMaxNS > traceMinNS && (
              <span className="playback-controls">
                <button
                  type="button"
                  className="btn btn--ghost playback-btn"
                  title="Reset playback to start"
                  onClick={() => { setIsPlaying(false); setScrubTimeNS(traceMinNS); }}
                >
                  ⏮
                </button>
                <button
                  type="button"
                  className={`btn btn--ghost playback-btn${isPlaying ? " active" : ""}`}
                  title={isPlaying ? "Pause" : "Play"}
                  onClick={() => setIsPlaying((v) => !v)}
                >
                  {isPlaying ? "⏸" : "▶"}
                </button>
                {([1, 2, 4] as const).map((s) => (
                  <button
                    key={s}
                    type="button"
                    className={`btn btn--ghost playback-speed-btn${playSpeed === s ? " active" : ""}`}
                    onClick={() => setPlaySpeed(s)}
                    title={`${s}× speed`}
                  >
                    {s}×
                  </button>
                ))}
              </span>
            )}
          </div>
          <Timeline
            ref={timelineRef}
            goroutines={displayGoroutines}
            selectedId={selectedId}
            onSelectGoroutine={handleSelectFromTimeline}
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

        <PanelDivider onMouseDown={(e) => startInspectorDrag(e, "left")} />

        <aside className="panel inspector-panel" style={{ width: inspectorWidth, flexShrink: 0 }}>
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
            stackFrameNeedle={stackFrameNeedle ?? undefined}
            isPinned={selectedGoroutine !== null && pinned.has(selectedGoroutine.goroutine_id)}
            pinnedNote={selectedGoroutine !== null ? pinned.get(selectedGoroutine.goroutine_id) : undefined}
            onSetNote={selectedGoroutine !== null ? (note) => handleSetNote(selectedGoroutine.goroutine_id, note) : undefined}
          />
        </aside>
      </main>

      <AnalysisPanel
        tab={analysisTab}
        open={analysisOpen}
        onTabChange={setAnalysisTab}
        onToggleOpen={() => setAnalysisOpen((v) => !v)}
        goroutines={goroutines}
        selectedId={selectedId}
        resources={resources}
        contention={contention}
        deadlockHints={deadlockHints}
        timelineSegments={timelineSegments}
        hotspots={hotspots}
        filters={filters}
        onSelectGoroutine={handleSelect}
        onFilterByHotspot={(ids) => setFilters((f) => ({ ...f, hotspotIds: ids }))}
        onClearHotspotFilter={() => setFilters((f) => ({ ...f, hotspotIds: null }))}
        onSetFilters={setFilters}
        onSetScrubTime={setScrubTimeNS}
        onHighlightRequest={setHighlightedIds}
      />

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        commands={paletteCommands}
        goroutines={goroutines}
        onSelectGoroutine={handleSelect}
      />
    </div>
  );
}
