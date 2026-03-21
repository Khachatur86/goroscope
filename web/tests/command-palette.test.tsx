import React from "react";
import { describe, expect, test, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { CommandPalette } from "../src/palette/CommandPalette";
import type { Command } from "../src/palette/CommandPalette";

const commands: Command[] = [
  { id: "tab-timeline", label: "Timeline", group: "Analysis tabs", action: vi.fn() },
  { id: "tab-heatmap", label: "Heatmap", group: "Analysis tabs", action: vi.fn() },
  { id: "save-gif", label: "Save GIF", group: "Timeline", keywords: ["export", "gif"], action: vi.fn() },
  { id: "refresh", label: "Refresh", group: "Data", action: vi.fn() },
];

const goroutines: any[] = [
  { goroutine_id: 1, state: "RUNNING", reason: "", resource_id: "", labels: {} },
  { goroutine_id: 2, state: "BLOCKED", reason: "chan_send", resource_id: "chan:0x1", labels: {} },
];

describe("CommandPalette", () => {
  test("renders nothing when closed", () => {
    const { container } = render(
      <CommandPalette
        open={false}
        onClose={vi.fn()}
        commands={commands}
        goroutines={[]}
        onSelectGoroutine={vi.fn()}
      />
    );
    expect(container.querySelector(".palette-backdrop")).toBeNull();
  });

  test("renders input and entries when open", () => {
    render(
      <CommandPalette
        open={true}
        onClose={vi.fn()}
        commands={commands}
        goroutines={[]}
        onSelectGoroutine={vi.fn()}
      />
    );
    expect(screen.getByPlaceholderText(/search commands/i)).toBeTruthy();
    // "Timeline" appears as both a command label and a group header — use getAllByText.
    expect(screen.getAllByText("Timeline").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("Heatmap")).toBeTruthy();
  });

  test("filters entries by fuzzy query", () => {
    render(
      <CommandPalette
        open={true}
        onClose={vi.fn()}
        commands={commands}
        goroutines={[]}
        onSelectGoroutine={vi.fn()}
      />
    );

    const input = screen.getByPlaceholderText(/search commands/i);
    fireEvent.change(input, { target: { value: "gif" } });

    // "Save GIF" matches via keyword
    expect(screen.getByText("Save GIF")).toBeTruthy();
    // "Heatmap" should not appear when query is "gif"
    expect(screen.queryByText("Heatmap")).toBeNull();
  });

  test("calls onClose on Escape", () => {
    const onClose = vi.fn();
    render(
      <CommandPalette
        open={true}
        onClose={onClose}
        commands={commands}
        goroutines={[]}
        onSelectGoroutine={vi.fn()}
      />
    );

    // The keyDown handler is on the input element, not document.
    const input = screen.getByPlaceholderText(/search commands/i);
    fireEvent.keyDown(input, { key: "Escape" });
    expect(onClose).toHaveBeenCalledOnce();
  });

  test("executes command action and closes on Enter", () => {
    const action = vi.fn();
    const onClose = vi.fn();
    const cmds: Command[] = [
      { id: "only", label: "Only Command", group: "Test", action },
    ];

    render(
      <CommandPalette
        open={true}
        onClose={onClose}
        commands={cmds}
        goroutines={[]}
        onSelectGoroutine={vi.fn()}
      />
    );

    const input = screen.getByPlaceholderText(/search commands/i);
    fireEvent.keyDown(input, { key: "Enter" });

    expect(action).toHaveBeenCalledOnce();
    expect(onClose).toHaveBeenCalledOnce();
  });

  test("shows goroutine entries alongside commands", () => {
    render(
      <CommandPalette
        open={true}
        onClose={vi.fn()}
        commands={[]}
        goroutines={goroutines}
        onSelectGoroutine={vi.fn()}
      />
    );

    // Goroutine entries are labelled "G<id>" by the palette.
    expect(screen.getByText("G1")).toBeTruthy();
    expect(screen.getByText("G2")).toBeTruthy();
  });

  test("calls onSelectGoroutine on Enter when cursor is on a goroutine entry", () => {
    const onSelect = vi.fn();
    render(
      <CommandPalette
        open={true}
        onClose={vi.fn()}
        commands={[]}
        goroutines={goroutines}
        onSelectGoroutine={onSelect}
      />
    );

    // Cursor starts at 0 → first goroutine entry (G1). Enter confirms it.
    const input = screen.getByPlaceholderText(/search commands/i);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onSelect).toHaveBeenCalledWith(1);
  });
});
