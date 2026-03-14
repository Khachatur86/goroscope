const state = {
  session: null,
  sessions: [],
  goroutines: [],
  timeline: [],
  resources: [],
  selectedId: null,
  selectedGoroutine: null,
  search: "",
  stateFilter: "ALL",
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

const colors = {
  RUNNING: "#2a9d8f",
  RUNNABLE: "#6b7280",
  WAITING: "#f4a261",
  BLOCKED: "#d1495b",
  SYSCALL: "#457b9d",
  DONE: "#4b5563",
};

const elements = {
  refreshButton: document.getElementById("refresh-button"),
  sessionName: document.getElementById("session-name"),
  sessionTarget: document.getElementById("session-target"),
  sessionStatus: document.getElementById("session-status"),
  sessionStarted: document.getElementById("session-started"),
  goroutineCount: document.getElementById("goroutine-count"),
  blockedCount: document.getElementById("blocked-count"),
  searchInput: document.getElementById("search-input"),
  stateFilter: document.getElementById("state-filter"),
  goroutineList: document.getElementById("goroutine-list"),
  timelineCanvas: document.getElementById("timeline-canvas"),
  timelineRange: document.getElementById("timeline-range"),
  resetZoomButton: document.getElementById("reset-zoom-button"),
  inspector: document.getElementById("inspector"),
  resourceList: document.getElementById("resource-list"),
  tooltip: document.getElementById("timeline-tooltip"),
  sessionHistory: document.getElementById("session-history"),
  minimapCanvas: document.getElementById("minimap-canvas"),
};

const canvasContext = elements.timelineCanvas.getContext("2d");
const minimapContext = elements.minimapCanvas ? elements.minimapCanvas.getContext("2d") : null;

// ─── Control event listeners ───────────────────────────────────────────────

elements.refreshButton.addEventListener("click", () => {
  loadData();
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

if (elements.resetZoomButton) {
  elements.resetZoomButton.addEventListener("click", () => {
    timelineView.zoomLevel = 1;
    timelineView.panOffsetNS = 0;
    renderTimeline();
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
  const innerWidth = width - metrics.horizontalPadding * 2;

  // Fraction [0, 1] of the inner drawing area where the cursor sits.
  const fx = Math.max(0, Math.min(1, (canvasX - metrics.horizontalPadding) / innerWidth));

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
      const innerWidth = width - metrics.horizontalPadding * 2;
      const visibleSpan = fullSpan / timelineView.zoomLevel;
      const dNS = -(dx / innerWidth) * visibleSpan;
      let newPan = timelineView.dragStartPanNS + dNS;
      newPan = Math.max(0, Math.min(fullSpan - visibleSpan, newPan));
      timelineView.panOffsetNS = newPan;
      renderTimeline();
    }

    elements.timelineCanvas.style.cursor = "grabbing";
    hideTooltip();
    return;
  }

  const rect = elements.timelineCanvas.getBoundingClientRect();
  const canvasX = event.clientX - rect.left;
  const canvasY = event.clientY - rect.top;
  const hoveredRow = getTimelineRowAt(canvasY);
  const hit = getSegmentAt(canvasX, canvasY);
  setTimelineHighlight(hoveredRow ? hoveredRow.goroutine_id : null, hit ? buildSegmentKey(hit.segment) : "");

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
  hideTooltip();
});

window.addEventListener("resize", () => {
  renderTimeline();
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

async function loadData() {
  try {
    const [session, goroutines, timeline, resources, sessions] = await Promise.all([
      fetchJSON("/api/v1/session/current"),
      fetchJSON("/api/v1/goroutines"),
      fetchJSON("/api/v1/timeline"),
      fetchJSON("/api/v1/resources/graph"),
      fetchJSON("/api/v1/sessions"),
    ]);

    state.session = session;
    state.goroutines = goroutines;
    state.timeline = timeline;
    state.resources = resources;
    state.sessions = Array.isArray(sessions) ? sessions : [];

    ensureSelection();
    await hydrateSelectedGoroutine();
    render();
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

function ensureSelection() {
  const filtered = getFilteredGoroutines();
  if (filtered.length === 0) {
    state.selectedId = null;
    state.selectedGoroutine = null;
    return;
  }

  const selectedStillVisible = filtered.some((item) => item.goroutine_id === state.selectedId);
  if (selectedStillVisible) {
    return;
  }

  const preferred = filtered.find((item) => item.state === "BLOCKED" || item.state === "WAITING") ?? filtered[0];
  state.selectedId = preferred.goroutine_id;
  state.selectedGoroutine = preferred;
}

async function selectGoroutine(id) {
  state.selectedId = id;
  await hydrateSelectedGoroutine();
  render();
}

function getFilteredGoroutines() {
  return state.goroutines
    .filter((item) => state.stateFilter === "ALL" || item.state === state.stateFilter)
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
    })
    .sort((left, right) => left.goroutine_id - right.goroutine_id);
}

function getTimelineMetrics() {
  return {
    axisHeight: 38,
    rowHeight: 36,
    horizontalPadding: 18,
  };
}

function buildSegmentKey(segment) {
  return `${segment.goroutine_id}:${segment.start_ns}:${segment.end_ns}:${segment.state}`;
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
  renderTimeline();
}

function clearTimelineHighlight() {
  setTimelineHighlight(null, "");
}

// ─── Render ────────────────────────────────────────────────────────────────

function render() {
  renderSummary();
  renderGoroutineList();
  renderInspector();
  renderResources();
  renderTimeline();
  renderSessionHistory();
}

function renderSummary() {
  if (!state.session) {
    return;
  }

  const goroutines = getFilteredGoroutines();
  const blockedCount = state.goroutines.filter((item) => item.state === "BLOCKED" || item.state === "WAITING").length;
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
}

function renderGoroutineList() {
  const goroutines = getFilteredGoroutines();

  if (goroutines.length === 0) {
    elements.goroutineList.innerHTML = `<div class="empty-message">No goroutines match the current filters.</div>`;
    return;
  }

  elements.goroutineList.innerHTML = "";
  for (const goroutine of goroutines) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `lane-item${goroutine.goroutine_id === state.selectedId ? " active" : ""}`;

    const waitBadge = goroutine.wait_ns > 0
      ? `<span class="wait-badge">${formatDuration(goroutine.wait_ns)}</span>`
      : "";

    button.innerHTML = `
      <div class="lane-item-header">
        <span class="lane-item-title">G${goroutine.goroutine_id}</span>
        <span class="state-pill ${goroutine.state}">${goroutine.state}</span>
      </div>
      <div class="lane-item-meta">
        <div class="lane-func">${escapeHTML(goroutine.labels?.function || "unknown function")}</div>
        <div class="lane-reason">${escapeHTML(goroutine.reason || "no active wait reason")} ${waitBadge}</div>
      </div>
    `;
    button.addEventListener("click", () => {
      selectGoroutine(goroutine.goroutine_id);
    });
    elements.goroutineList.appendChild(button);
  }
}

function renderInspector() {
  const goroutine = state.selectedGoroutine;
  if (!goroutine) {
    elements.inspector.innerHTML = `<div class="empty-message">Pick a goroutine to inspect its current state and stack trace.</div>`;
    return;
  }

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
  const metrics = getTimelineMetrics();
  const width = Math.max(320, elements.timelineCanvas.parentElement.clientWidth);
  const height = Math.max(220, metrics.axisHeight + goroutines.length * metrics.rowHeight + 16);
  const dpr = window.devicePixelRatio || 1;

  elements.timelineCanvas.width = Math.floor(width * dpr);
  elements.timelineCanvas.height = Math.floor(height * dpr);
  elements.timelineCanvas.style.width = `${width}px`;
  elements.timelineCanvas.style.height = `${height}px`;

  canvasContext.setTransform(dpr, 0, 0, dpr, 0, 0);
  canvasContext.clearRect(0, 0, width, height);

  canvasContext.fillStyle = "#0f172a";
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
  elements.timelineRange.textContent = `${formatDuration(visibleSpan)} visible window${zoomText}`;

  updateZoomControls();

  const innerWidth = width - metrics.horizontalPadding * 2;

  drawAxis(visibleStart, visibleEnd, fullMinStart, width, metrics);

  goroutines.forEach((goroutine, index) => {
    const y = metrics.axisHeight + index * metrics.rowHeight;
    const isSelected = goroutine.goroutine_id === state.selectedId;
    const isHoveredRow = goroutine.goroutine_id === timelineHighlight.hoveredGoroutineID;

    if (isSelected) {
      canvasContext.fillStyle = "rgba(96, 165, 250, 0.10)";
      canvasContext.fillRect(0, y, width, metrics.rowHeight);
      canvasContext.fillStyle = "rgba(125, 211, 252, 0.95)";
      canvasContext.fillRect(0, y + 2, 4, metrics.rowHeight - 4);
    } else if (isHoveredRow) {
      canvasContext.fillStyle = "rgba(219, 228, 238, 0.06)";
      canvasContext.fillRect(0, y, width, metrics.rowHeight);
    }

    canvasContext.strokeStyle = "rgba(219, 228, 238, 0.08)";
    canvasContext.beginPath();
    canvasContext.moveTo(metrics.horizontalPadding, y + metrics.rowHeight - 0.5);
    canvasContext.lineTo(width - metrics.horizontalPadding, y + metrics.rowHeight - 0.5);
    canvasContext.stroke();

    canvasContext.fillStyle = isSelected ? "#f8fafc" : isHoveredRow ? "#dbe4ee" : "#9fb3c8";
    canvasContext.font = '12px "IBM Plex Mono", monospace';
    canvasContext.fillText(`G${goroutine.goroutine_id}`, metrics.horizontalPadding, y + 22);

    timeline
      .filter((segment) => segment.goroutine_id === goroutine.goroutine_id)
      .forEach((segment) => {
        const isHoveredSegment = buildSegmentKey(segment) === timelineHighlight.hoveredSegmentKey;
        // Map segment to canvas X coordinates using the visible window.
        const rawX = metrics.horizontalPadding + ((segment.start_ns - visibleStart) / visibleSpan) * innerWidth;
        const rawX2 = metrics.horizontalPadding + ((segment.end_ns - visibleStart) / visibleSpan) * innerWidth;

        // Clip to the drawable area — segments may extend outside the visible window.
        const clampedX = Math.max(metrics.horizontalPadding, Math.min(rawX, metrics.horizontalPadding + innerWidth));
        const clampedX2 = Math.max(metrics.horizontalPadding, Math.min(rawX2, metrics.horizontalPadding + innerWidth));

        const barWidth = Math.max(clampedX2 - clampedX, clampedX2 > clampedX ? 2 : 0);
        if (barWidth === 0) {
          return;
        }

        const barHeight = 18;
        const barY = y + 9;

        canvasContext.save();
        roundRect(canvasContext, clampedX, barY, barWidth, barHeight, 7);
        canvasContext.fillStyle = colors[segment.state] ?? "#94a3b8";
        canvasContext.fill();
        if (isSelected || isHoveredSegment) {
          canvasContext.lineWidth = isHoveredSegment ? 2 : 1.5;
          canvasContext.strokeStyle = isHoveredSegment
            ? "rgba(255, 255, 255, 0.95)"
            : "rgba(186, 230, 253, 0.72)";
          canvasContext.stroke();
        }
        canvasContext.restore();

        if (barWidth > 78) {
          canvasContext.fillStyle = "rgba(255, 255, 255, 0.94)";
          canvasContext.font = '11px "IBM Plex Mono", monospace';
          canvasContext.fillText(segment.state, clampedX + 8, barY + 12);
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
  const innerWidth = width - metrics.horizontalPadding * 2;

  canvasContext.strokeStyle = "rgba(219, 228, 238, 0.14)";
  canvasContext.beginPath();
  canvasContext.moveTo(metrics.horizontalPadding, metrics.axisHeight - 10);
  canvasContext.lineTo(width - metrics.horizontalPadding, metrics.axisHeight - 10);
  canvasContext.stroke();

  for (let index = 0; index < ticks; index += 1) {
    const ratio = ticks === 1 ? 0 : index / (ticks - 1);
    const x = metrics.horizontalPadding + ratio * innerWidth;
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
  const innerWidth = width - metrics.horizontalPadding * 2;

  for (const seg of timeline) {
    if (seg.goroutine_id !== goroutine.goroutine_id) {
      continue;
    }

    // Map segment to canvas X using the visible window (same as renderTimeline).
    const rawX = metrics.horizontalPadding + ((seg.start_ns - minStart) / span) * innerWidth;
    const rawX2 = metrics.horizontalPadding + ((seg.end_ns - minStart) / span) * innerWidth;
    const segX = Math.max(metrics.horizontalPadding, Math.min(rawX, metrics.horizontalPadding + innerWidth));
    const segX2 = Math.max(metrics.horizontalPadding, Math.min(rawX2, metrics.horizontalPadding + innerWidth));
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
  elements.inspector.innerHTML = `<div class="empty-message">${escapeHTML(message)}</div>`;
  elements.resourceList.innerHTML = "";
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

// renderMinimap draws a compact overview of the full trace and highlights the
// current visible viewport.  Called at the end of every renderTimeline() call.
function renderMinimap() {
  const canvas = elements.minimapCanvas;
  if (!canvas || !minimapContext) {
    return;
  }

  const { goroutines, timeline, fullMinStart, fullSpan, span: visibleSpan } = timelineCache;

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
    const y = topPad + rowIdx * rowHeight;
    const barH = rowHeight - 1;

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
