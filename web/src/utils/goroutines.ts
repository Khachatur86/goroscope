import type { Goroutine } from "../api/client";

const LEAK_THRESHOLD_NS = 30 * 1e9; // 30 seconds

function isRuntimeGoroutine(g: Goroutine): boolean {
  const fn = g.labels?.function ?? "";
  return fn.startsWith("runtime.") || fn.startsWith("internal/");
}

/** Extracts distinct label key=value pairs from goroutines for filter dropdown. */
export function distinctLabelPairs(goroutines: Goroutine[]): string[] {
  const seen = new Set<string>();
  for (const g of goroutines ?? []) {
    for (const [k, v] of Object.entries(g.labels ?? {})) {
      if (k && v != null) seen.add(`${k}=${v}`);
    }
  }
  return [...seen].sort();
}

export function filterAndSortGoroutines(
  goroutines: Goroutine[],
  filters: {
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
  }
): Goroutine[] {
  if (!goroutines || !Array.isArray(goroutines)) return [];
  let filtered = goroutines.filter((g) => {
    if (filters.hotspotIds && filters.hotspotIds.length > 0 && !filters.hotspotIds.includes(g.goroutine_id))
      return false;
    if (filters.hideRuntime && isRuntimeGoroutine(g)) return false;
    if (filters.showLeakOnly) {
      const isLeakState = g.state === "WAITING" || g.state === "BLOCKED";
      const waitLongEnough = (g.wait_ns ?? 0) >= LEAK_THRESHOLD_NS;
      if (!isLeakState || !waitLongEnough) return false;
    }
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
    if (filters.labelFilter) {
      const eq = filters.labelFilter.indexOf("=");
      if (eq > 0) {
        const key = filters.labelFilter.slice(0, eq);
        const value = filters.labelFilter.slice(eq + 1);
        if ((g.labels?.[key] ?? "") !== value) return false;
      }
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
