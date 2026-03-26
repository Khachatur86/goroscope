import { useState } from "react";
import type { ThemeMode, AccentColor } from "./useTheme";
import { ACCENT_COLORS, applyTheme, loadTheme } from "./useTheme";

const MODES: { id: ThemeMode; label: string }[] = [
  { id: "dark",   label: "Dark"   },
  { id: "light",  label: "Light"  },
  { id: "system", label: "System" },
];

export function ThemeSwitcher() {
  const [open, setOpen] = useState(false);
  const { mode: initMode, accent: initAccent } = loadTheme();
  const [mode,   setMode]   = useState<ThemeMode>(initMode);
  const [accent, setAccent] = useState<AccentColor>(initAccent);

  const handleMode = (m: ThemeMode) => {
    setMode(m);
    applyTheme(m, accent);
  };

  const handleAccent = (a: AccentColor) => {
    setAccent(a);
    applyTheme(mode, a);
  };

  return (
    <div className="theme-switcher">
      <button
        type="button"
        className="theme-switcher-trigger action-button secondary"
        onClick={() => setOpen((v) => !v)}
        title="Theme settings"
        aria-expanded={open}
      >
        ◑
      </button>
      {open && (
        <>
          <div className="theme-switcher-backdrop" onClick={() => setOpen(false)} />
          <div className="theme-switcher-panel">
            <div className="theme-switcher-section-label">Theme</div>
            <div className="theme-switcher-modes">
              {MODES.map(({ id, label }) => (
                <button
                  key={id}
                  type="button"
                  className={`theme-mode-btn ${mode === id ? "active" : ""}`}
                  onClick={() => handleMode(id)}
                >
                  {label}
                </button>
              ))}
            </div>
            <div className="theme-switcher-section-label">Accent</div>
            <div className="theme-switcher-accents">
              {(Object.keys(ACCENT_COLORS) as AccentColor[]).map((a) => (
                <button
                  key={a}
                  type="button"
                  className={`theme-accent-btn ${accent === a ? "active" : ""}`}
                  style={{ background: ACCENT_COLORS[a] }}
                  title={a}
                  onClick={() => handleAccent(a)}
                  aria-label={a}
                />
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
