/**
 * Fetches a URL and parses JSON in a Web Worker to keep the main thread free.
 * Falls back to a regular fetch if workers are not available.
 */

let worker: Worker | null = null;
let nextId = 1;
const pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();

function getWorker(): Worker | null {
  if (worker) return worker;
  try {
    worker = new Worker(new URL("./jsonParse.worker.ts", import.meta.url), { type: "module" });
    worker.onmessage = (ev: MessageEvent<{ id: number; data?: unknown; error?: string }>) => {
      const { id, data, error } = ev.data;
      const p = pending.get(id);
      if (!p) return;
      pending.delete(id);
      if (error !== undefined) {
        p.reject(new Error(error));
      } else {
        p.resolve(data);
      }
    };
    worker.onerror = (ev) => {
      // On worker crash, reject all pending and reset so we recreate next time.
      const msg = ev.message ?? "worker error";
      for (const p of pending.values()) p.reject(new Error(msg));
      pending.clear();
      worker = null;
    };
    return worker;
  } catch {
    return null;
  }
}

export async function fetchJsonViaWorker<T>(url: string): Promise<T> {
  const w = getWorker();
  if (!w) {
    // Fallback: parse on main thread.
    const res = await fetch(url);
    if (!res.ok) throw new Error(`fetch ${url}: ${res.status}`);
    return res.json() as Promise<T>;
  }
  return new Promise<T>((resolve, reject) => {
    const id = nextId++;
    pending.set(id, {
      resolve: (v) => resolve(v as T),
      reject,
    });
    w.postMessage({ id, url });
  });
}
