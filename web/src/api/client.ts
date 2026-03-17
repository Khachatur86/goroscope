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

const base = "";

async function fetchJson<T>(path: string): Promise<T> {
  const res = await fetch(`${base}${path}`);
  if (!res.ok) {
    throw new Error(`fetch ${path}: ${res.status}`);
  }
  return res.json();
}

export async function fetchCurrentSession(): Promise<Session | null> {
  try {
    return await fetchJson<Session>("/api/v1/session/current");
  } catch {
    return null;
  }
}

export async function fetchGoroutines(params?: {
  state?: string;
  reason?: string;
  search?: string;
  min_wait_ns?: string;
  limit?: number;
  offset?: number;
}): Promise<Goroutine[]> {
  const q = new URLSearchParams();
  if (params?.state && params.state !== "ALL") q.set("state", params.state);
  if (params?.reason) q.set("reason", params.reason);
  if (params?.search) q.set("search", params.search);
  if (params?.min_wait_ns) q.set("min_wait_ns", params.min_wait_ns);
  if (params?.limit) q.set("limit", String(params.limit));
  if (params?.offset) q.set("offset", String(params.offset));
  const query = q.toString();
  const path = `/api/v1/goroutines${query ? `?${query}` : ""}`;
  const data = await fetchJson<Goroutine[] | { goroutines?: Goroutine[] }>(path);
  if (Array.isArray(data)) return data;
  return data?.goroutines ?? [];
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

export async function fetchTimeline(params?: {
  state?: string;
  reason?: string;
  search?: string;
}): Promise<TimelineSegment[]> {
  const q = new URLSearchParams();
  if (params?.state && params.state !== "ALL") q.set("state", params.state);
  if (params?.reason) q.set("reason", params.reason);
  if (params?.search) q.set("search", params.search);
  const query = q.toString();
  const path = `/api/v1/timeline${query ? `?${query}` : ""}`;
  const data = await fetchJson<TimelineSegment[] | null>(path);
  return Array.isArray(data) ? data : [];
}

export async function fetchResourceGraph(): Promise<ResourceEdge[]> {
  return fetchJson<ResourceEdge[]>("/api/v1/resources/graph");
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

export type Insights = { long_blocked_count: number };
export type DeadlockHint = { goroutine_ids: number[]; resource_ids: string[] };

export async function fetchInsights(minWaitNs?: string): Promise<Insights> {
  const q = minWaitNs ? `?min_wait_ns=${minWaitNs}` : "";
  return fetchJson<Insights>(`/api/v1/insights${q}`).catch(() => ({ long_blocked_count: 0 }));
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
