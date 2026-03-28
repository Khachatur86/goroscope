import { test, expect, Page } from "@playwright/test";

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

async function mockAPI(page: Page) {
  await page.route("**/api/v1/session/current", (route) =>
    route.fulfill({ json: session })
  );
  await page.route("**/api/v1/goroutines**", (route) =>
    route.fulfill({ json: { goroutines, sampleInfo: null } })
  );
  await page.route("**/api/v1/insights**", (route) =>
    route.fulfill({
      json: { long_blocked_count: 1, leak_candidates_count: 0 },
    })
  );
  await page.route("**/api/v1/deadlock-hints**", (route) =>
    route.fulfill({ json: { hints: [] } })
  );
  await page.route("**/api/v1/resources/graph**", (route) =>
    route.fulfill({ json: [] })
  );
  await page.route("**/api/v1/timeline**", (route) =>
    route.fulfill({
      json: [
        { goroutine_id: 1, start_ns: 0, end_ns: 1_000_000, state: "RUNNING" },
        {
          goroutine_id: 2,
          start_ns: 0,
          end_ns: 2_000_000,
          state: "BLOCKED",
          reason: "chan_send",
          resource_id: "chan:0x1",
        },
      ],
    })
  );
  await page.route("**/api/v1/processor-timeline**", (route) =>
    route.fulfill({ json: [] })
  );
  await page.route("**/api/v1/smart-insights**", (route) =>
    route.fulfill({ json: [] })
  );
  await page.route("**/api/v1/stream**", (route) =>
    route.fulfill({ status: 200, body: "" })
  );
  await page.route("**/healthz**", (route) =>
    route.fulfill({ json: { status: "ok" } })
  );
}

test.describe("App smoke", () => {
  test("shows goroutine count after load", async ({ page }) => {
    await mockAPI(page);
    await page.goto("/");

    const label = page.locator(".goroutine-count-label");
    await expect(label).toContainText("goroutines");
  });

  test("renders timeline canvas", async ({ page }) => {
    await mockAPI(page);
    await page.goto("/");

    await expect(page.locator("canvas.timeline-canvas-axis")).toBeVisible();
  });

  test("blocked goroutine appears in list", async ({ page }) => {
    await mockAPI(page);
    await page.goto("/");

    // BLOCKED goroutine should appear as a row/badge in the goroutine list
    await expect(page.locator("text=BLOCKED").first()).toBeVisible();
  });
});
