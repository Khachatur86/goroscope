import type { Goroutine } from "../api/client";

export function filterAndSortGoroutines(
  goroutines: Goroutine[],
  filters: {
    state: string;
    reason: string;
    resource: string;
    search: string;
    minWaitNs: string;
    sortMode: string;
  }
): Goroutine[] {
  if (!goroutines || !Array.isArray(goroutines)) return [];
  let filtered = goroutines.filter((g) => {
    if (filters.state !== "ALL" && g.state !== filters.state) return false;
    if (filters.reason && g.reason !== filters.reason) return false;
    if (filters.resource && !(g.resource_id ?? "").includes(filters.resource))
      return false;
    if (filters.minWaitNs) {
      const min = parseInt(filters.minWaitNs, 10);
      if (!Number.isFinite(min) || (g.wait_ns ?? 0) < min) return false;
    }
    if (filters.search) {
      const haystack = [
        String(g.goroutine_id),
        g.state,
        g.reason ?? "",
        g.resource_id ?? "",
        g.labels?.function ?? "",
      ]
        .join(" ")
        .toLowerCase();
      if (!haystack.includes(filters.search.toLowerCase())) return false;
    }
    return true;
  });

  filtered = [...filtered].sort((a, b) => compareGoroutines(a, b, filters.sortMode));
  return filtered;
}

function compareGoroutines(
  a: Goroutine,
  b: Goroutine,
  sortMode: string
): number {
  switch (sortMode) {
    case "WAIT_TIME":
      return (b.wait_ns ?? 0) - (a.wait_ns ?? 0) || a.goroutine_id - b.goroutine_id;
    case "BLOCKED":
      return (
        getStateRank(b.state) - getStateRank(a.state) ||
        (b.wait_ns ?? 0) - (a.wait_ns ?? 0) ||
        a.goroutine_id - b.goroutine_id
      );
    case "SUSPICIOUS":
      return (
        (b.wait_ns ?? 0) - (a.wait_ns ?? 0) ||
        getStateRank(b.state) - getStateRank(a.state) ||
        a.goroutine_id - b.goroutine_id
      );
    case "ID":
    default:
      return a.goroutine_id - b.goroutine_id;
  }
}

function getStateRank(state: string): number {
  switch (state) {
    case "BLOCKED":
      return 5;
    case "WAITING":
      return 4;
    case "SYSCALL":
      return 3;
    case "RUNNABLE":
      return 2;
    case "RUNNING":
      return 1;
    default:
      return 0;
  }
}
