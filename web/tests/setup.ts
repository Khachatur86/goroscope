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

