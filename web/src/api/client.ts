export type Session = {
  id: string;
  name: string;
  target: string;
  status: string;
  started_at?: string;
  error?: string;
};

export type Goroutine = {
  goroutine_id: number;
  state: string;
  reason?: string;
  resource_id?: string;
  wait_ns?: number;
  created_at?: string;
  last_seen_at?: string;
  /** Nanosecond timestamp when the goroutine was first observed (H-2). */
  born_ns?: number;
  /** Nanosecond timestamp when the goroutine reached DONE state (H-2). */
  died_ns?: number;
  /** False if the goroutine has finished (H-2). */
  is_alive?: boolean;
  parent_id?: number;
  labels?: Record<string, string>;
  last_stack?: {
    frames: Array<{ func: string; file: string; line: number }>;
  };
};

export type TimelineSegment = {
  goroutine_id: number;
  start_ns: number;
  end_ns: number;
  state: string;
  reason?: string;
  resource_id?: string;
};

export type ResourceEdge = {
  from_goroutine_id: number;
  to_goroutine_id: number;
  reason?: string;
  resource_id?: string;
};

import { fetchJsonViaWorker } from "../workers/fetchViaWorker";

const base = "";

async function fetchJson<T>(path: string): Promise<T> {
  const res = await fetch(`${base}${path}`);
  if (!res.ok) {
    throw new Error(`fetch ${path}: ${res.status}`);
  }
  return res.json();
}

/** Like fetchJson but parses JSON in a Web Worker to avoid main-thread jank. */
async function fetchJsonOffThread<T>(path: string): Promise<T> {
  return fetchJsonViaWorker<T>(`${base}${path}`);
}

export async function fetchCurrentSession(): Promise<Session | null> {
  try {
    return await fetchJson<Session>("/api/v1/session/current");
  } catch {
    return null;
  }
}

export type SampleInfo = {
  sampled: boolean;
  totalCount: number;
  displayCount: number;
  warning: string;
};

export type GoroutineListResult = {
  goroutines: Goroutine[];
  sampleInfo: SampleInfo | null;
};

type GoroutineListEnvelope = {
  goroutines?: Goroutine[];
  sampled?: boolean;
  total_count?: number;
  display_count?: number;
  warning?: string;
};

export async function fetchGoroutines(params?: {
  state?: string;
  reason?: string;
  search?: string;
  stack_frame?: string;
  min_wait_ns?: string;
  label?: string;
  limit?: number;
  offset?: number;
}): Promise<GoroutineListResult> {
  const q = new URLSearchParams();
  if (params?.state && params.state !== "ALL") q.set("state", params.state);
  if (params?.reason) q.set("reason", params.reason);
  if (params?.search) q.set("search", params.search);
  if (params?.stack_frame) q.set("stack_frame", params.stack_frame);
  if (params?.min_wait_ns) q.set("min_wait_ns", params.min_wait_ns);
  if (params?.label) q.set("label", params.label);
  if (params?.limit) q.set("limit", String(params.limit));
  if (params?.offset) q.set("offset", String(params.offset));
  const query = q.toString();
  const path = `/api/v1/goroutines${query ? `?${query}` : ""}`;
  const data = await fetchJsonOffThread<Goroutine[] | GoroutineListEnvelope>(path);

  if (Array.isArray(data)) {
    return { goroutines: data, sampleInfo: null };
  }

  const goroutines = data?.goroutines ?? [];
  const sampleInfo: SampleInfo | null = data?.sampled
    ? {
        sampled: true,
        totalCount: data.total_count ?? goroutines.length,
        displayCount: data.display_count ?? goroutines.length,
        warning: data.warning ?? "",
      }
    : null;

  return { goroutines, sampleInfo };
}

export async function fetchGoroutine(id: number): Promise<Goroutine | null> {
  try {
    return await fetchJson<Goroutine>(`/api/v1/goroutines/${id}`);
  } catch {
    return null;
  }
}

export type StackAtResponse = {
  frames: Array<{ func: string; file: string; line: number }>;
};

export async function fetchStackAt(goroutineId: number, ns: number): Promise<StackAtResponse | null> {
  try {
    return await fetchJson<StackAtResponse>(`/api/v1/goroutines/${goroutineId}/stack-at?ns=${ns}`);
  } catch {
    return null;
  }
}

/** One stack snapshot as returned by GET /api/v1/goroutines/{id}/stacks */
export type StackSnapshot = {
  goroutine_id: number;
  timestamp: string;
  frames: Array<{ func: string; file: string; line: number }>;
};

export type StacksResponse = {
  goroutine_id: number;
  stacks: StackSnapshot[];
};

/** Fetch all historical stack snapshots for a goroutine (for flame graph). */
export async function fetchStacks(goroutineId: number): Promise<StackSnapshot[]> {
  try {
    const res = await fetchJson<StacksResponse>(`/api/v1/goroutines/${goroutineId}/stacks`);
    return res?.stacks ?? [];
  } catch {
    return [];
  }
}

/** Fetch all stack snapshots across all goroutines within a time window [startNs, endNs].
 * Used to build the cross-goroutine CPU flame graph overlay for a selected segment. */
export async function fetchPprofStacks(startNs: number, endNs: number): Promise<StackSnapshot[]> {
  try {
    const res = await fetchJson<{ stacks: StackSnapshot[] }>(
      `/api/v1/pprof/stacks?start_ns=${startNs}&end_ns=${endNs}`
    );
    return res?.stacks ?? [];
  } catch {
    return [];
  }
}

export async function fetchTimeline(params?: {
  state?: string;
  reason?: string;
  search?: string;
  label?: string;
  /** When set, only return segments for these goroutine IDs (lazy-load for visible range). */
  goroutineIds?: number[];
}): Promise<TimelineSegment[]> {
  const q = new URLSearchParams();
  if (params?.state && params.state !== "ALL") q.set("state", params.state);
  if (params?.reason) q.set("reason", params.reason);
  if (params?.search) q.set("search", params.search);
  if (params?.label) q.set("label", params.label);
  if (params?.goroutineIds && params.goroutineIds.length > 0) {
    q.set("goroutine_ids", params.goroutineIds.join(","));
  }
  const query = q.toString();
  const path = `/api/v1/timeline${query ? `?${query}` : ""}`;
  const data = await fetchJson<TimelineSegment[] | null>(path);
  return Array.isArray(data) ? data : [];
}

export async function fetchResourceGraph(): Promise<ResourceEdge[]> {
  return fetchJson<ResourceEdge[]>("/api/v1/resources/graph");
}

export type ResourceContention = {
  resource_id: string;
  peak_waiters: number;
  segment_count: number;
  total_wait_ns: number;
  avg_wait_ns: number;
};

export async function fetchResourceContention(): Promise<ResourceContention[]> {
  const data = await fetchJson<{ contention: ResourceContention[] }>(
    "/api/v1/resources/graph?view=contention"
  );
  return data?.contention ?? [];
}

export type ProcessorSegment = {
  processor_id: number;
  goroutine_id: number;
  start_ns: number;
  end_ns: number;
};

export async function fetchProcessorTimeline(): Promise<ProcessorSegment[]> {
  const data = await fetchJson<ProcessorSegment[] | null>("/api/v1/processor-timeline").catch(() => null);
  return Array.isArray(data) ? data : [];
}

export type Insights = {
  long_blocked_count: number;
  leak_candidates_count?: number;
};
export type DeadlockHint = {
  goroutine_ids: number[];
  resource_ids: string[];
  blame_chain?: string;
};

export async function fetchInsights(
  minWaitNs?: string,
  leakThresholdNs?: string
): Promise<Insights> {
  const params = new URLSearchParams();
  if (minWaitNs) params.set("min_wait_ns", minWaitNs);
  if (leakThresholdNs) params.set("leak_threshold_ns", leakThresholdNs);
  const q = params.toString() ? `?${params.toString()}` : "";
  return fetchJson<Insights>(`/api/v1/insights${q}`).catch(() => ({
    long_blocked_count: 0,
    leak_candidates_count: 0,
  }));
}

export async function fetchDeadlockHints(): Promise<{ hints: DeadlockHint[] }> {
  return fetchJson<{ hints: DeadlockHint[] }>("/api/v1/deadlock-hints").catch(() => ({ hints: [] }));
}

/** Upload a .gtrace file for replay. Returns session_id on success. */
export async function uploadReplayCapture(file: File): Promise<{ status: string; session_id: string }> {
  const form = new FormData();
  form.append("file", file);
  const res = await fetch("/api/v1/replay/load", {
    method: "POST",
    body: form,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `upload failed: ${res.status}`);
  }
  return res.json();
}

export type InsightSeverity = "critical" | "warning" | "info";
export type InsightKind =
  | "deadlock"
  | "leak"
  | "contention"
  | "blocking"
  | "goroutine_count";

export type Insight = {
  id: string;
  kind: InsightKind;
  severity: InsightSeverity;
  score: number;
  title: string;
  description: string;
  recommendation: string;
  goroutine_ids?: number[];
  resource_ids?: string[];
};

export type SmartInsightsResponse = {
  insights: Insight[];
  total: number;
};

export async function fetchSmartInsights(params?: {
  leak_threshold_ns?: string;
  block_threshold_ns?: string;
  contention_min_peak?: number;
  goroutine_count_min?: number;
}): Promise<SmartInsightsResponse> {
  const q = new URLSearchParams();
  if (params?.leak_threshold_ns) q.set("leak_threshold_ns", params.leak_threshold_ns);
  if (params?.block_threshold_ns) q.set("block_threshold_ns", params.block_threshold_ns);
  if (params?.contention_min_peak) q.set("contention_min_peak", String(params.contention_min_peak));
  if (params?.goroutine_count_min) q.set("goroutine_count_min", String(params.goroutine_count_min));
  const query = q.toString();
  const path = `/api/v1/smart-insights${query ? `?${query}` : ""}`;
  return fetchJson<SmartInsightsResponse>(path).catch(() => ({ insights: [], total: 0 }));
}

export type GoroutineGroup = {
  key: string;
  by: string;
  count: number;
  states: Record<string, number>;
  avg_wait_ns: number;
  max_wait_ns: number;
  total_wait_ns: number;
  total_cpu_ns: number;
  goroutine_ids: number[];
};

export type GoroutineGroupsResponse = {
  groups: GoroutineGroup[];
  by: string;
  total: number;
};

export type GroupByField = "function" | "package" | "parent_id" | "label";

export async function fetchGoroutineGroups(params?: {
  by?: GroupByField;
  label_key?: string;
}): Promise<GoroutineGroupsResponse> {
  const q = new URLSearchParams();
  if (params?.by) q.set("by", params.by);
  if (params?.label_key) q.set("label_key", params.label_key);
  const query = q.toString();
  const path = `/api/v1/goroutines/groups${query ? `?${query}` : ""}`;
  return fetchJson<GoroutineGroupsResponse>(path).catch(() => ({
    groups: [],
    by: params?.by ?? "function",
    total: 0,
  }));
}

export type GoroutineDelta = {
  wait_delta_ns: number;
  blocked_delta_ns: number;
  status: "improved" | "regressed" | "unchanged";
};

export type CaptureDiff = {
  goroutine_deltas: Record<string, GoroutineDelta>;
  only_in_baseline: number[];
  only_in_compare: number[];
};

export type CompareResponse = {
  baseline: { goroutines: Goroutine[]; timeline: TimelineSegment[] };
  compare: { goroutines: Goroutine[]; timeline: TimelineSegment[] };
  diff: CaptureDiff;
};

export type RequestGroup = {
  request_id: string;
  url?: string;
  method?: string;
  start_ns: number;
  end_ns: number;
  duration_ns: number;
  goroutine_count: number;
  goroutine_ids: number[];
  state_breakdown: Record<string, number>;
  source: "label" | "stack";
};

export type RequestGroupsResponse = {
  groups: RequestGroup[];
  total: number;
};

export async function fetchRequestGroups(): Promise<RequestGroupsResponse> {
  return fetchJson<RequestGroupsResponse>("/api/v1/requests").catch(() => ({ groups: [], total: 0 }));
}

export async function fetchRequestGoroutines(requestId: string): Promise<Goroutine[]> {
  const res = await fetchJson<{ goroutines: Goroutine[] }>(`/api/v1/requests/${encodeURIComponent(requestId)}/goroutines`).catch(() => ({ goroutines: [] }));
  return res?.goroutines ?? [];
}

/** Compare two .gtrace captures. Returns baseline, compare, and diff. */
export async function fetchCompare(fileA: File, fileB: File): Promise<CompareResponse> {
  const form = new FormData();
  form.append("file_a", fileA);
  form.append("file_b", fileB);
  const res = await fetch("/api/v1/compare", {
    method: "POST",
    body: form,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `compare failed: ${res.status}`);
  }
  return res.json();
}
