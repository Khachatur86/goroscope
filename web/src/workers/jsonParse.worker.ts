/**
 * Web Worker: fetch a URL and parse the JSON body off the main thread.
 * Message in:  { id: number; url: string }
 * Message out: { id: number; data: unknown } | { id: number; error: string }
 */

self.onmessage = async (ev: MessageEvent<{ id: number; url: string }>) => {
  const { id, url } = ev.data;
  try {
    const res = await fetch(url);
    if (!res.ok) {
      self.postMessage({ id, error: `fetch ${url}: ${res.status}` });
      return;
    }
    const data = await res.json();
    self.postMessage({ id, data });
  } catch (err) {
    self.postMessage({ id, error: String(err) });
  }
};
