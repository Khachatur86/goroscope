import { memo } from "react";
import type { ListChildComponentProps } from "react-window";
import type { Goroutine, TimelineSegment } from "../api/client";
import type { PinnedMap } from "./pinned";
import { LifetimeBar } from "./LifetimeBar";

export type GoroutineRowData = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelect: (id: number) => void;
  segmentsByGoroutine: Map<number, TimelineSegment[]>;
  pinned: PinnedMap;
  onTogglePin: (id: number) => void;
};

// memo prevents re-renders when itemData reference is stable and this row's
// slice of the data hasn't changed (react-window still passes new props when
// the parent re-renders, so memo is required to short-circuit that).
export const GoroutineRow = memo(function GoroutineRow({ index, style, data }: ListChildComponentProps<GoroutineRowData>) {
  const g = data.goroutines[index];
  const isPinned = data.pinned.has(g.goroutine_id);
  const note = data.pinned.get(g.goroutine_id) ?? "";
  return (
    <div style={style}>
      {/* Outer row uses div+role="button" to avoid invalid nested <button> DOM.
          pin-btn is a <button> so the outer must not be a <button> too. */}
      <div
        role="button"
        tabIndex={0}
        className={`lane-item ${data.selectedId === g.goroutine_id ? "active" : ""}${isPinned ? " pinned" : ""}`}
        onClick={() => data.onSelect(g.goroutine_id)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            data.onSelect(g.goroutine_id);
          }
        }}
      >
        <button
          type="button"
          className="pin-btn"
          title={isPinned ? "Unpin goroutine" : "Pin goroutine"}
          onClick={(e) => { e.stopPropagation(); data.onTogglePin(g.goroutine_id); }}
          aria-pressed={isPinned}
        >
          {isPinned ? "★" : "☆"}
        </button>
        <span className={`badge badge--state ${g.state}`}>{g.state}</span>
        <span className="lane-item-title">G{g.goroutine_id}</span>
        <span className="lane-item-meta">
          {note || (g.labels?.function ?? g.reason ?? "—")}
        </span>
        <LifetimeBar segments={data.segmentsByGoroutine.get(g.goroutine_id)} />
      </div>
    </div>
  );
});
