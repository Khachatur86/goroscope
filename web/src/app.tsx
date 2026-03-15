import { useEffect, useState } from "react";
import type { Goroutine, Session } from "./api/client";
import {
  fetchCurrentSession,
  fetchGoroutines,
  fetchGoroutine,
} from "./api/client";
import { Filters } from "./filters/Filters";
import { Inspector } from "./inspector/Inspector";
import { Timeline } from "./timeline/Timeline";

export function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [goroutines, setGoroutines] = useState<Goroutine[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [selectedGoroutine, setSelectedGoroutine] = useState<Goroutine | null>(
    null
  );
  const [filters, setFilters] = useState({
    state: "ALL",
    reason: "",
    search: "",
    minWaitNs: "",
  });

  const loadData = async () => {
    const [sess, gs] = await Promise.all([
      fetchCurrentSession(),
      fetchGoroutines({
        state: filters.state !== "ALL" ? filters.state : undefined,
        reason: filters.reason || undefined,
        search: filters.search || undefined,
        min_wait_ns: filters.minWaitNs || undefined,
      }),
    ]);
    setSession(sess ?? null);
    setGoroutines(gs);
  };

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 2000);
    return () => clearInterval(interval);
  }, [filters.state, filters.reason, filters.search, filters.minWaitNs]);

  useEffect(() => {
    if (selectedId) {
      fetchGoroutine(selectedId).then(setSelectedGoroutine);
    } else {
      setSelectedGoroutine(null);
    }
  }, [selectedId]);

  const blockedCount = goroutines.filter((g) =>
    ["BLOCKED", "WAITING", "SYSCALL"].includes(g.state)
  ).length;

  return (
    <div className="app">
      <header className="hero">
        <div>
          <p className="eyebrow">Local Go Concurrency Debugger</p>
          <h1>Goroscope</h1>
        </div>
        <button type="button" className="action-button" onClick={loadData}>
          Refresh
        </button>
      </header>

      <section className="summary-bar">
        <div className="summary-card">
          <span className="summary-label">Session</span>
          <strong>{session?.name ?? "—"}</strong>
        </div>
        <div className="summary-card">
          <span className="summary-label">Status</span>
          <strong>{session?.status ?? "—"}</strong>
        </div>
        <div className="summary-card">
          <span className="summary-label">Goroutines</span>
          <strong>{goroutines.length}</strong>
          <span className="summary-meta">{blockedCount} blocked</span>
        </div>
      </section>

      <main className="workspace">
        <aside className="panel lane-panel">
          <h2>Goroutines</h2>
          <Filters
            filters={filters}
            onFiltersChange={setFilters}
            onJumpTo={(id) => setSelectedId(id)}
          />
          <div className="goroutine-list">
            {goroutines.slice(0, 100).map((g) => (
              <button
                key={g.goroutine_id}
                type="button"
                className={`lane-item ${selectedId === g.goroutine_id ? "active" : ""}`}
                onClick={() => setSelectedId(g.goroutine_id)}
              >
                <span className={`state-pill ${g.state}`}>{g.state}</span>
                <span className="lane-item-title">G{g.goroutine_id}</span>
                <span className="lane-item-meta">
                  {g.labels?.function ?? g.reason ?? "—"}
                </span>
              </button>
            ))}
            {goroutines.length > 100 && (
              <p className="lane-more">+{goroutines.length - 100} more</p>
            )}
          </div>
        </aside>

        <section className="panel timeline-panel">
          <h2>Timeline</h2>
          <Timeline
            goroutines={goroutines}
            selectedId={selectedId}
            onSelectGoroutine={setSelectedId}
          />
        </section>

        <aside className="panel inspector-panel">
          <h2>Inspector</h2>
          <Inspector goroutine={selectedGoroutine} />
        </aside>
      </main>
    </div>
  );
}
