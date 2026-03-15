const state = {
  session: null,
  sessions: [],
  goroutines: [],
  timeline: [],
  resources: [],
  processorTimeline: [],
  insights: null,
  selectedId: null,
  selectedGoroutine: null,
  initialGoroutineFromURL: null,
  relatedFocus: false,
  search: "",
  stateFilter: "ALL",
  reasonFilter: "",
  minWaitNs: "",
  sortMode: "SUSPICIOUS",
  // "lanes" = classic lane view; "heatmap" = pixel heatmap + GMP strip.
  viewMode: "lanes",
};

// timelineCache stores the last-rendered timeline bounds so the mousemove
// hit-test and wheel-zoom handler can reuse them without recomputing.
const timelineCache = {
  goroutines: [],
  timeline: [],
  // fullMinStart / fullSpan cover the entire trace (zoom-independent).
  fullMinStart: 0,
  fullSpan: 1,
  // minStart / span reflect the current visible window after pan/zoom.
  minStart: 0,
  span: 1,
  width: 0,
  metrics: null,
};

// timelineView holds the zoom/pan state.  zoomLevel=1 means fully zoomed out.
// panOffsetNS is the offset from fullMinStart to the left edge of the visible
// window, in nanoseconds.
const timelineView = {
  zoomLevel: 1,
  panOffsetNS: 0,
  isDragging: false,
  dragStartX: 0,
  dragStartPanNS: 0,
  hasDragged: false,
};

// timelineHighlight keeps transient UI-only highlight state for the canvas.
const timelineHighlight = {
  hoveredGoroutineID: null,
  hoveredSegmentKey: "",
};

const derivedCache = {
  diagnosticsByID: new Map(),
  resourceEdgesByID: null,
  timelineSegmentsByID: null,
};

const colors = {
  RUNNING:  "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING:  "#f59e0b",
  BLOCKED:  "#f43f5e",
  SYSCALL:  "#4da6ff",
  DONE:     "#4b5563",
};

const timelineStates = ["RUNNING", "RUNNABLE", "WAITING", "BLOCKED", "SYSCALL", "DONE"];
const stallStates = new Set(["WAITING", "BLOCKED", "SYSCALL"]);
const offCPUStates = new Set(["RUNNABLE", "WAITING", "BLOCKED", "SYSCALL"]);

const LANE_ITEM_HEIGHT = 88;
const LANE_VIRTUAL_THRESHOLD = 100;
const LANE_LIST_VIEWPORT_HEIGHT = 500;

const laneListState = {
  scrollTop: 0,
};

const elements = {
  refreshButton: document.getElementById("refresh-button"),
  sessionName: document.getElementById("session-name"),
  sessionTarget: document.getElementById("session-target"),
  sessionStatus: document.getElementById("session-status"),
  sessionStarted: document.getElementById("session-started"),
  goroutineCount: document.getElementById("goroutine-count"),
  blockedCount: document.getElementById("blocked-count"),
  longBlockedCount: document.getElementById("long-blocked-count"),
  longBlockedCard: document.getElementById("long-blocked-card"),
  jumpToInput: document.getElementById("jump-to-input"),
  searchInput: document.getElementById("search-input"),
  stateFilter: document.getElementById("state-filter"),
  reasonFilter: document.getElementById("reason-filter"),
  minWaitFilter: document.getElementById("min-wait-filter"),
  sortMode: document.getElementById("sort-mode"),
  lanePriority: document.getElementById("lane-priority"),
  goroutineList: document.getElementById("goroutine-list"),
  timelineCanvas: document.getElementById("timeline-canvas"),
  timelineContext: document.getElementById("timeline-context"),
  timelineCursor: document.getElementById("timeline-cursor"),
  timelineRange: document.getElementById("timeline-range"),
  focusRelatedButton: document.getElementById("focus-related-button"),
  resetZoomButton: document.getElementById("reset-zoom-button"),
  inspector: document.getElementById("inspector"),
  resourceList: document.getElementById("resource-list"),
  resourceGraphToggle: document.getElementById("resource-graph-toggle"),
  resourceGraphTable: document.getElementById("resource-graph-table"),
  resourceGraphCount: document.getElementById("resource-graph-count"),
  tooltip: document.getElementById("timeline-tooltip"),
  sessionHistory: document.getElementById("session-history"),
  minimapCanvas: document.getElementById("minimap-canvas"),
  heatmapCanvas: document.getElementById("heatmap-canvas"),
  viewToggle: document.getElementById("view-toggle"),
};

const canvasContext = elements.timelineCanvas.getContext("2d");
const minimapContext = elements.minimapCanvas ? elements.minimapCanvas.getContext("2d") : null;
const heatmapContext = elements.heatmapCanvas ? elements.heatmapCanvas.getContext("2d") : null;

// ─── Control event listeners ───────────────────────────────────────────────

elements.refreshButton.addEventListener("click", () => {
  loadData();
});

if (elements.jumpToInput) {
  elements.jumpToInput.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      const id = parseInt(elements.jumpToInput.value, 10);
      if (Number.isFinite(id) && id > 0) {
        jumpToGoroutine(id);
        elements.jumpToInput.value = "";
      }
    }
  });
}

document.addEventListener("keydown", (event) => {
  if ((event.ctrlKey || event.metaKey) && event.key === "g") {
    event.preventDefault();
    elements.jumpToInput?.focus();
  }
});

elements.searchInput.addEventListener("input", (event) => {
  state.search = event.target.value.trim().toLowerCase();
  render();
});

elements.stateFilter.addEventListener("change", (event) => {
  state.stateFilter = event.target.value;
  ensureSelection();
  render();
});

if (elements.reasonFilter) {
  elements.reasonFilter.addEventListener("change", (event) => {
    state.reasonFilter = event.target.value;
    loadData();
  });
}

if (elements.minWaitFilter) {
  elements.minWaitFilter.addEventListener("change", (event) => {
    state.minWaitNs = event.target.value;
    loadData();
  });
}

if (elements.resourceGraphToggle && elements.resourceGraphTable) {
  elements.resourceGraphToggle.addEventListener("click", () => {
    const expanded = elements.resourceGraphToggle.getAttribute("aria-expanded") === "true";
    elements.resourceGraphToggle.setAttribute("aria-expanded", String(!expanded));
    elements.resourceGraphTable.hidden = !expanded;
  });
}

if (elements.longBlockedCard) {
  elements.longBlockedCard.addEventListener("click", () => {
    state.minWaitNs = "1000000000";
    if (elements.minWaitFilter) {
      elements.minWaitFilter.value = state.minWaitNs;
    }
    loadData();
  });
  elements.longBlockedCard.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      elements.longBlockedCard.click();
    }
  });
}

if (elements.sortMode) {
  elements.sortMode.addEventListener("change", (event) => {
    state.sortMode = event.target.value;
    ensureSelection();
    render();
  });
}

if (elements.resetZoomButton) {
  elements.resetZoomButton.addEventListener("click", () => {
    timelineView.zoomLevel = 1;
    timelineView.panOffsetNS = 0;
    renderCurrentView();
  });
}

if (elements.focusRelatedButton) {
  elements.focusRelatedButton.addEventListener("click", () => {
    if (!state.selectedId) {
      return;
    }
    state.relatedFocus = !state.relatedFocus;
    render();
  });
}

if (elements.viewToggle) {
  elements.viewToggle.addEventListener("click", () => {
    state.viewMode = state.viewMode === "lanes" ? "heatmap" : "lanes";
    elements.viewToggle.textContent = state.viewMode === "heatmap" ? "≡ Lanes" : "⊞ Heatmap";
    elements.viewToggle.setAttribute("aria-pressed", String(state.viewMode === "heatmap"));
    // Show the right canvas and hide the other.
    if (elements.timelineCanvas) elements.timelineCanvas.hidden = state.viewMode === "heatmap";
    if (elements.minimapCanvas) elements.minimapCanvas.hidden = true; // minimap only for lane view
    if (elements.heatmapCanvas) elements.heatmapCanvas.hidden = state.viewMode !== "heatmap";
    // Toggle a class on the stage so CSS can remove the stage dark background
    // when the heatmap canvas itself provides the dark fill.
    const stage = elements.heatmapCanvas?.closest(".timeline-stage");
    if (stage) stage.classList.toggle("heatmap-active", state.viewMode === "heatmap");
    renderCurrentView();
  });
}

// ─── Timeline canvas event listeners ──────────────────────────────────────

// Wheel zoom: zoom in/out centered on the cursor position so the nanosecond
// value under the cursor stays fixed.
elements.timelineCanvas.addEventListener("wheel", (event) => {
  event.preventDefault();

  const { metrics, fullMinStart, fullSpan, width } = timelineCache;
  if (!metrics || fullSpan <= 1) {
    return;
  }

  const rect = elements.timelineCanvas.getBoundingClientRect();
  const canvasX = event.clientX - rect.left;
  const { left: plotLeft, innerWidth } = getTimelinePlotBounds(width, metrics);

  // Fraction [0, 1] of the inner drawing area where the cursor sits.
  const fx = Math.max(0, Math.min(1, (canvasX - plotLeft) / innerWidth));

  // NS value under the cursor in the current visible window.
  const currentVisibleSpan = fullSpan / timelineView.zoomLevel;
  const cursorNS = timelineView.panOffsetNS + fx * currentVisibleSpan;

  const zoomFactor = event.deltaY < 0 ? 1.3 : 1 / 1.3;
  const newZoomLevel = Math.max(1, Math.min(500, timelineView.zoomLevel * zoomFactor));
  const newVisibleSpan = fullSpan / newZoomLevel;

  // Keep the NS under the cursor anchored to the same screen position.
  let newPanNS = cursorNS - fx * newVisibleSpan;
  newPanNS = Math.max(0, Math.min(fullSpan - newVisibleSpan, newPanNS));

  timelineView.zoomLevel = newZoomLevel;
  timelineView.panOffsetNS = newPanNS;

  renderTimeline();
}, { passive: false });

// Mousedown starts a potential drag.
elements.timelineCanvas.addEventListener("mousedown", (event) => {
  if (event.button !== 0) {
    return;
  }

  clearTimelineHighlight();
  hideTimelineCursor();
  renderTimelineContext();
  timelineView.isDragging = true;
  timelineView.dragStartX = event.clientX;
  timelineView.dragStartPanNS = timelineView.panOffsetNS;
  timelineView.hasDragged = false;

  if (timelineView.zoomLevel > 1) {
    elements.timelineCanvas.style.cursor = "grabbing";
  }

  event.preventDefault();
});

// Mouseup: if a drag happened, suppress the click; otherwise do row selection.
elements.timelineCanvas.addEventListener("mouseup", (event) => {
  if (event.button !== 0) {
    return;
  }

  const wasDragged = timelineView.hasDragged;
  timelineView.isDragging = false;
  timelineView.hasDragged = false;

  // Restore cursor after drag.
  elements.timelineCanvas.style.cursor = timelineView.zoomLevel > 1 ? "grab" : "";

  if (wasDragged) {
    return;
  }

  // Treat this as a goroutine-row click.
  const metrics = getTimelineMetrics();
  const rect = elements.timelineCanvas.getBoundingClientRect();
  const y = event.clientY - rect.top;
  if (y <= metrics.axisHeight) {
    return;
  }

  const rowIndex = Math.floor((y - metrics.axisHeight) / metrics.rowHeight);
  const rows = getFilteredGoroutines();
  if (rowIndex >= 0 && rowIndex < rows.length) {
    selectGoroutine(rows[rowIndex].goroutine_id);
  }
});

// Mousemove: handle drag panning and tooltip.
elements.timelineCanvas.addEventListener("mousemove", (event) => {
  if (timelineView.isDragging && timelineView.zoomLevel > 1) {
    const dx = event.clientX - timelineView.dragStartX;

    if (Math.abs(dx) > 3) {
      timelineView.hasDragged = true;
    }

    const { fullSpan, width, metrics } = timelineCache;
    if (metrics && width > 0) {
      const { innerWidth } = getTimelinePlotBounds(width, metrics);
      const visibleSpan = fullSpan / timelineView.zoomLevel;
      const dNS = -(dx / innerWidth) * visibleSpan;
      let newPan = timelineView.dragStartPanNS + dNS;
      newPan = Math.max(0, Math.min(fullSpan - visibleSpan, newPan));
      timelineView.panOffsetNS = newPan;
      renderTimeline();
    }

    elements.timelineCanvas.style.cursor = "grabbing";
    hideTooltip();
    hideTimelineCursor();
    renderTimelineContext();
    return;
  }

  const rect = elements.timelineCanvas.getBoundingClientRect();
  const canvasX = event.clientX - rect.left;
  const canvasY = event.clientY - rect.top;
  const hoveredRow = getTimelineRowAt(canvasY);
  const hit = getSegmentAt(canvasX, canvasY);
  const hoveredNS = getHoveredTimelineNS(canvasX);
  setTimelineHighlight(hoveredRow ? hoveredRow.goroutine_id : null, hit ? buildSegmentKey(hit.segment) : "");
  updateTimelineCursor(canvasX);
  renderTimelineContext({ hit, hoveredRow, hoveredNS });

  if (hit) {
    elements.timelineCanvas.style.cursor = timelineView.zoomLevel > 1 ? "grab" : "pointer";
    showTooltip(hit, event.clientX, event.clientY);
  } else if (hoveredRow) {
    elements.timelineCanvas.style.cursor = "pointer";
    hideTooltip();
  } else {
    elements.timelineCanvas.style.cursor = timelineView.zoomLevel > 1 ? "grab" : "";
    hideTooltip();
  }
});

elements.timelineCanvas.addEventListener("mouseleave", () => {
  timelineView.isDragging = false;
  timelineView.hasDragged = false;
  elements.timelineCanvas.style.cursor = "";
  clearTimelineHighlight();
  hideTimelineCursor();
  renderTimelineContext();
  hideTooltip();
});

let _renderBusy = false;

// Only re-render when the actual viewport WIDTH changes (user resizing the
// browser window). We intentionally do NOT watch for height changes or use
// ResizeObserver on the stage element — canvas style.height changes trigger
// those in Chrome, creating a feedback loop. window.innerWidth is unaffected
// by our own canvas dimension writes, so this never loops.
let _lastViewportWidth = window.innerWidth;
window.addEventListener("resize", () => {
  const vw = window.innerWidth;
  if (vw !== _lastViewportWidth) {
    _lastViewportWidth = vw;
    if (!_renderBusy) renderCurrentView();
  }
});

// ─── Minimap interaction ───────────────────────────────────────────────────

// minimapDragging tracks whether the user is currently dragging on the minimap.
let minimapDragging = false;

if (elements.minimapCanvas) {
  elements.minimapCanvas.addEventListener("mousedown", (event) => {
    if (event.button !== 0) {
      return;
    }
    minimapDragging = true;
    seekMinimapToEvent(event);
    event.preventDefault();
  });

  elements.minimapCanvas.addEventListener("mousemove", (event) => {
    if (minimapDragging) {
      seekMinimapToEvent(event);
    }
  });
}

// Global mouseup so drag is released even when cursor leaves the minimap.
window.addEventListener("mouseup", () => {
  minimapDragging = false;
});

// ─── Data loading ──────────────────────────────────────────────────────────

function buildGoroutinesURL() {
  const params = new URLSearchParams();
  if (state.stateFilter && state.stateFilter !== "ALL") {
    params.set("state", state.stateFilter);
  }
  if (state.reasonFilter) {
    params.set("reason", state.reasonFilter);
  }
  if (state.minWaitNs) {
    params.set("min_wait_ns", state.minWaitNs);
  }
  const qs = params.toString();
  return qs ? `/api/v1/goroutines?${qs}` : "/api/v1/goroutines";
}

async function loadData() {
  try {
    const goroutinesURL = buildGoroutinesURL();
    const [session, goroutinesResp, timeline, resources, sessions, processorTimeline, insights] = await Promise.all([
      fetchJSON("/api/v1/session/current"),
      fetchJSON(goroutinesURL),
      fetchJSON("/api/v1/timeline"),
      fetchJSON("/api/v1/resources/graph"),
      fetchJSON("/api/v1/sessions"),
      fetchJSON("/api/v1/processor-timeline").catch(() => []),
      fetchJSON("/api/v1/insights").catch(() => ({ long_blocked_count: 0 })),
    ]);

    const goroutines = Array.isArray(goroutinesResp)
      ? goroutinesResp
      : (goroutinesResp.goroutines ?? []);

    state.session = session;
    state.goroutines = goroutines;
    state.timeline = timeline;
    state.resources = resources;
    state.sessions = Array.isArray(sessions) ? sessions : [];
    state.processorTimeline = Array.isArray(processorTimeline) ? processorTimeline : [];
    state.insights = insights;
    resetDerivedCaches();

    parseGoroutineFromURL();
    ensureSelection();
    await hydrateSelectedGoroutine();
    render();
    updateURLForSelection();
  } catch (error) {
    renderError(error instanceof Error ? error.message : String(error));
  }
}

async function hydrateSelectedGoroutine() {
  if (!state.selectedId) {
    state.selectedGoroutine = null;
    return;
  }

  try {
    state.selectedGoroutine = await fetchJSON(`/api/v1/goroutines/${state.selectedId}`);
  } catch {
    state.selectedGoroutine = state.goroutines.find((item) => item.goroutine_id === state.selectedId) ?? null;
  }
}

function parseGoroutineFromURL() {
  const params = new URLSearchParams(window.location.search);
  const id = params.get("goroutine");
  if (id) {
    const n = parseInt(id, 10);
    if (Number.isFinite(n) && n > 0) {
      state.initialGoroutineFromURL = n;
    }
  }
}

function updateURLForSelection() {
  const params = new URLSearchParams(window.location.search);
  if (state.selectedId) {
    params.set("goroutine", String(state.selectedId));
  } else {
    params.delete("goroutine");
  }
  const qs = params.toString();
  const url = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
  window.history.replaceState(null, "", url);
}

function ensureSelection() {
  const filtered = getFilteredGoroutines();
  if (filtered.length === 0) {
    state.selectedId = null;
    state.selectedGoroutine = null;
    state.initialGoroutineFromURL = null;
    return;
  }

  if (state.initialGoroutineFromURL) {
    const found = filtered.some((item) => item.goroutine_id === state.initialGoroutineFromURL);
    if (found) {
      state.selectedId = state.initialGoroutineFromURL;
      state.initialGoroutineFromURL = null;
      return;
    }
    state.initialGoroutineFromURL = null;
  }

  const selectedStillVisible = filtered.some((item) => item.goroutine_id === state.selectedId);
  if (selectedStillVisible) {
    return;
  }

  const preferred = state.sortMode === "ID"
    ? filtered.find((item) => item.state === "BLOCKED" || item.state === "WAITING") ?? filtered[0]
    : filtered[0];
  state.selectedId = preferred.goroutine_id;
  state.selectedGoroutine = preferred;
}

async function selectGoroutine(id) {
  state.selectedId = id;
  updateURLForSelection();
  await hydrateSelectedGoroutine();
  render();
}

async function jumpToGoroutine(id) {
  const exists = state.goroutines.some((g) => g.goroutine_id === id);
  if (!exists) {
    try {
      const g = await fetchJSON(`/api/v1/goroutines/${id}`);
      state.goroutines = [...state.goroutines, g];
      resetDerivedCaches();
    } catch {
      return;
    }
  }
  await selectGoroutine(id);
}

function resetDerivedCaches() {
  derivedCache.diagnosticsByID = new Map();
  derivedCache.resourceEdgesByID = null;
  derivedCache.timelineSegmentsByID = null;
}

function getStateUrgencyRank(stateName) {
  switch (stateName) {
    case "BLOCKED":
      return 5;
    case "WAITING":
      return 4;
    case "SYSCALL":
      return 3;
    case "RUNNABLE":
      return 2;
    case "RUNNING":
      return 1;
    case "DONE":
    default:
      return 0;
  }
}

function getFilteredGoroutines() {
  const filtered = state.goroutines
    .filter((item) => state.stateFilter === "ALL" || item.state === state.stateFilter)
    .filter((item) => !state.reasonFilter || item.reason === state.reasonFilter)
    .filter((item) => {
      if (!state.search) {
        return true;
      }

      const haystack = [
        String(item.goroutine_id),
        item.state,
        item.reason ?? "",
        item.resource_id ?? "",
        item.labels?.function ?? "",
      ].join(" ").toLowerCase();

      return haystack.includes(state.search);
    });
  const selected = state.selectedId && !filtered.some((g) => g.goroutine_id === state.selectedId)
    ? state.goroutines.find((g) => g.goroutine_id === state.selectedId)
    : null;
  if (selected) {
    filtered.push(selected);
  }
  return filtered.sort(compareGoroutinesForSort);
}

function compareGoroutinesForSort(left, right) {
  switch (state.sortMode) {
    case "BLOCKED":
      return compareGoroutinesByBlocked(left, right);
    case "SUSPICIOUS":
      return compareGoroutinesBySuspicion(left, right);
    case "ID":
    default:
      return left.goroutine_id - right.goroutine_id;
  }
}

function compareGoroutinesBySuspicion(left, right) {
  const leftDiagnostics = buildGoroutineDiagnostics(left);
  const rightDiagnostics = buildGoroutineDiagnostics(right);

  return (
    (rightDiagnostics?.suspicionScore ?? 0) - (leftDiagnostics?.suspicionScore ?? 0) ||
    (rightDiagnostics?.stallNS ?? 0) - (leftDiagnostics?.stallNS ?? 0) ||
    (right.wait_ns ?? 0) - (left.wait_ns ?? 0) ||
    getStateUrgencyRank(right.state) - getStateUrgencyRank(left.state) ||
    left.goroutine_id - right.goroutine_id
  );
}

function compareGoroutinesByBlocked(left, right) {
  const leftDiagnostics = buildGoroutineDiagnostics(left);
  const rightDiagnostics = buildGoroutineDiagnostics(right);

  return (
    getStateUrgencyRank(right.state) - getStateUrgencyRank(left.state) ||
    (right.wait_ns ?? 0) - (left.wait_ns ?? 0) ||
    (rightDiagnostics?.stallNS ?? 0) - (leftDiagnostics?.stallNS ?? 0) ||
    (rightDiagnostics?.suspicionScore ?? 0) - (leftDiagnostics?.suspicionScore ?? 0) ||
    left.goroutine_id - right.goroutine_id
  );
}

function getTimelineMetrics() {
  return {
    axisHeight: 38,
    rowHeight: 28,
    labelGutterWidth: 182,
    leftPadding: 14,
    rightPadding: 18,
  };
}

function buildSegmentKey(segment) {
  return `${segment.goroutine_id}:${segment.start_ns}:${segment.end_ns}:${segment.state}`;
}

function getTimelinePlotBounds(width, metrics) {
  const left = metrics.labelGutterWidth + metrics.leftPadding;
  const right = width - metrics.rightPadding;
  return {
    left,
    right,
    innerWidth: Math.max(right - left, 1),
  };
}

function getTimelineSegmentsForGoroutine(goroutineID) {
  if (!derivedCache.timelineSegmentsByID) {
    derivedCache.timelineSegmentsByID = new Map();
    for (const segment of state.timeline) {
      const segments = derivedCache.timelineSegmentsByID.get(segment.goroutine_id) ?? [];
      segments.push(segment);
      derivedCache.timelineSegmentsByID.set(segment.goroutine_id, segments);
    }
    for (const segments of derivedCache.timelineSegmentsByID.values()) {
      segments.sort((left, right) => left.start_ns - right.start_ns || left.end_ns - right.end_ns);
    }
  }

  return derivedCache.timelineSegmentsByID.get(goroutineID) ?? [];
}

function getResourceEdgesForGoroutine(goroutineID) {
  if (!derivedCache.resourceEdgesByID) {
    derivedCache.resourceEdgesByID = new Map();
    for (const edge of state.resources) {
      const fromEdges = derivedCache.resourceEdgesByID.get(edge.from_goroutine_id) ?? [];
      fromEdges.push(edge);
      derivedCache.resourceEdgesByID.set(edge.from_goroutine_id, fromEdges);

      if (edge.to_goroutine_id !== edge.from_goroutine_id) {
        const toEdges = derivedCache.resourceEdgesByID.get(edge.to_goroutine_id) ?? [];
        toEdges.push(edge);
        derivedCache.resourceEdgesByID.set(edge.to_goroutine_id, toEdges);
      }
    }
  }

  return derivedCache.resourceEdgesByID.get(goroutineID) ?? [];
}

function buildGoroutineDiagnostics(goroutine) {
  if (!goroutine) {
    return null;
  }

  const cached = derivedCache.diagnosticsByID.get(goroutine.goroutine_id);
  if (cached) {
    return cached;
  }

  const segments = getTimelineSegmentsForGoroutine(goroutine.goroutine_id);
  const totalsByState = Object.fromEntries(timelineStates.map((stateName) => [stateName, 0]));
  const resourceEdges = getResourceEdgesForGoroutine(goroutine.goroutine_id);
  const stallResources = new Set();
  let recordedNS = 0;
  let stallNS = 0;
  let longestStall = null;
  let stallSegmentCount = 0;

  for (const segment of segments) {
    const durationNS = Math.max(segment.end_ns - segment.start_ns, 0);
    totalsByState[segment.state] = (totalsByState[segment.state] ?? 0) + durationNS;
    recordedNS += durationNS;

    if (stallStates.has(segment.state)) {
      stallNS += durationNS;
      stallSegmentCount += 1;
      if (segment.resource_id) {
        stallResources.add(segment.resource_id);
      }
      if (!longestStall || durationNS > longestStall.durationNS) {
        longestStall = {
          state: segment.state,
          durationNS,
          reason: segment.reason || "",
          resourceID: segment.resource_id || "",
        };
      }
    }
  }

  const firstStartNS = segments.length > 0 ? segments[0].start_ns : 0;
  const lastEndNS = segments.length > 0 ? segments[segments.length - 1].end_ns : 0;
  const windowNS = segments.length > 0 ? Math.max(lastEndNS - firstStartNS, recordedNS) : 0;
  const diagnostics = {
    segments,
    totalsByState,
    recordedNS,
    windowNS,
    runningNS: totalsByState.RUNNING ?? 0,
    offCPUNS: timelineStates.reduce(
      (acc, stateName) => acc + (offCPUStates.has(stateName) ? (totalsByState[stateName] ?? 0) : 0),
      0,
    ),
    stallNS,
    longestStall,
    stallSegmentCount,
    transitionCount: Math.max(segments.length - 1, 0),
    resourceEdgeCount: resourceEdges.length,
    stallResourceCount: stallResources.size,
    completed: goroutine.state === "DONE" || segments.some((segment) => segment.state === "DONE"),
  };

  diagnostics.flags = buildSuspicionFlags(diagnostics);
  diagnostics.suspicionScore = scoreGoroutineDiagnostics(goroutine, diagnostics);
  diagnostics.primaryFlag = diagnostics.flags[0] ?? null;
  derivedCache.diagnosticsByID.set(goroutine.goroutine_id, diagnostics);
  return diagnostics;
}

function scoreGoroutineDiagnostics(goroutine, diagnostics) {
  if (!goroutine || !diagnostics) {
    return 0;
  }

  let score = 0;
  if (state.session?.ended_at && !diagnostics.completed) {
    score += 120;
  }
  if (diagnostics.longestStall) {
    score += 36 + Math.min(72, Math.round(diagnostics.longestStall.durationNS / 25_000_000));
  }
  if (diagnostics.stallSegmentCount >= 3) {
    score += 24 + Math.min(24, diagnostics.stallSegmentCount * 3);
  }
  if (diagnostics.resourceEdgeCount >= 3 || diagnostics.stallResourceCount >= 2) {
    score += 18;
  }
  if (diagnostics.transitionCount >= 8) {
    score += 12 + Math.min(18, diagnostics.transitionCount - 8);
  }
  if (goroutine.state === "BLOCKED" || goroutine.state === "WAITING") {
    score += 14;
  } else if (goroutine.state === "SYSCALL") {
    score += 9;
  }
  if ((goroutine.wait_ns ?? 0) > 0) {
    score += Math.min(28, Math.round(goroutine.wait_ns / 25_000_000));
  }
  if (diagnostics.offCPUNS > diagnostics.runningNS * 2 && diagnostics.windowNS >= 50_000_000) {
    score += 10;
  }
  return score;
}

function buildSuspicionFlags(diagnostics) {
  if (!diagnostics) {
    return [];
  }

  const flags = [];
  const sessionEnded = Boolean(state.session?.ended_at);

  if (sessionEnded && !diagnostics.completed) {
    flags.push({
      tone: "danger",
      label: "Unfinished",
      detail: "Session ended before this lane reached DONE.",
    });
  }

  if (
    diagnostics.longestStall &&
    diagnostics.longestStall.durationNS >= Math.max(100_000_000, diagnostics.windowNS * 0.25)
  ) {
    const stallParts = [
      formatDuration(diagnostics.longestStall.durationNS),
      diagnostics.longestStall.state.toLowerCase(),
    ];
    if (diagnostics.longestStall.reason) {
      stallParts.push(diagnostics.longestStall.reason);
    }
    flags.push({
      tone: "warn",
      label: "Long stall",
      detail: stallParts.join(" · "),
    });
  }

  if (
    diagnostics.stallSegmentCount >= 3 &&
    diagnostics.stallNS >= Math.max(50_000_000, diagnostics.recordedNS * 0.3)
  ) {
    flags.push({
      tone: "warn",
      label: "Repeated stalls",
      detail: `${diagnostics.stallSegmentCount} blocking segments across ${formatDuration(diagnostics.stallNS)}.`,
    });
  }

  if (
    diagnostics.stallNS > 0 &&
    (diagnostics.resourceEdgeCount >= 3 || diagnostics.stallResourceCount >= 2)
  ) {
    const details = [];
    if (diagnostics.resourceEdgeCount > 0) {
      details.push(`${diagnostics.resourceEdgeCount} graph edges`);
    }
    if (diagnostics.stallResourceCount > 0) {
      details.push(`${diagnostics.stallResourceCount} stall resources`);
    }
    flags.push({
      tone: "info",
      label: "Resource pressure",
      detail: details.join(" · "),
    });
  }

  if (diagnostics.transitionCount >= 8) {
    flags.push({
      tone: "info",
      label: "High churn",
      detail: `${diagnostics.transitionCount} state transitions in one lane.`,
    });
  }

  return flags;
}

function renderInspectorDiagnostics(diagnostics) {
  if (!diagnostics || diagnostics.segments.length === 0) {
    return "";
  }

  const summaryCards = [
    { label: "Active Window", value: formatDuration(diagnostics.windowNS), meta: "first seen to last segment" },
    { label: "Running", value: formatDuration(diagnostics.runningNS), meta: "on-CPU time" },
    { label: "Off CPU", value: formatDuration(diagnostics.offCPUNS), meta: "runnable + stalled" },
    {
      label: "Longest Stall",
      value: diagnostics.longestStall ? formatDuration(diagnostics.longestStall.durationNS) : "none",
      meta: diagnostics.longestStall ? diagnostics.longestStall.state : "no blocking segment",
    },
  ];

  const statesWithCoverage = timelineStates.filter((stateName) => (diagnostics.totalsByState[stateName] ?? 0) > 0);
  const stateBreakdown = statesWithCoverage.length > 0
    ? `
      <div class="diagnostic-bar" aria-hidden="true">
        ${statesWithCoverage.map((stateName) => {
          const value = diagnostics.totalsByState[stateName];
          const width = diagnostics.recordedNS > 0 ? (value / diagnostics.recordedNS) * 100 : 0;
          return `<span class="diagnostic-bar-segment ${stateName}" style="width:${width.toFixed(3)}%"></span>`;
        }).join("")}
      </div>
      <div class="diagnostic-state-grid">
        ${statesWithCoverage.map((stateName) => {
          const value = diagnostics.totalsByState[stateName];
          const share = diagnostics.recordedNS > 0 ? Math.round((value / diagnostics.recordedNS) * 100) : 0;
          return `
            <div class="diagnostic-state-card">
              <div class="diagnostic-state-head">
                <span class="diagnostic-state-dot ${stateName}"></span>
                <span>${stateName}</span>
              </div>
              <div class="diagnostic-state-values">
                <strong>${formatDuration(value)}</strong>
                <span>${share}%</span>
              </div>
            </div>
          `;
        }).join("")}
      </div>
    `
    : "";

  const flagsMarkup = diagnostics.flags.length > 0
    ? `
      <div class="diagnostic-flags">
        ${diagnostics.flags.map((flag) => `
          <div class="diagnostic-flag tone-${flag.tone}">
            <span class="diagnostic-flag-label">${escapeHTML(flag.label)}</span>
            <span class="diagnostic-flag-detail">${escapeHTML(flag.detail)}</span>
          </div>
        `).join("")}
      </div>
    `
    : `<div class="diagnostic-clear">No strong suspicion flags in the recorded trace for this lane.</div>`;

  const metaParts = [
    `${diagnostics.segments.length} segments`,
    `${diagnostics.transitionCount} transition${diagnostics.transitionCount === 1 ? "" : "s"}`,
  ];
  if (diagnostics.resourceEdgeCount > 0) {
    metaParts.push(`${diagnostics.resourceEdgeCount} resource edge${diagnostics.resourceEdgeCount === 1 ? "" : "s"}`);
  }

  return `
    <div class="inspector-section">
      <div class="inspector-label">Lane Diagnosis</div>
      <div class="diagnostic-grid">
        ${summaryCards.map((card) => `
          <div class="diagnostic-card">
            <span class="diagnostic-card-label">${escapeHTML(card.label)}</span>
            <strong class="diagnostic-card-value">${escapeHTML(card.value)}</strong>
            <span class="diagnostic-card-meta">${escapeHTML(card.meta)}</span>
          </div>
        `).join("")}
      </div>
      <div class="diagnostic-meta">${metaParts.join(" · ")}</div>
      ${stateBreakdown}
      ${flagsMarkup}
    </div>
  `;
}

function getDiagnosticContextHint(diagnostics) {
  if (!diagnostics) {
    return "";
  }
  if (diagnostics.primaryFlag) {
    return `${diagnostics.primaryFlag.label}: ${diagnostics.primaryFlag.detail}`;
  }
  if (diagnostics.longestStall) {
    return `Longest stall ${formatDuration(diagnostics.longestStall.durationNS)} ${diagnostics.longestStall.state.toLowerCase()}`;
  }
  if (diagnostics.transitionCount > 0) {
    return `${diagnostics.transitionCount} state transitions`;
  }
  return "";
}

function renderSuspicionTags(diagnostics, limit = 2) {
  if (!diagnostics || diagnostics.flags.length === 0) {
    return "";
  }

  return `<div class="suspicion-tags">
    ${diagnostics.flags.slice(0, limit).map((flag) => `
      <span class="suspicion-tag tone-${flag.tone}">${escapeHTML(flag.label)}</span>
    `).join("")}
  </div>`;
}

function getRelatedFocus() {
  const selectedID = state.selectedId;
  const rolesByID = new Map();

  if (!state.relatedFocus || selectedID === null) {
    return { enabled: false, selectedID, rolesByID, relatedCount: 0 };
  }

  const addRole = (goroutineID, role) => {
    if (!goroutineID) {
      return;
    }
    let roles = rolesByID.get(goroutineID);
    if (!roles) {
      roles = new Set();
      rolesByID.set(goroutineID, roles);
    }
    roles.add(role);
  };

  addRole(selectedID, "selected");

  const selected = state.goroutines.find((item) => item.goroutine_id === selectedID);
  if (!selected) {
    return { enabled: true, selectedID, rolesByID, relatedCount: 0 };
  }

  if (selected.parent_id) {
    addRole(selected.parent_id, "parent");
  }

  for (const goroutine of state.goroutines) {
    if (goroutine.parent_id === selectedID && goroutine.goroutine_id !== selectedID) {
      addRole(goroutine.goroutine_id, "child");
    }
  }

  for (const edge of state.resources) {
    if (edge.from_goroutine_id === selectedID && edge.to_goroutine_id !== selectedID) {
      addRole(edge.to_goroutine_id, "resource");
    } else if (edge.to_goroutine_id === selectedID && edge.from_goroutine_id !== selectedID) {
      addRole(edge.from_goroutine_id, "resource");
    }
  }

  return {
    enabled: true,
    selectedID,
    rolesByID,
    relatedCount: Math.max(0, rolesByID.size - 1),
  };
}

function getFocusRoles(focus, goroutineID) {
  if (!focus.enabled) {
    return null;
  }
  return focus.rolesByID.get(goroutineID) ?? null;
}

function getPrimaryFocusRole(focusRoles) {
  if (!focusRoles) {
    return "";
  }
  if (focusRoles.has("selected")) {
    return "selected";
  }
  if (focusRoles.has("child")) {
    return "child";
  }
  if (focusRoles.has("parent")) {
    return "parent";
  }
  if (focusRoles.has("resource")) {
    return "resource";
  }
  return "";
}

function getFocusAccentColor(primaryRole) {
  switch (primaryRole) {
    case "selected":
      return "rgba(125, 211, 252, 0.95)";
    case "parent":
      return "rgba(251, 191, 36, 0.92)";
    case "child":
      return "rgba(45, 212, 191, 0.92)";
    case "resource":
      return "rgba(244, 114, 182, 0.90)";
    default:
      return "";
  }
}

function focusRoleLabel(role) {
  switch (role) {
    case "selected":
      return "SELECTED";
    case "parent":
      return "PARENT";
    case "child":
      return "CHILD";
    case "resource":
      return "EDGE";
    default:
      return role.toUpperCase();
  }
}

function renderFocusTags(focus, goroutineID) {
  const focusRoles = getFocusRoles(focus, goroutineID);
  if (!focusRoles) {
    return "";
  }

  const orderedRoles = ["selected", "parent", "child", "resource"].filter((role) => focusRoles.has(role));
  return `<div class="focus-tags">${orderedRoles.map((role) => `
    <span class="focus-tag ${role}">${focusRoleLabel(role)}</span>
  `).join("")}</div>`;
}

function setTimelineHighlight(hoveredGoroutineID, hoveredSegmentKey) {
  const nextGoroutineID = hoveredGoroutineID ?? null;
  const nextSegmentKey = hoveredSegmentKey ?? "";
  if (
    timelineHighlight.hoveredGoroutineID === nextGoroutineID &&
    timelineHighlight.hoveredSegmentKey === nextSegmentKey
  ) {
    return;
  }

  timelineHighlight.hoveredGoroutineID = nextGoroutineID;
  timelineHighlight.hoveredSegmentKey = nextSegmentKey;
  renderCurrentView();
}

function clearTimelineHighlight() {
  setTimelineHighlight(null, "");
}

// ─── Render ────────────────────────────────────────────────────────────────

function render() {
  _renderBusy = true;
  try {
    renderSummary();
    renderFocusControls();
    renderLanePriority();
    renderGoroutineList();
    renderInspector();
    renderResources();
    renderResourceGraph();
    renderCurrentView();
    renderSessionHistory();
  } finally {
    _renderBusy = false;
  }
}

// renderCurrentView dispatches to the lane or heatmap renderer based on
// state.viewMode.  This is the single call-site to use when the view mode
// might have changed (resize, data refresh, toggle).
function renderCurrentView() {
  _renderBusy = true;
  try {
    if (state.viewMode === "heatmap") {
      renderHeatmap();
    } else {
      renderTimeline();
    }
  } finally {
    _renderBusy = false;
  }
}

function renderFocusControls() {
  if (!elements.focusRelatedButton) {
    return;
  }

  const focus = getRelatedFocus();
  const visibleRelatedCount = focus.enabled
    ? getFilteredGoroutines().filter(
      (item) => focus.rolesByID.has(item.goroutine_id) && item.goroutine_id !== focus.selectedID,
    ).length
    : 0;

  elements.focusRelatedButton.disabled = state.selectedId === null;
  elements.focusRelatedButton.classList.toggle("active", focus.enabled);
  elements.focusRelatedButton.setAttribute("aria-pressed", focus.enabled ? "true" : "false");
  elements.focusRelatedButton.textContent = focus.enabled
    ? `Related focus · ${visibleRelatedCount}`
    : "Related focus";
}

function renderSummary() {
  if (!state.session) {
    return;
  }

  const goroutines = getFilteredGoroutines();
  const blockedCount = state.goroutines.filter((item) => item.state === "BLOCKED" || item.state === "WAITING").length;
  const longBlockedCount = state.insights?.long_blocked_count ?? 0;
  const metaParts = [`Started ${formatTimestamp(state.session.started_at)}`];

  if (state.session.ended_at) {
    metaParts.push(`Ended ${formatTimestamp(state.session.ended_at)}`);
  }
  if (state.session.error) {
    metaParts.push(state.session.error);
  }

  elements.sessionName.textContent = state.session.name;
  elements.sessionTarget.textContent = state.session.target;
  elements.sessionStatus.textContent = state.session.status;
  elements.sessionStarted.textContent = metaParts.join(" • ");
  elements.goroutineCount.textContent = String(goroutines.length);
  elements.blockedCount.textContent = `${blockedCount} waiting or blocked`;

  if (elements.longBlockedCount) {
    elements.longBlockedCount.textContent = String(longBlockedCount);
  }
  if (elements.longBlockedCard) {
    elements.longBlockedCard.classList.toggle("active", Boolean(state.minWaitNs));
    elements.longBlockedCard.setAttribute("aria-pressed", state.minWaitNs ? "true" : "false");
  }
}

function buildLaneItemHTML(goroutine, focus, diagnostics) {
  const focusRoles = getFocusRoles(focus, goroutine.goroutine_id);
  const primaryFocusRole = getPrimaryFocusRole(focusRoles);
  const focusClass = focus.enabled
    ? focusRoles ? ` focus-related focus-${primaryFocusRole}` : " focus-dimmed"
    : "";
  const waitBadge = goroutine.wait_ns > 0
    ? `<span class="wait-badge">${formatDuration(goroutine.wait_ns)}</span>`
    : "";
  const focusTags = focus.enabled ? renderFocusTags(focus, goroutine.goroutine_id) : "";
  const suspicionTags = renderSuspicionTags(diagnostics);
  const primaryFlag = diagnostics?.primaryFlag
    ? `<div class="lane-priority-line">${escapeHTML(diagnostics.primaryFlag.detail)}</div>`
    : "";

  return {
    className: `lane-item${goroutine.goroutine_id === state.selectedId ? " active" : ""}${focusClass}`,
    html: `
      <div class="lane-item-header">
        <span class="lane-item-title">G${goroutine.goroutine_id}</span>
        <span class="state-pill ${goroutine.state}">${goroutine.state}</span>
      </div>
      <div class="lane-item-meta">
        <div class="lane-func">${escapeHTML(goroutine.labels?.function || "unknown function")}</div>
        <div class="lane-reason">${escapeHTML(goroutine.reason || "no active wait reason")} ${waitBadge}</div>
      </div>
      ${suspicionTags}
      ${primaryFlag}
      ${focusTags}
    `,
    goroutine,
  };
}

function renderGoroutineList() {
  const goroutines = getFilteredGoroutines();
  const focus = getRelatedFocus();

  if (goroutines.length === 0) {
    elements.goroutineList.innerHTML = `<div class="empty-message">No goroutines match the current filters.</div>`;
    return;
  }

  const useVirtual = goroutines.length > LANE_VIRTUAL_THRESHOLD;

  if (!useVirtual) {
    elements.goroutineList.innerHTML = "";
    elements.goroutineList.classList.remove("lane-list-virtual");
    for (const goroutine of goroutines) {
      const diagnostics = buildGoroutineDiagnostics(goroutine);
      const { className, html, goroutine: g } = buildLaneItemHTML(goroutine, focus, diagnostics);
      const button = document.createElement("button");
      button.type = "button";
      button.className = className;
      button.innerHTML = html;
      button.addEventListener("click", () => selectGoroutine(g.goroutine_id));
      elements.goroutineList.appendChild(button);
    }
    return;
  }

  const viewport = elements.goroutineList.querySelector(".lane-list-viewport");
  const scrollTop = viewport ? viewport.scrollTop : laneListState.scrollTop;
  const totalHeight = goroutines.length * LANE_ITEM_HEIGHT;

  if (!viewport) {
    elements.goroutineList.classList.add("lane-list-virtual");
    elements.goroutineList.innerHTML = `
      <div class="lane-list-viewport">
        <div class="lane-list-spacer" style="height: ${totalHeight}px"></div>
        <div class="lane-list-content"></div>
      </div>
    `;
    const newViewport = elements.goroutineList.querySelector(".lane-list-viewport");
    newViewport.addEventListener("scroll", () => {
      laneListState.scrollTop = newViewport.scrollTop;
      renderGoroutineListVisible(getFilteredGoroutines(), getRelatedFocus(), newViewport);
    });
  } else {
    const spacer = viewport.querySelector(".lane-list-spacer");
    if (spacer) spacer.style.height = `${totalHeight}px`;
  }

  const vp = elements.goroutineList.querySelector(".lane-list-viewport");
  if (vp) {
    let targetScroll = scrollTop;
    if (state.selectedId) {
      const selectedIndex = goroutines.findIndex((g) => g.goroutine_id === state.selectedId);
      if (selectedIndex >= 0) {
        const selectedTop = selectedIndex * LANE_ITEM_HEIGHT;
        const selectedBottom = selectedTop + LANE_ITEM_HEIGHT;
        if (selectedTop < scrollTop) {
          targetScroll = selectedTop;
        } else if (selectedBottom > scrollTop + LANE_LIST_VIEWPORT_HEIGHT) {
          targetScroll = selectedBottom - LANE_LIST_VIEWPORT_HEIGHT;
        }
      }
    }
    vp.scrollTop = Math.min(targetScroll, Math.max(0, totalHeight - LANE_LIST_VIEWPORT_HEIGHT));
    laneListState.scrollTop = vp.scrollTop;
    renderGoroutineListVisible(goroutines, focus, vp);
  }
}

function renderGoroutineListVisible(goroutines, focus, viewport) {
  const content = viewport?.querySelector(".lane-list-content");
  if (!content) return;

  const scrollTop = viewport.scrollTop;
  const startIndex = Math.max(0, Math.floor(scrollTop / LANE_ITEM_HEIGHT));
  const visibleCount = Math.ceil(LANE_LIST_VIEWPORT_HEIGHT / LANE_ITEM_HEIGHT) + 2;
  const endIndex = Math.min(goroutines.length, startIndex + visibleCount);

  content.innerHTML = "";
  content.style.top = `${startIndex * LANE_ITEM_HEIGHT}px`;

  for (let i = startIndex; i < endIndex; i++) {
    const goroutine = goroutines[i];
    const diagnostics = buildGoroutineDiagnostics(goroutine);
    const { className, html, goroutine: g } = buildLaneItemHTML(goroutine, focus, diagnostics);
    const button = document.createElement("button");
    button.type = "button";
    button.className = `lane-item lane-item-virtual ${className}`;
    button.style.minHeight = `${LANE_ITEM_HEIGHT - 10}px`;
    button.innerHTML = html;
    button.addEventListener("click", () => selectGoroutine(g.goroutine_id));
    content.appendChild(button);
  }
}

function renderLanePriority() {
  if (!elements.lanePriority) {
    return;
  }

  const flagged = getFilteredGoroutines()
    .map((goroutine) => ({ goroutine, diagnostics: buildGoroutineDiagnostics(goroutine) }))
    .filter(({ diagnostics }) => diagnostics && diagnostics.flags.length > 0);

  if (flagged.length === 0) {
    elements.lanePriority.hidden = true;
    elements.lanePriority.innerHTML = "";
    return;
  }

  const topFlagged = flagged
    .slice()
    .sort((left, right) => compareGoroutinesBySuspicion(left.goroutine, right.goroutine))
    .slice(0, 4);

  elements.lanePriority.hidden = false;
  elements.lanePriority.innerHTML = `
    <div class="lane-priority-header">
      <div>
        <span class="lane-priority-kicker">Top Offenders</span>
        <strong>${flagged.length} flagged lane${flagged.length === 1 ? "" : "s"} in view</strong>
      </div>
      <span class="lane-priority-hint">${state.sortMode === "SUSPICIOUS" ? "sorted by suspicion" : "quick jump"}</span>
    </div>
    <div class="lane-priority-list">
      ${topFlagged.map(({ goroutine, diagnostics }) => `
        <button
          type="button"
          class="lane-priority-chip${goroutine.goroutine_id === state.selectedId ? " active" : ""}"
          data-priority-goroutine="${goroutine.goroutine_id}"
        >
          <span class="lane-priority-chip-id">G${goroutine.goroutine_id}</span>
          <span class="lane-priority-chip-label">${escapeHTML(diagnostics.primaryFlag?.label || "Needs attention")}</span>
        </button>
      `).join("")}
    </div>
  `;

  elements.lanePriority.querySelectorAll("[data-priority-goroutine]").forEach((button) => {
    button.addEventListener("click", () => {
      selectGoroutine(Number(button.dataset.priorityGoroutine));
    });
  });
}

function renderInspector() {
  const goroutine = state.selectedGoroutine;
  if (!goroutine) {
    elements.inspector.innerHTML = `<div class="empty-message">Pick a goroutine to inspect its current state and stack trace.</div>`;
    return;
  }

  const diagnostics = buildGoroutineDiagnostics(goroutine);
  const frames = goroutine.last_stack?.frames ?? [];
  const stackMarkup = frames.length > 0
    ? frames.map((frame) => `
        <div class="stack-frame">
          <div class="stack-func">${escapeHTML(frame.func)}</div>
          <div class="stack-path">${escapeHTML(frame.file)}:${frame.line}</div>
        </div>
      `).join("")
    : `<div class="empty-message">No stack snapshot yet.</div>`;

  elements.inspector.innerHTML = `
    <div>
      <div class="state-pill ${goroutine.state}">${goroutine.state}</div>
    </div>
    ${renderInspectorDiagnostics(diagnostics)}
    <div class="inspector-section inspector-grid">
      <div>
        <div class="inspector-label">Goroutine</div>
        <div class="inspector-value">#${goroutine.goroutine_id}</div>
      </div>
      <div>
        <div class="inspector-label">Wait Time</div>
        <div class="inspector-value">${formatDuration(goroutine.wait_ns ?? 0)}</div>
      </div>
      <div>
        <div class="inspector-label">Reason</div>
        <div class="inspector-value">${escapeHTML(goroutine.reason || "none")}</div>
      </div>
      <div>
        <div class="inspector-label">Resource</div>
        <div class="inspector-value">${escapeHTML(goroutine.resource_id || "none")}</div>
      </div>
      <div>
        <div class="inspector-label">Created</div>
        <div class="inspector-value">${formatTimestamp(goroutine.created_at)}</div>
      </div>
      <div>
        <div class="inspector-label">Last Seen</div>
        <div class="inspector-value">${formatTimestamp(goroutine.last_seen_at)}</div>
      </div>
    </div>
    <div class="inspector-section">
      <div class="inspector-label">Function</div>
      <div class="inspector-value">${escapeHTML(goroutine.labels?.function || "unknown")}</div>
    </div>
    ${renderSpawnTree(goroutine)}
    <div class="inspector-section">
      <div class="inspector-label">Latest Stack</div>
      ${stackMarkup}
    </div>
  `;

  // Wire up spawn-tree click handlers after the HTML is injected.
  elements.inspector.querySelectorAll("[data-select-goroutine]").forEach((button) => {
    button.addEventListener("click", () => {
      selectGoroutine(Number(button.dataset.selectGoroutine));
    });
  });
}

// renderSpawnTree returns the HTML for the "Spawn Tree" section of the
// inspector.  It uses the already-loaded state.goroutines list so no extra
// network request is needed.
function renderSpawnTree(goroutine) {
  const parent = goroutine.parent_id
    ? state.goroutines.find((item) => item.goroutine_id === goroutine.parent_id)
    : null;

  const children = state.goroutines.filter(
    (item) => item.parent_id === goroutine.goroutine_id,
  );

  if (!parent && children.length === 0) {
    return "";
  }

  const parentLine = parent
    ? `<div class="spawn-row">
        <span class="spawn-label">Spawned by</span>
        ${goroutineChip(parent)}
       </div>`
    : "";

  const childLine = children.length > 0
    ? `<div class="spawn-row">
        <span class="spawn-label">Spawned</span>
        <span class="spawn-chips">${children.map(goroutineChip).join("")}</span>
       </div>`
    : "";

  return `
    <div class="inspector-section">
      <div class="inspector-label">Spawn Tree</div>
      <div class="spawn-tree">
        ${parentLine}
        ${childLine}
      </div>
    </div>
  `;
}

function goroutineChip(goroutine) {
  return `<button type="button" class="goroutine-chip state-chip-${goroutine.state}" data-select-goroutine="${goroutine.goroutine_id}">
    G${goroutine.goroutine_id}
    <span class="chip-state">${goroutine.state}</span>
  </button>`;
}

function renderResources() {
  const selectedID = state.selectedId;
  const edges = state.resources.filter((edge) => edge.from_goroutine_id === selectedID || edge.to_goroutine_id === selectedID);

  if (edges.length === 0) {
    elements.resourceList.innerHTML = `<div class="empty-message">No resource edges for the current selection.</div>`;
    return;
  }

  elements.resourceList.innerHTML = edges.map((edge) => `
    <div class="resource-edge">
      <strong>${edge.kind}</strong><br>
      G${edge.from_goroutine_id} → G${edge.to_goroutine_id}<br>
      ${escapeHTML(edge.resource_id)}
    </div>
  `).join("");
}

function renderResourceGraph() {
  const table = elements.resourceGraphTable;
  if (!table) return;

  const edges = state.resources;
  if (elements.resourceGraphCount) {
    elements.resourceGraphCount.textContent = edges.length > 0 ? `(${edges.length})` : "";
  }
  const selectedID = state.selectedId;

  if (edges.length === 0) {
    table.innerHTML = `<div class="empty-message">No resource edges in trace.</div>`;
    table.hidden = false;
    return;
  }

  const rows = edges.map((edge) => {
    const involvesSelected = selectedID !== null &&
      (edge.from_goroutine_id === selectedID || edge.to_goroutine_id === selectedID);
    const rowClass = involvesSelected ? "resource-graph-row resource-graph-row-highlight" : "resource-graph-row";
    return `
      <tr class="${rowClass}" data-from="${edge.from_goroutine_id}" data-to="${edge.to_goroutine_id}">
        <td><button type="button" class="resource-graph-gid" data-select-goroutine="${edge.from_goroutine_id}">G${edge.from_goroutine_id}</button></td>
        <td><button type="button" class="resource-graph-gid" data-select-goroutine="${edge.to_goroutine_id}">G${edge.to_goroutine_id}</button></td>
        <td>${escapeHTML(edge.kind || "—")}</td>
        <td class="resource-graph-resource">${escapeHTML(edge.resource_id || "—")}</td>
      </tr>
    `;
  }).join("");

  table.innerHTML = `
    <table class="resource-graph-table-inner">
      <thead>
        <tr>
          <th>From</th>
          <th>To</th>
          <th>Kind</th>
          <th>Resource</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>
  `;

  table.querySelectorAll("[data-select-goroutine]").forEach((btn) => {
    btn.addEventListener("click", () => {
      selectGoroutine(Number(btn.dataset.selectGoroutine));
    });
  });

  table.hidden = false;
}

function renderSessionHistory() {
  if (!elements.sessionHistory) {
    return;
  }

  const sessions = state.sessions;
  if (sessions.length === 0) {
    elements.sessionHistory.hidden = true;
    return;
  }

  elements.sessionHistory.hidden = false;

  const rows = sessions
    .slice()
    .reverse()
    .map((session) => {
      const durationNS = session.ended_at
        ? (new Date(session.ended_at) - new Date(session.started_at)) * 1_000_000
        : null;
      const statusClass = session.status === "COMPLETED" ? "status-completed"
        : session.status === "FAILED" ? "status-failed"
        : "status-running";

      return `
        <div class="history-row">
          <span class="history-name">${escapeHTML(session.name)}</span>
          <span class="history-target">${escapeHTML(session.target)}</span>
          <span class="history-status ${statusClass}">${session.status}</span>
          <span class="history-time">${formatTimestamp(session.started_at)}</span>
          <span class="history-duration">${durationNS !== null ? formatDuration(durationNS) : "—"}</span>
        </div>
      `;
    }).join("");

  elements.sessionHistory.innerHTML = `
    <div class="history-header">
      <h3>Session History</h3>
      <span class="history-count">${sessions.length} session${sessions.length !== 1 ? "s" : ""}</span>
    </div>
    <div class="history-list">
      <div class="history-row history-heading">
        <span>Name</span><span>Target</span><span>Status</span><span>Started</span><span>Duration</span>
      </div>
      ${rows}
    </div>
  `;
}

// ─── Timeline canvas rendering ─────────────────────────────────────────────

function renderTimeline() {
  const goroutines = getFilteredGoroutines();
  const focus = getRelatedFocus();
  const metrics = getTimelineMetrics();
  const width = Math.max(320, elements.timelineCanvas.parentElement.clientWidth);
  const height = Math.max(220, metrics.axisHeight + goroutines.length * metrics.rowHeight + 16);
  const dpr = window.devicePixelRatio || 1;

  const newW = Math.floor(width * dpr);
  const newH = Math.floor(height * dpr);
  if (elements.timelineCanvas.width !== newW) elements.timelineCanvas.width = newW;
  if (elements.timelineCanvas.height !== newH) elements.timelineCanvas.height = newH;
  const sw = `${width}px`, sh = `${height}px`;
  if (elements.timelineCanvas.style.width  !== sw) elements.timelineCanvas.style.width  = sw;
  if (elements.timelineCanvas.style.height !== sh) elements.timelineCanvas.style.height = sh;

  canvasContext.setTransform(dpr, 0, 0, dpr, 0, 0);
  canvasContext.clearRect(0, 0, width, height);

  canvasContext.fillStyle = "#0d1117";
  canvasContext.fillRect(0, 0, width, height);

  if (goroutines.length === 0) {
    canvasContext.fillStyle = "#dbe4ee";
    canvasContext.font = '16px "IBM Plex Mono", monospace';
    canvasContext.fillText("No timeline data for the current filters.", 24, 52);
    elements.timelineRange.textContent = "No visible range";
    timelineCache.metrics = null;
    updateZoomControls();
    return;
  }

  const timeline = state.timeline.filter((segment) => goroutines.some((item) => item.goroutine_id === segment.goroutine_id));
  if (timeline.length === 0) {
    canvasContext.fillStyle = "#dbe4ee";
    canvasContext.font = '16px "IBM Plex Mono", monospace';
    canvasContext.fillText("Timeline is empty for the current selection.", 24, 52);
    elements.timelineRange.textContent = "No visible range";
    timelineCache.metrics = null;
    updateZoomControls();
    return;
  }

  // Full trace bounds (never affected by zoom/pan).
  const fullMinStart = Math.min(...timeline.map((segment) => segment.start_ns));
  const fullMaxEnd = Math.max(...timeline.map((segment) => segment.end_ns));
  const fullSpan = Math.max(fullMaxEnd - fullMinStart, 1);

  // Compute visible window from timelineView.  Clamp pan to keep it in range.
  const visibleSpan = fullSpan / timelineView.zoomLevel;
  timelineView.panOffsetNS = Math.max(0, Math.min(fullSpan - visibleSpan, timelineView.panOffsetNS));
  const visibleStart = fullMinStart + timelineView.panOffsetNS;
  const visibleEnd = visibleStart + visibleSpan;

  // Populate cache so tooltip hit-test and wheel handler can reuse these.
  timelineCache.goroutines = goroutines;
  timelineCache.timeline = timeline;
  timelineCache.fullMinStart = fullMinStart;
  timelineCache.fullSpan = fullSpan;
  timelineCache.minStart = visibleStart;
  timelineCache.span = visibleSpan;
  timelineCache.width = width;
  timelineCache.metrics = metrics;

  const zoomText = timelineView.zoomLevel > 1.05
    ? ` · ${timelineView.zoomLevel.toFixed(1)}× zoom`
    : "";
  const visibleFocusCount = focus.enabled
    ? goroutines.filter((item) => focus.rolesByID.has(item.goroutine_id)).length
    : 0;
  const focusText = focus.enabled ? ` · focus ${visibleFocusCount} lane${visibleFocusCount === 1 ? "" : "s"}` : "";
  elements.timelineRange.textContent = `${formatDuration(visibleSpan)} visible window${zoomText}${focusText}`;

  updateZoomControls();

  const { left: plotLeft, innerWidth } = getTimelinePlotBounds(width, metrics);

  drawAxis(visibleStart, visibleEnd, fullMinStart, width, metrics);
  renderTimelineContext();

  canvasContext.fillStyle = "rgba(2, 6, 23, 0.48)";
  canvasContext.fillRect(0, metrics.axisHeight, plotLeft - 8, height - metrics.axisHeight);
  canvasContext.strokeStyle = "rgba(219, 228, 238, 0.10)";
  canvasContext.beginPath();
  canvasContext.moveTo(plotLeft - 0.5, metrics.axisHeight - 18);
  canvasContext.lineTo(plotLeft - 0.5, height - 16);
  canvasContext.stroke();

  goroutines.forEach((goroutine, index) => {
    const y = metrics.axisHeight + index * metrics.rowHeight;
    const isSelected = goroutine.goroutine_id === state.selectedId;
    const isHoveredRow = goroutine.goroutine_id === timelineHighlight.hoveredGoroutineID;
    const focusRoles = getFocusRoles(focus, goroutine.goroutine_id);
    const primaryFocusRole = getPrimaryFocusRole(focusRoles);
    const focusAccent = getFocusAccentColor(primaryFocusRole);
    const isFocusRelated = focus.enabled && !!focusRoles && !focusRoles.has("selected");
    const isDimmed = focus.enabled && !focusRoles;

    // Zebra stripe: even rows get a faint background so the eye can track
    // horizontally across wide traces without losing its row.
    if (index % 2 === 0) {
      canvasContext.fillStyle = "rgba(255, 255, 255, 0.028)";
      canvasContext.fillRect(0, y, width, metrics.rowHeight);
    }

    if (isSelected) {
      canvasContext.fillStyle = "rgba(96, 165, 250, 0.10)";
      canvasContext.fillRect(0, y, width, metrics.rowHeight);
      canvasContext.fillStyle = "rgba(125, 211, 252, 0.95)";
      canvasContext.fillRect(0, y + 2, 4, metrics.rowHeight - 4);
    } else if (isFocusRelated) {
      canvasContext.fillStyle = "rgba(255, 255, 255, 0.035)";
      canvasContext.fillRect(0, y, width, metrics.rowHeight);
      if (focusAccent) {
        canvasContext.fillStyle = focusAccent;
        canvasContext.fillRect(0, y + 4, 3, metrics.rowHeight - 8);
      }
    } else if (isHoveredRow) {
      canvasContext.fillStyle = isDimmed ? "rgba(219, 228, 238, 0.04)" : "rgba(219, 228, 238, 0.06)";
      canvasContext.fillRect(0, y, width, metrics.rowHeight);
    }

    canvasContext.strokeStyle = "rgba(219, 228, 238, 0.13)";
    canvasContext.beginPath();
    canvasContext.moveTo(0, y + metrics.rowHeight - 0.5);
    canvasContext.lineTo(width - metrics.rightPadding, y + metrics.rowHeight - 0.5);
    canvasContext.stroke();

    canvasContext.fillStyle = isSelected
      ? "#f8fafc"
      : isFocusRelated || isHoveredRow
        ? "#dbe4ee"
        : isDimmed
          ? "rgba(159, 179, 200, 0.46)"
          : "#9fb3c8";
    canvasContext.font = '12px "IBM Plex Mono", monospace';
    canvasContext.fillText(`G${goroutine.goroutine_id}`, 14, y + 12);
    canvasContext.fillStyle = isDimmed ? "rgba(159, 179, 200, 0.40)" : "rgba(219, 228, 238, 0.74)";
    canvasContext.font = '11px "IBM Plex Mono", monospace';
    const laneFunction = truncateCanvasText(
      canvasContext,
      goroutine.labels?.function || "unknown",
      metrics.labelGutterWidth - 22,
    );
    canvasContext.fillText(laneFunction, 14, y + 23);

    timeline
      .filter((segment) => segment.goroutine_id === goroutine.goroutine_id)
      .forEach((segment) => {
        const isHoveredSegment = buildSegmentKey(segment) === timelineHighlight.hoveredSegmentKey;
        // Map segment to canvas X coordinates using the visible window.
        const rawX = plotLeft + ((segment.start_ns - visibleStart) / visibleSpan) * innerWidth;
        const rawX2 = plotLeft + ((segment.end_ns - visibleStart) / visibleSpan) * innerWidth;

        // Clip to the drawable area — segments may extend outside the visible window.
        const clampedX = Math.max(plotLeft, Math.min(rawX, plotLeft + innerWidth));
        const clampedX2 = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));

        const barWidth = Math.max(clampedX2 - clampedX, clampedX2 > clampedX ? 2 : 0);
        if (barWidth === 0) {
          return;
        }

        // Tall bars (20 px) fill most of the 28 px row — profiler-style density.
        // 4 px top padding, 4 px bottom: (28 - 20) / 2 = 4.
        const barHeight = 20;
        const barY = y + 4;

        canvasContext.save();
        if (isDimmed) {
          canvasContext.globalAlpha = 0.18;
        } else if (isFocusRelated) {
          canvasContext.globalAlpha = 0.92;
        }

        // Flat fill — vivid colours read clearly without gradient overhead.
        canvasContext.fillStyle = colors[segment.state] ?? "#94a3b8";
        roundRect(canvasContext, clampedX, barY, barWidth, barHeight, 2);
        canvasContext.fill();

        // 1 px bright top edge gives the bar a crisp "lit from above" feel
        // without a full gradient pass.
        if (!isDimmed && barWidth > 2) {
          canvasContext.fillStyle = "rgba(255, 255, 255, 0.22)";
          canvasContext.fillRect(clampedX + 1, barY, barWidth - 2, 1);
        }

        if (isSelected || isHoveredSegment) {
          canvasContext.lineWidth = isHoveredSegment ? 2 : 1.5;
          canvasContext.strokeStyle = isHoveredSegment
            ? "rgba(255, 255, 255, 0.95)"
            : "rgba(186, 230, 253, 0.72)";
          roundRect(canvasContext, clampedX, barY, barWidth, barHeight, 2);
          canvasContext.stroke();
        } else if (isFocusRelated && focusAccent) {
          canvasContext.lineWidth = 1;
          canvasContext.strokeStyle = focusAccent.replace(/0\.\d+\)$/, "0.42)");
          roundRect(canvasContext, clampedX, barY, barWidth, barHeight, 2);
          canvasContext.stroke();
        }
        canvasContext.restore();

        if (barWidth > 78) {
          canvasContext.fillStyle = isDimmed ? "rgba(255, 255, 255, 0.50)" : "rgba(255, 255, 255, 0.94)";
          canvasContext.font = '11px "IBM Plex Mono", monospace';
          // Baseline centred in a 20 px bar: barY + 14 puts cap height mid-bar.
          canvasContext.fillText(segment.state, clampedX + 8, barY + 14);
        }
      });
  });

  // Render the minimap overview strip after the main canvas is done.
  renderMinimap();
}

// drawAxis draws the time axis tick marks and labels.  Tick labels show
// elapsed time from the beginning of the full trace (fullMinStart) so the
// values remain meaningful when zoomed and panned.
function drawAxis(visibleStart, visibleEnd, fullMinStart, width, metrics) {
  const ticks = 5;
  const visibleSpan = Math.max(visibleEnd - visibleStart, 1);
  const { left: plotLeft, right: plotRight, innerWidth } = getTimelinePlotBounds(width, metrics);

  canvasContext.strokeStyle = "rgba(219, 228, 238, 0.14)";
  canvasContext.beginPath();
  canvasContext.moveTo(plotLeft, metrics.axisHeight - 10);
  canvasContext.lineTo(plotRight, metrics.axisHeight - 10);
  canvasContext.stroke();

  for (let index = 0; index < ticks; index += 1) {
    const ratio = ticks === 1 ? 0 : index / (ticks - 1);
    const x = plotLeft + ratio * innerWidth;
    const value = visibleStart + ratio * visibleSpan;

    canvasContext.strokeStyle = "rgba(219, 228, 238, 0.12)";
    canvasContext.beginPath();
    canvasContext.moveTo(x, metrics.axisHeight - 18);
    canvasContext.lineTo(x, elements.timelineCanvas.clientHeight - 16);
    canvasContext.stroke();

    canvasContext.fillStyle = "#dbe4ee";
    canvasContext.font = '11px "IBM Plex Mono", monospace';
    // Label shows elapsed time from the very start of the trace.
    canvasContext.fillText(formatDuration(value - fullMinStart), x + 6, 20);
  }
}

// updateZoomControls shows or hides the reset zoom button.
function updateZoomControls() {
  if (elements.resetZoomButton) {
    elements.resetZoomButton.hidden = timelineView.zoomLevel <= 1.05;
  }
}

// ─── Tooltip ──────────────────────────────────────────────────────────────

// getSegmentAt returns the timeline segment and goroutine at canvas-local
// coordinates (canvasX, canvasY), or null if the cursor is not over any bar.
// Uses the current visible window stored in timelineCache.minStart/span.
function getSegmentAt(canvasX, canvasY) {
  const { goroutines, timeline, minStart, span, width, metrics } = timelineCache;
  if (!metrics || goroutines.length === 0) {
    return null;
  }

  if (canvasY <= metrics.axisHeight) {
    return null;
  }

  const rowIndex = Math.floor((canvasY - metrics.axisHeight) / metrics.rowHeight);
  if (rowIndex < 0 || rowIndex >= goroutines.length) {
    return null;
  }

  const goroutine = goroutines[rowIndex];
  const { left: plotLeft, innerWidth } = getTimelinePlotBounds(width, metrics);

  for (const seg of timeline) {
    if (seg.goroutine_id !== goroutine.goroutine_id) {
      continue;
    }

    // Map segment to canvas X using the visible window (same as renderTimeline).
    const rawX = plotLeft + ((seg.start_ns - minStart) / span) * innerWidth;
    const rawX2 = plotLeft + ((seg.end_ns - minStart) / span) * innerWidth;
    const segX = Math.max(plotLeft, Math.min(rawX, plotLeft + innerWidth));
    const segX2 = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
    const barWidth = Math.max(segX2 - segX, segX2 > segX ? 2 : 0);

    if (barWidth > 0 && canvasX >= segX && canvasX <= segX + barWidth) {
      return { segment: seg, goroutine };
    }
  }

  return null;
}

function getTimelineRowAt(canvasY) {
  const { goroutines, metrics } = timelineCache;
  if (!metrics || goroutines.length === 0 || canvasY <= metrics.axisHeight) {
    return null;
  }

  const rowIndex = Math.floor((canvasY - metrics.axisHeight) / metrics.rowHeight);
  if (rowIndex < 0 || rowIndex >= goroutines.length) {
    return null;
  }

  return goroutines[rowIndex];
}

function getHoveredTimelineNS(canvasX) {
  const { minStart, span, width, metrics } = timelineCache;
  if (!metrics || width <= 0) {
    return null;
  }

  const { left: plotLeft, right: plotRight, innerWidth } = getTimelinePlotBounds(width, metrics);
  if (canvasX < plotLeft || canvasX > plotRight) {
    return null;
  }

  const ratio = (canvasX - plotLeft) / innerWidth;
  return minStart + ratio * span;
}

function updateTimelineCursor(canvasX) {
  const cursor = elements.timelineCursor;
  if (!cursor) {
    return;
  }

  const hoveredNS = getHoveredTimelineNS(canvasX);
  if (hoveredNS === null) {
    cursor.hidden = true;
    return;
  }

  cursor.hidden = false;
  cursor.style.left = `${canvasX}px`;
  cursor.style.height = `${elements.timelineCanvas.clientHeight}px`;
}

function hideTimelineCursor() {
  if (elements.timelineCursor) {
    elements.timelineCursor.hidden = true;
  }
}

function renderTimelineContext(hover = {}) {
  if (!elements.timelineContext) {
    return;
  }

  const { hit = null, hoveredRow = null, hoveredNS = null } = hover;
  if (hit && hoveredNS !== null) {
    const { segment, goroutine } = hit;
    const parts = [
      `<strong>Hover G${goroutine.goroutine_id}</strong>`,
      escapeHTML(segment.state),
      formatDuration(segment.end_ns - segment.start_ns),
      `T+${formatDuration(hoveredNS - timelineCache.fullMinStart)}`,
    ];
    if (segment.reason) {
      parts.push(escapeHTML(segment.reason));
    }
    if (segment.resource_id) {
      parts.push(escapeHTML(segment.resource_id));
    }
    elements.timelineContext.innerHTML = parts.join(" · ");
    return;
  }

  if (hoveredRow && hoveredNS !== null) {
    elements.timelineContext.innerHTML = `<strong>Hover G${hoveredRow.goroutine_id}</strong> · ${escapeHTML(hoveredRow.labels?.function || "unknown")} · T+${formatDuration(hoveredNS - timelineCache.fullMinStart)}`;
    return;
  }

  if (state.selectedGoroutine) {
    const selected = state.selectedGoroutine;
    const diagnostics = buildGoroutineDiagnostics(selected);
    const parts = [
      `<strong>Selected G${selected.goroutine_id}</strong>`,
      escapeHTML(selected.state),
      escapeHTML(selected.labels?.function || "unknown"),
    ];
    if (selected.reason) {
      parts.push(escapeHTML(selected.reason));
    }
    if (selected.resource_id) {
      parts.push(escapeHTML(selected.resource_id));
    }
    const diagnosticHint = getDiagnosticContextHint(diagnostics);
    if (diagnosticHint) {
      parts.push(escapeHTML(diagnosticHint));
    }
    elements.timelineContext.innerHTML = parts.join(" · ");
    return;
  }

  elements.timelineContext.textContent = "Select a goroutine to inspect related lanes and timeline segments.";
}

function showTooltip(hit, clientX, clientY) {
  const { segment, goroutine } = hit;
  const duration = segment.end_ns - segment.start_ns;
  const reasonLine = segment.reason
    ? `<div class="tt-row"><span class="tt-label">Reason</span><span class="tt-val">${escapeHTML(segment.reason)}</span></div>`
    : "";
  const resourceLine = segment.resource_id
    ? `<div class="tt-row"><span class="tt-label">Resource</span><span class="tt-val">${escapeHTML(segment.resource_id)}</span></div>`
    : "";

  elements.tooltip.innerHTML = `
    <div class="tt-header">
      <span class="tt-gid">G${goroutine.goroutine_id}</span>
      <span class="state-pill ${segment.state} tt-state">${segment.state}</span>
    </div>
    <div class="tt-body">
      <div class="tt-row"><span class="tt-label">Duration</span><span class="tt-val">${formatDuration(duration)}</span></div>
      ${reasonLine}
      ${resourceLine}
    </div>
  `;

  positionTooltip(clientX, clientY);
  elements.tooltip.classList.remove("hidden");
}

function hideTooltip() {
  elements.tooltip.classList.add("hidden");
}

function positionTooltip(clientX, clientY) {
  const tip = elements.tooltip;
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const offsetX = 14;
  const offsetY = 12;

  // Reset position so getBoundingClientRect reflects natural size.
  tip.style.left = "0px";
  tip.style.top = "0px";

  const tipWidth = tip.offsetWidth;
  const tipHeight = tip.offsetHeight;

  let left = clientX + offsetX;
  let top = clientY + offsetY;

  if (left + tipWidth > viewportWidth - 8) {
    left = clientX - tipWidth - offsetX;
  }
  if (top + tipHeight > viewportHeight - 8) {
    top = clientY - tipHeight - offsetY;
  }

  tip.style.left = `${Math.max(8, left)}px`;
  tip.style.top = `${Math.max(8, top)}px`;
}

// ─── Misc rendering ───────────────────────────────────────────────────────

function renderError(message) {
  elements.goroutineList.innerHTML = `<div class="empty-message">${escapeHTML(message)}</div>`;
  if (elements.lanePriority) {
    elements.lanePriority.hidden = true;
    elements.lanePriority.innerHTML = "";
  }
  elements.inspector.innerHTML = `<div class="empty-message">${escapeHTML(message)}</div>`;
  elements.resourceList.innerHTML = "";
  if (elements.timelineContext) {
    elements.timelineContext.textContent = message;
  }
  hideTimelineCursor();
  canvasContext.clearRect(0, 0, elements.timelineCanvas.width, elements.timelineCanvas.height);
}

// ─── Utilities ────────────────────────────────────────────────────────────

async function fetchJSON(url) {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Request failed: ${url}`);
  }

  return response.json();
}

function formatTimestamp(value) {
  if (!value) {
    return "n/a";
  }

  const date = new Date(value);
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date);
}

function formatDuration(value) {
  const nanoseconds = Number(value);
  if (!Number.isFinite(nanoseconds) || nanoseconds <= 0) {
    return "0ms";
  }

  if (nanoseconds >= 1_000_000_000) {
    return `${(nanoseconds / 1_000_000_000).toFixed(2)}s`;
  }
  if (nanoseconds >= 1_000_000) {
    return `${(nanoseconds / 1_000_000).toFixed(0)}ms`;
  }
  if (nanoseconds >= 1_000) {
    return `${(nanoseconds / 1_000).toFixed(0)}µs`;
  }
  return `${nanoseconds}ns`;
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function truncateCanvasText(context, value, maxWidth) {
  const text = String(value ?? "");
  if (!text || context.measureText(text).width <= maxWidth) {
    return text;
  }

  for (let end = text.length - 1; end > 0; end -= 1) {
    const candidate = `${text.slice(0, end)}…`;
    if (context.measureText(candidate).width <= maxWidth) {
      return candidate;
    }
  }

  return "…";
}

function roundRect(context, x, y, width, height, radius) {
  context.beginPath();
  context.moveTo(x + radius, y);
  context.arcTo(x + width, y, x + width, y + height, radius);
  context.arcTo(x + width, y + height, x, y + height, radius);
  context.arcTo(x, y + height, x, y, radius);
  context.arcTo(x, y, x + width, y, radius);
  context.closePath();
}

// ─── Minimap ───────────────────────────────────────────────────────────────

// seekMinimapToEvent pans the main timeline so the clicked NS position in the
// minimap is centered in the visible viewport.
function seekMinimapToEvent(event) {
  const canvas = elements.minimapCanvas;
  if (!canvas) {
    return;
  }

  const { fullSpan, span: visibleSpan } = timelineCache;
  if (fullSpan <= 1) {
    return;
  }

  const rect = canvas.getBoundingClientRect();
  const mmPad = 4;
  const innerWidth = rect.width - mmPad * 2;
  const fx = Math.max(0, Math.min(1, (event.clientX - rect.left - mmPad) / innerWidth));

  // Center the visible window on the clicked position.
  let newPan = fx * fullSpan - visibleSpan / 2;
  newPan = Math.max(0, Math.min(fullSpan - visibleSpan, newPan));
  timelineView.panOffsetNS = newPan;

  renderTimeline();
}

// ─── Heatmap view ─────────────────────────────────────────────────────────

// goroutineHue returns a deterministic hue (0–360) for a goroutine ID so each
// goroutine has a stable, distinguishable colour in the GMP P-lane strip.
function goroutineHue(id) {
  // 12 evenly-spaced hues with good perceptual separation on a dark background.
  const hues = [195, 30, 270, 140, 355, 60, 310, 170, 80, 230, 15, 330];
  return hues[Number(id) % hues.length];
}

// buildSegmentIndex returns a Map<goroutineID → sorted TimelineSegment[]> for
// the current goroutine set.  Used by both renderHeatmap and hit-testing.
function buildSegmentIndex(goroutines, timeline) {
  const byID = new Map();
  for (const g of goroutines) {
    byID.set(g.goroutine_id, []);
  }
  for (const seg of timeline) {
    const list = byID.get(seg.goroutine_id);
    if (list) list.push(seg);
  }
  // Segments arrive pre-sorted from the engine but sort defensively.
  for (const list of byID.values()) {
    list.sort((a, b) => a.start_ns - b.start_ns);
  }
  return byID;
}

// renderHeatmap draws the pixel-style heatmap with a GMP processor strip above
// and one row per goroutine below, each coloured by state at every time slice.
function renderHeatmap() {
  if (!heatmapContext || !elements.heatmapCanvas) return;

  const goroutines = getFilteredGoroutines();
  const timeline = state.timeline;
  const processorSegs = state.processorTimeline;

  // ── Geometry ──────────────────────────────────────────────────────────────
  const dpr = window.devicePixelRatio || 1;
  const width = Math.max(320, elements.heatmapCanvas.parentElement.clientWidth);

  // Determine the set of active processors.
  const processorIDs = [...new Set(processorSegs.map((s) => s.processor_id))].sort((a, b) => a - b);
  const numPs = processorIDs.length;

  const axisHeight  = 38;
  const pRowH       = 18;    // height of each P lane
  const pLabelH     = 14;    // header row above P lanes ("GMP" section label)
  const pGap        = 2;     // gap between P lanes and G heatmap
  const gmpH        = numPs > 0 ? pLabelH + numPs * pRowH + pGap + 8 : 0;
  const gRowH       = 14;    // height of each goroutine heatmap row
  const labelW      = 58;    // narrow label for heatmap (just "G<id>")
  const rightPad    = 18;
  const plotLeft    = labelW;
  const innerWidth  = Math.max(1, width - plotLeft - rightPad);
  const totalHeight = Math.max(220, axisHeight + gmpH + goroutines.length * gRowH + 16);

  const hmNewW = Math.floor(width * dpr);
  const hmNewH = Math.floor(totalHeight * dpr);
  if (elements.heatmapCanvas.width  !== hmNewW) elements.heatmapCanvas.width  = hmNewW;
  if (elements.heatmapCanvas.height !== hmNewH) elements.heatmapCanvas.height = hmNewH;
  const hmSW = `${width}px`, hmSH = `${totalHeight}px`;
  if (elements.heatmapCanvas.style.width  !== hmSW) elements.heatmapCanvas.style.width  = hmSW;
  if (elements.heatmapCanvas.style.height !== hmSH) elements.heatmapCanvas.style.height = hmSH;
  heatmapContext.setTransform(dpr, 0, 0, dpr, 0, 0);

  // ── Background ────────────────────────────────────────────────────────────
  heatmapContext.fillStyle = "#0d1117";
  heatmapContext.fillRect(0, 0, width, totalHeight);

  // ── Visible time window (same zoom/pan as lane view) ─────────────────────
  if (timeline.length === 0) {
    heatmapContext.fillStyle = "rgba(219,228,238,0.35)";
    heatmapContext.font = '13px "IBM Plex Mono", monospace';
    heatmapContext.fillText("No trace data.", plotLeft + 16, axisHeight + 40);
    return;
  }

  const allNS    = timeline.flatMap((s) => [s.start_ns, s.end_ns]);
  const fullMin  = Math.min(...allNS);
  const fullMax  = Math.max(...allNS);
  const fullSpan = Math.max(fullMax - fullMin, 1);

  const visibleSpan  = fullSpan / timelineView.zoomLevel;
  const visibleStart = fullMin + timelineView.panOffsetNS;
  const visibleEnd   = visibleStart + visibleSpan;

  // ── Time axis ────────────────────────────────────────────────────────────
  heatmapContext.strokeStyle = "rgba(219,228,238,0.14)";
  heatmapContext.beginPath();
  heatmapContext.moveTo(plotLeft, axisHeight - 10);
  heatmapContext.lineTo(plotLeft + innerWidth, axisHeight - 10);
  heatmapContext.stroke();

  heatmapContext.fillStyle = "rgba(219,228,238,0.55)";
  heatmapContext.font = '10px "IBM Plex Mono", monospace';
  for (let i = 0; i < 5; i++) {
    const ratio = i / 4;
    const ns    = visibleStart + ratio * visibleSpan;
    const x     = plotLeft + ratio * innerWidth;
    const label = formatDuration(ns - fullMin);
    heatmapContext.fillText(label, x - (i === 4 ? heatmapContext.measureText(label).width : 0), axisHeight - 14);
    heatmapContext.strokeStyle = "rgba(219,228,238,0.08)";
    heatmapContext.beginPath();
    heatmapContext.moveTo(x, axisHeight - 10);
    heatmapContext.lineTo(x, totalHeight - 8);
    heatmapContext.stroke();
  }

  // ── GMP strip (P lanes) ──────────────────────────────────────────────────
  if (numPs > 0) {
    const gmpTop = axisHeight + 6;

    // Section label — drawn in its own header row above the P lanes
    heatmapContext.fillStyle = "rgba(219,228,238,0.38)";
    heatmapContext.font = '10px "IBM Plex Mono", monospace';
    heatmapContext.fillText("GMP", 4, gmpTop + 10);

    const pLanesTop = gmpTop + pLabelH;

    processorIDs.forEach((pid, pIdx) => {
      const py = pLanesTop + pIdx * pRowH;

      // P lane background
      heatmapContext.fillStyle = "rgba(255,255,255,0.025)";
      heatmapContext.fillRect(plotLeft, py, innerWidth, pRowH - 1);

      // P label
      heatmapContext.fillStyle = "rgba(219,228,238,0.50)";
      heatmapContext.font = '10px "IBM Plex Mono", monospace';
      heatmapContext.fillText(`P${pid}`, plotLeft - 54, py + 11);

      // Draw each processor segment for this P
      for (const seg of processorSegs) {
        if (seg.processor_id !== pid) continue;

        const rawX  = plotLeft + ((seg.start_ns - visibleStart) / visibleSpan) * innerWidth;
        const rawX2 = plotLeft + ((seg.end_ns   - visibleStart) / visibleSpan) * innerWidth;
        const cx    = Math.max(plotLeft, Math.min(rawX,  plotLeft + innerWidth));
        const cx2   = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
        const cw    = Math.max(cx2 - cx, rawX2 > rawX ? 1 : 0);
        if (cw === 0) continue;

        const hue  = goroutineHue(seg.goroutine_id);
        heatmapContext.fillStyle = `hsl(${hue}, 70%, 58%)`;
        heatmapContext.fillRect(cx, py + 1, cw, pRowH - 3);

        // 1 px bright top edge
        if (cw > 2) {
          heatmapContext.fillStyle = "rgba(255,255,255,0.20)";
          heatmapContext.fillRect(cx + 1, py + 1, cw - 2, 1);
        }

        // Label the goroutine ID if there's room
        if (cw > 28) {
          heatmapContext.fillStyle = "rgba(255,255,255,0.90)";
          heatmapContext.font = '9px "IBM Plex Mono", monospace';
          heatmapContext.fillText(`G${seg.goroutine_id}`, cx + 3, py + 11);
        }
      }

      // Separator
      heatmapContext.strokeStyle = "rgba(219,228,238,0.06)";
      heatmapContext.beginPath();
      heatmapContext.moveTo(plotLeft, py + pRowH - 0.5);
      heatmapContext.lineTo(plotLeft + innerWidth, py + pRowH - 0.5);
      heatmapContext.stroke();
    });
  }

  // ── Goroutine heatmap rows ────────────────────────────────────────────────
  const segIndex  = buildSegmentIndex(goroutines, timeline);
  const gTop      = axisHeight + gmpH;

  // Section label
  heatmapContext.fillStyle = "rgba(219,228,238,0.38)";
  heatmapContext.font = '10px "IBM Plex Mono", monospace';
  heatmapContext.fillText("Goroutines", 2, gTop + 10);

  goroutines.forEach((g, idx) => {
    const y    = gTop + idx * gRowH;
    const segs = segIndex.get(g.goroutine_id) || [];

    // Zebra
    if (idx % 2 === 0) {
      heatmapContext.fillStyle = "rgba(255,255,255,0.022)";
      heatmapContext.fillRect(0, y, width, gRowH);
    }

    // Row label
    const isSelected = g.goroutine_id === state.selectedId;
    heatmapContext.fillStyle = isSelected ? "#f8fafc" : "rgba(219,228,238,0.60)";
    heatmapContext.font = '10px "IBM Plex Mono", monospace';
    heatmapContext.fillText(`G${g.goroutine_id}`, 4, y + 10);

    // Selected accent bar
    if (isSelected) {
      heatmapContext.fillStyle = "rgba(96,165,250,0.12)";
      heatmapContext.fillRect(0, y, width, gRowH);
      heatmapContext.fillStyle = "rgba(125,211,252,0.95)";
      heatmapContext.fillRect(0, y, 3, gRowH);
    }

    // Draw state segments as coloured strips
    for (const seg of segs) {
      const rawX  = plotLeft + ((seg.start_ns - visibleStart) / visibleSpan) * innerWidth;
      const rawX2 = plotLeft + ((seg.end_ns   - visibleStart) / visibleSpan) * innerWidth;
      const cx    = Math.max(plotLeft, Math.min(rawX,  plotLeft + innerWidth));
      const cx2   = Math.max(plotLeft, Math.min(rawX2, plotLeft + innerWidth));
      const cw    = Math.max(cx2 - cx, rawX2 > rawX ? 1 : 0);
      if (cw === 0) continue;

      heatmapContext.fillStyle = colors[seg.state] ?? "#94a3b8";
      heatmapContext.fillRect(cx, y + 1, cw, gRowH - 2);
    }

    // Row separator
    heatmapContext.strokeStyle = "rgba(219,228,238,0.07)";
    heatmapContext.beginPath();
    heatmapContext.moveTo(0, y + gRowH - 0.5);
    heatmapContext.lineTo(width, y + gRowH - 0.5);
    heatmapContext.stroke();
  });

  // ── Gutter divider ────────────────────────────────────────────────────────
  heatmapContext.fillStyle = "rgba(2,6,23,0.45)";
  heatmapContext.fillRect(0, axisHeight, plotLeft - 2, totalHeight - axisHeight);
  heatmapContext.strokeStyle = "rgba(219,228,238,0.10)";
  heatmapContext.beginPath();
  heatmapContext.moveTo(plotLeft - 0.5, axisHeight - 18);
  heatmapContext.lineTo(plotLeft - 0.5, totalHeight - 8);
  heatmapContext.stroke();
}

// ─── Minimap ──────────────────────────────────────────────────────────────

// renderMinimap draws a compact overview of the full trace and highlights the
// current visible viewport.  Called at the end of every renderTimeline() call.
function renderMinimap() {
  const canvas = elements.minimapCanvas;
  if (!canvas || !minimapContext) {
    return;
  }

  const { goroutines, timeline, fullMinStart, fullSpan, span: visibleSpan } = timelineCache;
  const focus = getRelatedFocus();

  // Only show the minimap when zoomed in — at zoomLevel=1 it is redundant.
  if (!timelineCache.metrics || fullSpan <= 1 || timelineView.zoomLevel <= 1.05) {
    canvas.hidden = true;
    return;
  }

  canvas.hidden = false;

  const dpr = window.devicePixelRatio || 1;
  const width = Math.max(320, canvas.parentElement.clientWidth);
  const rowHeight = 6;
  const topPad = 6;
  const botPad = 6;
  const height = topPad + goroutines.length * rowHeight + botPad;

  canvas.width = Math.floor(width * dpr);
  canvas.height = Math.floor(height * dpr);
  canvas.style.width = `${width}px`;
  canvas.style.height = `${height}px`;

  minimapContext.setTransform(dpr, 0, 0, dpr, 0, 0);
  minimapContext.clearRect(0, 0, width, height);

  minimapContext.fillStyle = "#0d1526";
  minimapContext.fillRect(0, 0, width, height);

  const mmPad = 4;
  const innerWidth = width - mmPad * 2;

  // Draw all goroutine segments at full-trace scale.
  for (let rowIdx = 0; rowIdx < goroutines.length; rowIdx += 1) {
    const goroutine = goroutines[rowIdx];
    const isDimmed = focus.enabled && !focus.rolesByID.has(goroutine.goroutine_id);
    const y = topPad + rowIdx * rowHeight;
    const barH = rowHeight - 1;

    minimapContext.save();
    minimapContext.globalAlpha = isDimmed ? 0.14 : 1;
    for (const seg of timeline) {
      if (seg.goroutine_id !== goroutine.goroutine_id) {
        continue;
      }

      const x = mmPad + ((seg.start_ns - fullMinStart) / fullSpan) * innerWidth;
      const x2 = mmPad + ((seg.end_ns - fullMinStart) / fullSpan) * innerWidth;
      const barWidth = Math.max(x2 - x, 1);

      minimapContext.fillStyle = colors[seg.state] ?? "#94a3b8";
      minimapContext.fillRect(x, y, barWidth, barH);
    }
    minimapContext.restore();
  }

  // Darken the areas outside the current viewport.
  const vpX = mmPad + (timelineView.panOffsetNS / fullSpan) * innerWidth;
  const vpW = Math.max((visibleSpan / fullSpan) * innerWidth, 4);

  minimapContext.fillStyle = "rgba(0, 0, 0, 0.52)";
  // Left shade
  if (vpX > mmPad) {
    minimapContext.fillRect(mmPad, 0, vpX - mmPad, height);
  }
  // Right shade
  const vpRight = vpX + vpW;
  const drawableRight = mmPad + innerWidth;
  if (vpRight < drawableRight) {
    minimapContext.fillRect(vpRight, 0, drawableRight - vpRight, height);
  }

  // Viewport border — a bright rect with semi-transparent fill.
  minimapContext.fillStyle = "rgba(255, 255, 255, 0.07)";
  minimapContext.fillRect(vpX, 0, vpW, height);
  minimapContext.strokeStyle = "rgba(255, 255, 255, 0.60)";
  minimapContext.lineWidth = 1.5;
  minimapContext.strokeRect(vpX + 0.75, 0.75, vpW - 1.5, height - 1.5);
}

// ─── SSE live stream ───────────────────────────────────────────────────────

// connectStream opens an SSE connection to /api/v1/stream and calls loadData
// whenever the server pushes an "update" event. On error it retries after a
// short back-off so the UI recovers automatically if the server restarts.
// A 30-second safety-net interval is kept as a last-resort fallback in case
// the browser silently drops the SSE connection without firing an error event.
function connectStream() {
  const source = new EventSource("/api/v1/stream");
  let fallbackTimer = window.setInterval(loadData, 30_000);

  source.addEventListener("connected", () => {
    loadData();
  });

  source.addEventListener("update", () => {
    loadData();
  });

  source.onerror = () => {
    source.close();
    window.clearInterval(fallbackTimer);
    loadData();
    window.setTimeout(connectStream, 3_000);
  };
}

connectStream();
