/**
 * tokens.ts — centralized design tokens for goroscope.
 *
 * Single source of truth for all colors used across components.
 * Import from this file instead of defining local color maps.
 */

/** Colors keyed by goroutine state string (RUNNING, BLOCKED, etc.). */
export const STATE_COLORS: Record<string, string> = {
  RUNNING:  "#10cfb8",
  RUNNABLE: "#8394a8",
  WAITING:  "#f59e0b",
  BLOCKED:  "#f43f5e",
  SYSCALL:  "#4da6ff",
  DONE:     "#4b5563",
};

/** Fallback color for unrecognised / unknown states. */
export const COLOR_UNKNOWN = "#94a3b8";

/** Colors for capture-diff status (appeared / disappeared / unchanged). */
export const DIFF_COLORS = {
  appeared:    "#22c55e",
  disappeared: "#f43f5e",
  unchanged:   null,        // use STATE_COLORS for unchanged nodes
} as const;

/** Semantic one-off colors shared across the UI. */
export const COLOR_ERROR   = "#f43f5e";
export const COLOR_WARNING = "#f59e0b";
export const COLOR_SUCCESS = "#22c55e";
export const COLOR_INFO    = "#4da6ff";

/**
 * Background palette — mirrors CSS vars in :root for use in canvas / SVG
 * contexts where CSS variables are not accessible.
 */
export const BG_BASE      = "#0f172a";  // --bg-base
export const BG_SECONDARY = "#0d1117";  // --bg-secondary
export const BG_PANEL     = "#0c1322";  // --bg-panel
export const BG_CARD      = "#1e293b";  // --bg-card

/** Text palette — mirrors CSS vars for canvas use. */
export const TEXT_PRIMARY   = "#e2e8f0";  // --text-primary
export const TEXT_SECONDARY = "#94a3b8";  // --text-secondary (= COLOR_UNKNOWN)
export const TEXT_MUTED     = "#64748b";  // --text-muted

/** Structural / UI utility colors. */
export const COLOR_AXIS_TEXT  = "#475569";  // axis labels, node IDs
export const COLOR_EDGE       = "#334155";  // graph edges (normal)
export const COLOR_EDGE_GONE  = "#7f1d1d";  // graph edges (disappeared)
export const COLOR_SELECTED   = "#f8fafc";  // selected node stroke / label
export const COLOR_SCRUBBER   = "#fbbf24";  // amber scrubber cursor
export const COLOR_RANGE      = "#38bdf8";  // sky-blue range highlight

/** Diff node label colours (light pastel — readable on dark fill). */
export const COLOR_DIFF_APPEARED_TEXT    = "#86efac";  // light green
export const COLOR_DIFF_DISAPPEARED_TEXT = "#fca5a5";  // light red
