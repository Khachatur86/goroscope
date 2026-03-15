import { useEffect, useState, useCallback, useRef } from "react";
import type { Goroutine, Session, DeadlockHint } from "./api/client";
import {
  fetchCurrentSession,
  fetchGoroutines,
  fetchGoroutine,
  fetchTimeline,
  fetchResourceGraph,
  fetchInsights,
  fetchDeadlockHints,
} from "./api/client";
import { Filters } from "./filters/Filters";
import { Inspector } from "./inspector/Inspector";
import { Timeline } from "./timeline/Timeline";
import { ResourceGraph } from "./resource-graph/ResourceGraph";
import { filterAndSortGoroutines } from "./utils/goroutines";

type FiltersState = {
  state: string;
  reason: string;
  resource: string;
  search: string;
  minWaitNs: string;
  sortMode: string;
};

function buildShareableURL(filters: FiltersState, selectedId: number | null): string {
  const params = new URLSearchParams();
  if (selectedId) params.set("goroutine", String(selectedId));
  if (filters.state && filters.state !== "ALL") params.set("state", filters.state);
  if (filters.reason) params.set("reason", filters.reason);
  if (filters.resource) params.set("resource", filters.resource);
  if (filters.search) params.set("search", filters.search);
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
  const [resources, setResources] = useState<{ from_goroutine_id: number; to_goroutine_id: number; resource_id?: string }[]>([]);
  const [insights, setInsights] = useState<{ long_blocked_count: number }>({ long_blocked_count: 0 });
  const [deadlockHints, setDeadlockHints] = useState<DeadlockHint[]>([]);
  const [filters, setFilters] = useState<FiltersState>(() => {
    const fromUrl = parseFiltersFromURL();
    return {
      state: fromUrl.state ?? "ALL",
      reason: fromUrl.reason ?? "",
      resource: fromUrl.resource ?? "",
      search: fromUrl.search ?? "",
      minWaitNs: "",
      sortMode: "SUSPICIOUS",
    };
  });

  const filteredGoroutines = filterAndSortGoroutines(goroutines, filters);
  const displayGoroutines =
    selectedId && !filteredGoroutines.some((g) => g.goroutine_id === selectedId)
      ? (() => {
          const sel = goroutines.find((g) => g.goroutine_id === selectedId);
          return sel ? [sel, ...filteredGoroutines] : filteredGoroutines;
        })()
      : filteredGoroutines;

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

  const loadData = useCallback(async () => {
    const goroutineParams =
      hasGoroutineInURL
        ? undefined
        : {
            state: filters.state !== "ALL" ? filters.state : undefined,
            reason: filters.reason || undefined,
            search: filters.search || undefined,
            min_wait_ns: filters.minWaitNs || undefined,
          };
    const timelineParams =
      hasGoroutineInURL
        ? undefined
        : {
            state: filters.state !== "ALL" ? filters.state : undefined,
            reason: filters.reason || undefined,
            search: filters.search || undefined,
          };
    const [sess, gs, , res, ins, deadlock] = await Promise.all([
      fetchCurrentSession(),
      fetchGoroutines(goroutineParams),
      fetchTimeline(timelineParams),
      fetchResourceGraph(),
      fetchInsights(filters.minWaitNs || undefined),
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
    setInsights(ins ?? { long_blocked_count: 0 });
    setDeadlockHints(deadlock?.hints ?? []);
  }, [hasGoroutineInURL, filters.state, filters.reason, filters.search, filters.minWaitNs]);

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 2000);
    return () => clearInterval(interval);
  }, [loadData]);

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
    const qs = params.toString();
    const url = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
    window.history.replaceState(null, "", url);
  }, [selectedId, filters.state, filters.reason, filters.resource, filters.search]);

  const handleSelect = (id: number) => setSelectedId(id);

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

  const blockedCount = (goroutines ?? []).filter((g) =>
    ["BLOCKED", "WAITING", "SYSCALL"].includes(g.state)
  ).length;

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

  const jumpToInputRef = useRef<HTMLInputElement>(null);

  const handleExportJson = async () => {
    const segs = await fetchTimeline({
      state: filters.state !== "ALL" ? filters.state : undefined,
      reason: filters.reason || undefined,
      search: filters.search || undefined,
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

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const active = document.activeElement;
      const isInput = active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement;
      if ((e.ctrlKey || e.metaKey) && e.key === "g") {
        e.preventDefault();
        jumpToInputRef.current?.focus();
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
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [selectedId, displayGoroutines]);

  return (
    <div className="app">
      <header className="hero">
        <div>
          <p className="eyebrow">Local Go Concurrency Debugger</p>
          <h1>Goroscope</h1>
        </div>
        <div className="hero-actions">
          <button id="copy-link-btn" type="button" className="action-button secondary" onClick={handleCopyLink}>
            Copy link
          </button>
          <button type="button" className="action-button" onClick={loadData}>
            Refresh
          </button>
        </div>
      </header>

      <section className="legend-panel">
        <span className="legend-chip running">RUNNING</span>
        <span className="legend-chip runnable">RUNNABLE</span>
        <span className="legend-chip waiting">WAITING</span>
        <span className="legend-chip blocked">BLOCKED</span>
        <span className="legend-chip syscall">SYSCALL</span>
        <span className="legend-chip done">DONE</span>
      </section>

      <section className="summary-bar">
        <div className="summary-card">
          <span className="summary-label">Session</span>
          <strong>{session?.name ?? "—"}</strong>
          <span className="summary-meta">{session?.target ?? ""}</span>
        </div>
        <div className="summary-card">
          <span className="summary-label">Status</span>
          <strong>{session?.status ?? "—"}</strong>
          <span className="summary-meta">{session?.started_at ? new Date(session.started_at).toLocaleString() : ""}</span>
        </div>
        <div className="summary-card">
          <span className="summary-label">Goroutines</span>
          <strong>{filteredGoroutines.length}</strong>
          <span className="summary-meta">{blockedCount} blocked</span>
        </div>
        <div
          className={`summary-card summary-card-action ${filters.minWaitNs ? "active" : ""}`}
          role="button"
          tabIndex={0}
          onClick={handleLongBlockedClick}
          onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && handleLongBlockedClick()}
        >
          <span className="summary-label">Long blocked</span>
          <strong>{insights.long_blocked_count}</strong>
          <span className="summary-meta">≥1s wait</span>
        </div>
        <div className="summary-card">
          <span className="summary-label">Deadlock hints</span>
          <strong>{deadlockHints.length}</strong>
          <span className="summary-meta">potential cycles</span>
        </div>
      </section>

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
            </p>
          </div>
          <Filters filters={filters} onFiltersChange={setFilters} onJumpTo={handleJumpTo} jumpToInputRef={jumpToInputRef} />
          <div className="goroutine-list">
            {displayGoroutines.map((g) => (
              <button
                key={g.goroutine_id}
                type="button"
                className={`lane-item ${selectedId === g.goroutine_id ? "active" : ""}`}
                onClick={() => handleSelect(g.goroutine_id)}
              >
                <span className={`state-pill ${g.state}`}>{g.state}</span>
                <span className="lane-item-title">G{g.goroutine_id}</span>
                <span className="lane-item-meta">
                  {g.labels?.function ?? g.reason ?? "—"}
                </span>
              </button>
            ))}
            {displayGoroutines.length === 0 && (
              <p className="empty-message">No goroutines match the current filters.</p>
            )}
          </div>
        </aside>

        <section className="panel timeline-panel">
          <div className="timeline-controls">
            <h2>Timeline</h2>
            <button type="button" className="timeline-control-button" onClick={handleExportJson}>
              Export JSON
            </button>
          </div>
          <Timeline
            goroutines={displayGoroutines}
            selectedId={selectedId}
            onSelectGoroutine={handleSelect}
            filters={filters}
          />
        </section>

        <aside className="panel inspector-panel">
          <h2>Inspector</h2>
          <Inspector
            goroutine={selectedGoroutine}
            goroutines={goroutines}
            onSelectGoroutine={handleSelect}
          />
          <ResourceGraph
            resources={resources}
            selectedId={selectedId}
            onSelectGoroutine={handleSelect}
          />
        </aside>
      </main>
    </div>
  );
}
