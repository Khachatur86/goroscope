import React from "react";
import { describe, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";

import { App } from "../src/app";
import { CompareView } from "../src/compare/CompareView";
import * as api from "../src/api/client";

// Module-level defaults — every api function returns safe empty data so that
// effects firing after a test (intervals, in-flight promises) never hit the
// real network even after vi.restoreAllMocks() has been called.
vi.mock("../src/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof api>();
  return {
    ...actual,
    fetchCurrentSession:    vi.fn().mockResolvedValue(null),
    fetchGoroutines:        vi.fn().mockResolvedValue({ goroutines: [], sampleInfo: null }),
    fetchResourceGraph:     vi.fn().mockResolvedValue([]),
    fetchResourceContention:vi.fn().mockResolvedValue([]),
    fetchInsights:          vi.fn().mockResolvedValue({ long_blocked_count: 0, leak_candidates_count: 0 }),
    fetchDeadlockHints:     vi.fn().mockResolvedValue({ hints: [] }),
    fetchTimeline:          vi.fn().mockResolvedValue([]),
    fetchProcessorTimeline: vi.fn().mockResolvedValue([]),
    fetchSmartInsights:     vi.fn().mockResolvedValue([]),
    fetchCompare:           vi.fn().mockResolvedValue({}),
  };
});

// Prevent html2canvas (dynamic import) from loading ESM-only dependencies during tests.
vi.mock("html2canvas", () => ({
  default: vi.fn(async () => document.createElement("canvas")),
}));

const session = {
  id: "sess-1",
  name: "smoke",
  target: "worker-pool",
  status: "running",
  started_at: new Date().toISOString(),
};

const goroutines = [
  {
    goroutine_id: 1,
    state: "RUNNING",
    reason: "",
    labels: { function: "main.main" },
    wait_ns: 0,
  },
  {
    goroutine_id: 2,
    state: "BLOCKED",
    reason: "chan_send",
    resource_id: "chan:0x1",
    labels: { function: "worker" },
    wait_ns: 45_000_000_000,
    parent_id: 1,
  },
];

const timelineSegments = [
  { goroutine_id: 1, start_ns: 0, end_ns: 1_000_000, state: "RUNNING" },
  {
    goroutine_id: 2,
    start_ns: 0,
    end_ns: 2_000_000,
    state: "BLOCKED",
    reason: "chan_send",
    resource_id: "chan:0x1",
  },
];

describe("frontend smoke tests", () => {
  afterEach(() => {
    // Unmount components before resetting mocks so in-flight effects don't
    // call unmocked api functions and cause unhandled rejection noise.
    cleanup();
    vi.resetAllMocks();
    // Restore module-level defaults after each test.
    vi.mocked(api.fetchCurrentSession).mockResolvedValue(null as any);
    vi.mocked(api.fetchGoroutines).mockResolvedValue({ goroutines: [], sampleInfo: null } as any);
    vi.mocked(api.fetchResourceGraph).mockResolvedValue([] as any);
    vi.mocked(api.fetchResourceContention).mockResolvedValue([] as any);
    vi.mocked(api.fetchInsights).mockResolvedValue({ long_blocked_count: 0, leak_candidates_count: 0 } as any);
    vi.mocked(api.fetchDeadlockHints).mockResolvedValue({ hints: [] } as any);
    vi.mocked(api.fetchTimeline).mockResolvedValue([] as any);
    vi.mocked(api.fetchProcessorTimeline).mockResolvedValue([] as any);
    vi.mocked(api.fetchSmartInsights).mockResolvedValue([] as any);
    vi.mocked(api.fetchCompare).mockResolvedValue({} as any);
  });

  test("App renders goroutine count and timeline canvas", async () => {
    vi.mocked(api.fetchCurrentSession).mockResolvedValue(session as any);
    vi.mocked(api.fetchGoroutines).mockResolvedValue({ goroutines, sampleInfo: null } as any);
    vi.mocked(api.fetchInsights).mockResolvedValue({ long_blocked_count: 1, leak_candidates_count: 0 } as any);
    vi.mocked(api.fetchTimeline).mockResolvedValue(timelineSegments as any);

    const { container } = render(<App />);

    await waitFor(() => {
      const label = container.querySelector(".goroutine-count-label");
      expect(label?.textContent?.includes("goroutines")).toBe(true);
    });

    // Timeline should not stay in "placeholder" mode once segments are loaded.
    await waitFor(() => {
      expect(container.querySelector("canvas.timeline-canvas-axis")).toBeTruthy();
    });
  });

  test("CompareView can run compare flow (files -> fetched data -> rendered panels)", async () => {
    const compareResp: any = {
      baseline: {
        goroutines: [goroutines[0]],
        timeline: [{ goroutine_id: 1, start_ns: 0, end_ns: 1_000_000, state: "RUNNING" }],
      },
      compare: {
        goroutines: [goroutines[0], goroutines[1]],
        timeline: [
          { goroutine_id: 1, start_ns: 0, end_ns: 1_500_000, state: "RUNNING" },
          {
            goroutine_id: 2,
            start_ns: 0,
            end_ns: 2_500_000,
            state: "BLOCKED",
            reason: "chan_send",
            resource_id: "chan:0x1",
          },
        ],
      },
      diff: {
        goroutine_deltas: {
          "1": { wait_delta_ns: -10_000_000, blocked_delta_ns: 0, status: "improved" },
          "2": { wait_delta_ns: 20_000_000, blocked_delta_ns: 0, status: "regressed" },
        },
        only_in_baseline: [],
        only_in_compare: [],
      },
    };

    vi.mocked(api.fetchCompare).mockResolvedValue(compareResp);
    const onClose = vi.fn();

    render(<CompareView onClose={onClose} />);

    const inputA = screen.getByLabelText("Baseline (before)") as HTMLInputElement;
    const inputB = screen.getByLabelText("Compare (after)") as HTMLInputElement;

    fireEvent.change(inputA, {
      target: {
        files: [new File(["a"], "before.gtrace", { type: "application/octet-stream" })],
      },
    });
    fireEvent.change(inputB, {
      target: {
        files: [new File(["b"], "after.gtrace", { type: "application/octet-stream" })],
      },
    });

    fireEvent.click(screen.getByRole("button", { name: "Compare" }));

    // After fetch resolves, both panels should render.
    await screen.findByText("Baseline");
    await screen.findByText("Compare");
  });
});

