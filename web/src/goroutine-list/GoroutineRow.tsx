import { memo } from "react";
import type { ListChildComponentProps } from "react-window";
import type { Goroutine, TimelineSegment } from "../api/client";
import type { PinnedMap } from "./pinned";
import { LifetimeBar } from "./LifetimeBar";

/** Per-row equality check: only re-render when data relevant to *this* row changes.
 *
 * Without this, every call to loadSegmentsBatch causes segmentsByGoroutine to be
 * rebuilt as a new Map, which makes goroutineListItemData a new object reference,
 * which makes React.memo's default shallow-equal fail for ALL rows simultaneously —
 * even rows whose segments didn't change. The result is a full-list re-render on
 * every segment-batch response, which appears as visible flicker in the left panel.
 */
function areRowPropsEqual(
  prev: ListChildComponentProps<GoroutineRowData>,
  next: ListChildComponentProps<GoroutineRowData>,
): boolean {
  if (prev.index !== next.index) return false;
  const pd = prev.data, nd = next.data;
  const pg = pd.goroutines[prev.index];
  const ng = nd.goroutines[next.index];
  if (pg !== ng) return false; // goroutine identity or state changed
  if (!pg) return true;        // both undefined — row is out of range
  const id = pg.goroutine_id;
  return (
    // Selection: only matters for this row
    (pd.selectedId === id) === (nd.selectedId === id) &&
    // Lifetime-bar segments for this goroutine only
    pd.segmentsByGoroutine.get(id) === nd.segmentsByGoroutine.get(id) &&
    // Pin state for this goroutine
    pd.pinned.has(id) === nd.pinned.has(id) &&
    pd.pinned.get(id) === nd.pinned.get(id) &&
    // Stable callbacks (memoised in app.tsx via useCallback)
    pd.onSelect === nd.onSelect &&
    pd.onTogglePin === nd.onTogglePin
  );
}

export type GoroutineRowData = {
  goroutines: Goroutine[];
  selectedId: number | null;
  onSelect: (id: number) => void;
  segmentsByGoroutine: Map<number, TimelineSegment[]>;
  pinned: PinnedMap;
  onTogglePin: (id: number) => void;
};

// memo + areRowPropsEqual prevents re-renders when only unrelated goroutines'
// segments were updated (react-window still passes new props on every parent
// render, so the custom comparator is essential to short-circuit that).
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
}, areRowPropsEqual);
