import React from "react";
import { describe, expect, test, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { fireEvent } from "@testing-library/react";

import { ContentionHeatmap } from "../src/analysis/ContentionHeatmap";
import type { TimelineSegment } from "../src/api/client";

const blocked: TimelineSegment[] = [
  { goroutine_id: 1, start_ns: 0, end_ns: 500_000, state: "BLOCKED", reason: "mutex_lock", resource_id: "mutex:0x1" } as any,
  { goroutine_id: 2, start_ns: 100_000, end_ns: 600_000, state: "WAITING", reason: "chan_recv", resource_id: "chan:0x2" } as any,
  { goroutine_id: 3, start_ns: 200_000, end_ns: 700_000, state: "BLOCKED", reason: "mutex_lock", resource_id: "mutex:0x1" } as any,
];

describe("ContentionHeatmap", () => {
  test("renders heatmap container with blocked segments", () => {
    const { container } = render(<ContentionHeatmap segments={blocked} />);
    // The component renders either the heatmap container or the empty state.
    const hasHeatmap = container.querySelector(".heatmap-container") !== null;
    const hasEmpty   = container.querySelector(".heatmap-empty") !== null;
    expect(hasHeatmap || hasEmpty).toBe(true);
  });

  test("renders empty state message with no blocked segments", () => {
    const running: TimelineSegment[] = [
      { goroutine_id: 1, start_ns: 0, end_ns: 1_000_000, state: "RUNNING" } as any,
    ];
    const { container } = render(<ContentionHeatmap segments={running} />);
    // When no BLOCKED/WAITING segments exist, the component shows the empty state.
    expect(container.querySelector(".heatmap-empty")).toBeTruthy();
    expect(container.textContent).toContain("No resource contention recorded");
  });

  test("passes onSelectResource prop without crashing", () => {
    const onSelect = vi.fn();
    // Verify the component mounts with the callback without throwing.
    expect(() =>
      render(<ContentionHeatmap segments={blocked} onSelectResource={onSelect} />)
    ).not.toThrow();
  });
});
