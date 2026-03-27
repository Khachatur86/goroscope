export type FiltersState = {
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

export function buildShareableURL(filters: FiltersState, selectedId: number | null): string {
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
  return qs
    ? `${window.location.origin}${window.location.pathname}?${qs}`
    : window.location.origin + window.location.pathname;
}

export function parseFiltersFromURL(): Partial<FiltersState> {
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

export function parseGoroutineFromURL(): number | null {
  const params = new URLSearchParams(window.location.search);
  const id = params.get("goroutine");
  if (!id) return null;
  const n = parseInt(id, 10);
  return Number.isFinite(n) && n > 0 ? n : null;
}
