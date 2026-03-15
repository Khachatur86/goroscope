import { useRef, useCallback } from "react";

type Props = {
  fullSpan: number;
  visibleSpan: number;
  panOffsetNS: number;
  onPanChange: (panOffsetNS: number) => void;
};

export function MinimapCanvas({
  fullSpan,
  visibleSpan,
  panOffsetNS,
  onPanChange,
}: Props) {
  const trackRef = useRef<HTMLDivElement>(null);
  const dragStartX = useRef(0);
  const dragStartPan = useRef(0);
  const propsRef = useRef({ fullSpan, visibleSpan, onPanChange });
  propsRef.current = { fullSpan, visibleSpan, onPanChange };

  const handleViewportMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      e.preventDefault();
      e.stopPropagation();
      dragStartX.current = e.clientX;
      dragStartPan.current = panOffsetNS;

      const onMove = (ev: MouseEvent) => {
        const track = trackRef.current;
        if (!track) return;
        const trackRect = track.getBoundingClientRect();
        const dx = ev.clientX - dragStartX.current;
        const nsPerPx = propsRef.current.fullSpan / trackRect.width;
        const newPan = dragStartPan.current + dx * nsPerPx;
        const maxPan = Math.max(0, propsRef.current.fullSpan - propsRef.current.visibleSpan);
        propsRef.current.onPanChange(Math.max(0, Math.min(maxPan, newPan)));
      };

      const onUp = () => {
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
      };

      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
    },
    [panOffsetNS]
  );

  const handleTrackClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      const track = trackRef.current;
      if (!track || e.target !== track) return; // ignore clicks on viewport
      const rect = track.getBoundingClientRect();
      const clickX = e.clientX - rect.left;
      const ratio = Math.max(0, Math.min(1, clickX / rect.width));
      const maxPan = Math.max(0, fullSpan - visibleSpan);
      const newPan = ratio * maxPan;
      onPanChange(newPan);
    },
    [fullSpan, visibleSpan, onPanChange]
  );

  const leftPct = fullSpan > 0 ? (panOffsetNS / fullSpan) * 100 : 0;
  const widthPct = fullSpan > 0 ? (visibleSpan / fullSpan) * 100 : 100;

  return (
    <div
      className="timeline-minimap timeline-minimap-draggable"
      title="Click to jump, drag viewport to pan"
    >
      <div
        ref={trackRef}
        className="timeline-minimap-track"
        onClick={handleTrackClick}
        role="slider"
        aria-valuemin={0}
        aria-valuemax={fullSpan}
        aria-valuenow={panOffsetNS}
      >
        <div
          className="timeline-minimap-viewport"
          style={{
            left: `${leftPct}%`,
            width: `${widthPct}%`,
            cursor: "grab",
          }}
          onMouseDown={handleViewportMouseDown}
        />
      </div>
    </div>
  );
}
