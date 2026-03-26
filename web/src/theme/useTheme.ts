export type ThemeMode = "dark" | "light" | "system";
export type AccentColor = "teal" | "blue" | "amber" | "rose" | "purple" | "green";

export const ACCENT_COLORS: Record<AccentColor, string> = {
  teal:   "#10cfb8",
  blue:   "#60a5fa",
  amber:  "#f59e0b",
  rose:   "#f43f5e",
  purple: "#a78bfa",
  green:  "#34d399",
};

const LS_THEME  = "goroscope_theme";
const LS_ACCENT = "goroscope_accent";

function resolveMode(mode: ThemeMode): "dark" | "light" {
  if (mode === "system") {
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  return mode;
}

export function loadTheme(): { mode: ThemeMode; accent: AccentColor } {
  const mode   = (localStorage.getItem(LS_THEME)  as ThemeMode  | null) ?? "dark";
  const accent = (localStorage.getItem(LS_ACCENT) as AccentColor | null) ?? "teal";
  return { mode, accent };
}

export function applyTheme(mode: ThemeMode, accent: AccentColor): void {
  const resolved = resolveMode(mode);
  document.documentElement.setAttribute("data-theme", resolved);
  document.documentElement.setAttribute("data-accent", accent);
  document.documentElement.style.setProperty("--color-accent", ACCENT_COLORS[accent]);
  localStorage.setItem(LS_THEME, mode);
  localStorage.setItem(LS_ACCENT, accent);
}

/** Apply saved theme immediately (call at app startup, before first render). */
export function applyInitialTheme(): void {
  const { mode, accent } = loadTheme();
  applyTheme(mode, accent);
  // Re-apply if system preference changes.
  window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => {
    const { mode: m, accent: a } = loadTheme();
    if (m === "system") applyTheme("system", a);
  });
}
