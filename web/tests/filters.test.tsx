import React from "react";
import { describe, expect, test, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { Filters } from "../src/filters/Filters";

const defaultFilters = {
  state: "ALL",
  reason: "",
  resource: "",
  search: "",
  minWaitNs: "",
  sortMode: "wait_desc",
  showLeakOnly: false,
  hideRuntime: false,
};

describe("Filters", () => {
  test("renders preset buttons", () => {
    render(
      <Filters
        filters={defaultFilters}
        onFiltersChange={vi.fn()}
        onJumpTo={vi.fn()}
      />
    );

    expect(screen.getByRole("button", { name: /all/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /blocked/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /channels/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /mutex/i })).toBeTruthy();
  });

  test("Blocked preset calls onFiltersChange with state=BLOCKED", () => {
    const onChange = vi.fn();
    render(
      <Filters
        filters={defaultFilters}
        onFiltersChange={onChange}
        onJumpTo={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: /blocked/i }));
    expect(onChange).toHaveBeenCalledOnce();
    const arg = onChange.mock.calls[0][0];
    expect(arg.state).toBe("BLOCKED");
  });

  test("All preset resets state and reason", () => {
    const onChange = vi.fn();
    render(
      <Filters
        filters={{ ...defaultFilters, state: "BLOCKED" }}
        onFiltersChange={onChange}
        onJumpTo={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: /^all$/i }));
    const arg = onChange.mock.calls[0][0];
    expect(arg.state).toBe("ALL");
    expect(arg.reason).toBe("");
  });

  test("search input change propagates", () => {
    const onChange = vi.fn();
    render(
      <Filters
        filters={defaultFilters}
        onFiltersChange={onChange}
        onJumpTo={vi.fn()}
      />
    );

    // The search input has placeholder "id, function, reason" and type="search".
    const searchInput = screen.getByRole("searchbox");
    fireEvent.change(searchInput, { target: { value: "worker" } });
    expect(onChange).toHaveBeenCalled();
    expect(onChange.mock.calls[0][0].search).toBe("worker");
  });

  test("Jump-to fires onJumpTo on Enter with a valid number", () => {
    const onJumpTo = vi.fn();
    render(
      <Filters
        filters={defaultFilters}
        onFiltersChange={vi.fn()}
        onJumpTo={onJumpTo}
      />
    );

    const jumpInput = screen.getByPlaceholderText(/goroutine id/i);
    fireEvent.change(jumpInput, { target: { value: "42" } });
    fireEvent.keyDown(jumpInput, { key: "Enter" });
    expect(onJumpTo).toHaveBeenCalledWith(42);
  });

  test("label dropdown shows supplied pairs", () => {
    render(
      <Filters
        filters={defaultFilters}
        onFiltersChange={vi.fn()}
        onJumpTo={vi.fn()}
        distinctLabelPairs={["service=api", "service=worker"]}
      />
    );

    // The label select should contain the pair options.
    expect(screen.getByText("service=api")).toBeTruthy();
    expect(screen.getByText("service=worker")).toBeTruthy();
  });
});
