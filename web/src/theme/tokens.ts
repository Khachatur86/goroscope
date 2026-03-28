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
