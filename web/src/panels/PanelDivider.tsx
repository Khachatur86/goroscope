import { useState, useCallback } from "react";

const PANEL_MIN = 180;

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v));
}

export function usePanelResize(lsKey: string, defaultWidth: number) {
  const [width, setWidth] = useState<number>(() => {
    const stored = localStorage.getItem(lsKey);
    return stored ? Number(stored) : defaultWidth;
  });

  const startDrag = useCallback(
    (e: React.MouseEvent, side: "left" | "right") => {
      e.preventDefault();
      const startX = e.clientX;
      const startW = width;
      const onMove = (ev: MouseEvent) => {
        const delta = side === "right" ? ev.clientX - startX : startX - ev.clientX;
        setWidth(clamp(startW + delta, PANEL_MIN, 640));
      };
      const onUp = (ev: MouseEvent) => {
        const delta = side === "right" ? ev.clientX - startX : startX - ev.clientX;
        const next = clamp(startW + delta, PANEL_MIN, 640);
        localStorage.setItem(lsKey, String(next));
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
      };
      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
    },
    [width, lsKey]
  );

  return { width, startDrag };
}

/** Thin draggable strip between two panels. */
export function PanelDivider({ onMouseDown }: { onMouseDown: (e: React.MouseEvent) => void }) {
  return (
    <div
      className="panel-divider"
      onMouseDown={onMouseDown}
      role="separator"
      aria-orientation="vertical"
    />
  );
}
