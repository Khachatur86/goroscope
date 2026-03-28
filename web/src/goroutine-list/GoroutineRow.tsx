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

export function GoroutineRow({ index, style, data }: ListChildComponentProps<GoroutineRowData>) {
  const g = data.goroutines[index];
  const isPinned = data.pinned.has(g.goroutine_id);
  const note = data.pinned.get(g.goroutine_id) ?? "";
  return (
    <div style={style}>
      <button
        type="button"
        className={`lane-item ${data.selectedId === g.goroutine_id ? "active" : ""}${isPinned ? " pinned" : ""}`}
        onClick={() => data.onSelect(g.goroutine_id)}
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
      </button>
    </div>
  );
}
