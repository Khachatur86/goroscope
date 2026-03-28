import { vi } from "vitest";

// --- Canvas mocks ---
// The timeline renders multiple <canvas> elements and expects a 2D context.
// In jsdom, canvas APIs are not implemented, so we provide a minimal no-op surface.
const ctx2dMock: Partial<CanvasRenderingContext2D> = {
  setTransform: vi.fn(),
  scale: vi.fn(),
  clearRect: vi.fn(),
  fillRect: vi.fn(),
  strokeRect: vi.fn(),
  beginPath: vi.fn(),
  moveTo: vi.fn(),
  lineTo: vi.fn(),
  arc: vi.fn(),
  closePath: vi.fn(),
  stroke: vi.fn(),
  fill: vi.fn(),
  fillText: vi.fn(),
  strokeText: vi.fn(),
  measureText: vi.fn(() => ({ width: 100 })),
  save: vi.fn(),
  restore: vi.fn(),
  clip: vi.fn(),
  rect: vi.fn(),
  // These are assigned to from components; keep them defined to avoid TS/Runtime surprises.
  fillStyle: "#000",
  strokeStyle: "#000",
  font: "10px sans-serif",
  lineWidth: 1,
  globalAlpha: 1,
  textBaseline: "alphabetic",
  textAlign: "start",
};

Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  value: () => ctx2dMock,
});

// Some code paths read bounding rects; keep it stable for deterministic layout math.
Object.defineProperty(HTMLCanvasElement.prototype, "getBoundingClientRect", {
  value: () => ({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    width: 300,
    height: 150,
    right: 300,
    bottom: 150,
    toJSON: () => ({}),
  }),
});

// --- ResizeObserver mock ---
class ResizeObserverMock {
  observe() {
    // no-op
  }
  disconnect() {
    // no-op
  }
  unobserve() {
    // no-op
  }
}

// @ts-expect-error: jsdom doesn't ship ResizeObserver in all versions.
globalThis.ResizeObserver = ResizeObserverMock;

// --- EventSource mock ---
// App creates EventSource("/api/v1/stream") and attaches listeners; in smoke tests we don't want real networking.
class EventSourceMock {
  url: string;
  onerror: ((...args: any[]) => void) | null = null;

  constructor(url: string) {
    this.url = url;
  }

  addEventListener() {
    // no-op
  }

  close() {
    // no-op
  }
}

// @ts-expect-error: EventSource exists in browsers, not in jsdom.
globalThis.EventSource = EventSourceMock;

// --- Worker mock ---
// fetchViaWorker.ts tries to spin up a Worker to parse JSON off-thread.
// In tests there is no server to load the worker module from, so stub Worker
// to return a non-functional instance — getWorker() will catch the missing
// message handler and fall back to a plain fetch(), which vi.spyOn intercepts.
class WorkerMock {
  constructor(_url: string | URL) {}
  postMessage() {}
  addEventListener() {}
  terminate() {}
  set onmessage(_h: unknown) {}
  set onerror(_h: unknown) {}
}

// @ts-expect-error: Worker is not available in happy-dom.
globalThis.Worker = WorkerMock;

// --- Global fetch fallback ---
// App mounts timers and SSE listeners that outlive individual tests and fire
// fetch() calls against unmocked endpoints (e.g. /api/v1/resources/graph).
// Intercept every fetch at the global level and return an empty-200 response
// for any URL that a test hasn't already stubbed via vi.spyOn(api, ...).
// Tests that use vi.spyOn on specific api functions still take priority because
// those spies operate at the module level, before fetch is ever called.
// Intercept every fetch at the global level and return an empty-200 response
// for any URL that a test hasn't already stubbed via vi.spyOn(api, ...).
//
// We intentionally do NOT fall through to _originalFetch for relative paths:
// in happy-dom the base URL is http://localhost:3000, so a relative fetch like
// "/api/v1/stream" would become an absolute request to :3000 that isn't
// running in CI, causing spurious ECONNREFUSED noise in the test output.
globalThis.fetch = async (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> => {
  return new Response(JSON.stringify(null), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
};
