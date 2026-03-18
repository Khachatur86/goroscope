import { useState, useMemo } from "react";
import type { Goroutine } from "../api/client";

// ─── helpers ─────────────────────────────────────────────────────────────────

/** Walk parent_id links up to find ancestor chain: [root, ..., parent]. */
function buildAncestorChain(goroutine: Goroutine, all: Goroutine[]): Goroutine[] {
  const byId = new Map(all.map((g) => [g.goroutine_id, g]));
  const chain: Goroutine[] = [];
  let cur: Goroutine | undefined = goroutine;
  const visited = new Set<number>();
  while (cur?.parent_id && !visited.has(cur.goroutine_id)) {
    visited.add(cur.goroutine_id);
    const parent = byId.get(cur.parent_id);
    if (!parent) break;
    chain.unshift(parent);
    cur = parent;
  }
  return chain;
}

/** Collect all goroutine IDs in the subtree rooted at `id` (inclusive). */
export function collectDescendantIds(id: number, all: Goroutine[]): Set<number> {
  const childrenOf = new Map<number, Goroutine[]>();
  for (const g of all) {
    if (g.parent_id != null) {
      const list = childrenOf.get(g.parent_id) ?? [];
      list.push(g);
      childrenOf.set(g.parent_id, list);
    }
  }
  const result = new Set<number>();
  const stack = [id];
  while (stack.length > 0) {
    const cur = stack.pop()!;
    result.add(cur);
    const kids = childrenOf.get(cur) ?? [];
    for (const k of kids) stack.push(k.goroutine_id);
  }
  return result;
}

// ─── sub-components ───────────────────────────────────────────────────────────

type ChipProps = {
  goroutine: Goroutine;
  onSelect?: (id: number) => void;
  dimmed?: boolean;
};

function GChip({ goroutine, onSelect, dimmed }: ChipProps) {
  const label = goroutine.labels?.function
    ? goroutine.labels.function.replace(/^.*\//, "").replace(/\(.*\)/, "").trim() || `G${goroutine.goroutine_id}`
    : `G${goroutine.goroutine_id}`;
  return (
    <button
      type="button"
      className={`goroutine-chip spawn-tree-chip ${dimmed ? "spawn-tree-chip--dimmed" : ""}`}
      onClick={() => onSelect?.(goroutine.goroutine_id)}
      title={`G${goroutine.goroutine_id} — ${goroutine.labels?.function ?? goroutine.reason ?? goroutine.state}`}
    >
      <span className={`state-dot state-dot--${goroutine.state.toLowerCase()}`} />
      G{goroutine.goroutine_id}
      {label !== `G${goroutine.goroutine_id}` && (
        <span className="spawn-tree-chip-fn">{label}</span>
      )}
    </button>
  );
}

type NodeProps = {
  goroutine: Goroutine;
  allGoroutines: Goroutine[];
  depth: number;
  onSelect?: (id: number) => void;
  defaultExpanded?: boolean;
};

function SpawnTreeNode({ goroutine, allGoroutines, depth, onSelect, defaultExpanded = false }: NodeProps) {
  const [expanded, setExpanded] = useState(defaultExpanded || depth < 2);
  const children = useMemo(
    () => allGoroutines.filter((g) => g.parent_id === goroutine.goroutine_id),
    [allGoroutines, goroutine.goroutine_id]
  );
  const hasChildren = children.length > 0;

  return (
    <div className="spawn-node" style={{ paddingLeft: depth > 0 ? "1rem" : 0 }}>
      <div className="spawn-node-row">
        <button
          type="button"
          className={`spawn-node-expand ${hasChildren ? "" : "spawn-node-expand--leaf"}`}
          onClick={() => hasChildren && setExpanded((v) => !v)}
          aria-expanded={expanded}
          aria-label={hasChildren ? (expanded ? "Collapse" : "Expand") : undefined}
          tabIndex={hasChildren ? 0 : -1}
        >
          {hasChildren ? (expanded ? "▾" : "▸") : "·"}
        </button>
        <GChip goroutine={goroutine} onSelect={onSelect} />
        {hasChildren && (
          <span className="spawn-node-count">{children.length}</span>
        )}
      </div>
      {expanded && hasChildren && (
        <div className="spawn-node-children">
          {children.map((c) => (
            <SpawnTreeNode
              key={c.goroutine_id}
              goroutine={c}
              allGoroutines={allGoroutines}
              depth={depth + 1}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// ─── public component ────────────────────────────────────────────────────────

type Props = {
  goroutine: Goroutine;
  allGoroutines: Goroutine[];
  onSelectGoroutine?: (id: number) => void;
  onHighlightBranch?: (ids: Set<number> | null) => void;
  highlightActive?: boolean;
};

export function SpawnTree({
  goroutine,
  allGoroutines,
  onSelectGoroutine,
  onHighlightBranch,
  highlightActive = false,
}: Props) {
  const ancestors = useMemo(
    () => buildAncestorChain(goroutine, allGoroutines),
    [goroutine, allGoroutines]
  );

  const children = useMemo(
    () => allGoroutines.filter((g) => g.parent_id === goroutine.goroutine_id),
    [goroutine.goroutine_id, allGoroutines]
  );

  const hasTree = ancestors.length > 0 || children.length > 0;
  if (!hasTree) {
    return <div className="spawn-tree-empty">No spawn relationships found.</div>;
  }

  const handleHighlight = () => {
    if (highlightActive) {
      onHighlightBranch?.(null);
      return;
    }
    const descendantIds = collectDescendantIds(goroutine.goroutine_id, allGoroutines);
    const ancestorIds = new Set(ancestors.map((a) => a.goroutine_id));
    const branchIds = new Set([...descendantIds, ...ancestorIds]);
    onHighlightBranch?.(branchIds);
  };

  return (
    <div className="spawn-tree-v2">
      <div className="spawn-tree-v2-actions">
        {onHighlightBranch && (
          <button
            type="button"
            className={`spawn-tree-action-btn ${highlightActive ? "spawn-tree-action-btn--active" : ""}`}
            onClick={handleHighlight}
            title="Highlight this goroutine's full branch in the timeline"
          >
            {highlightActive ? "Clear highlight" : "Highlight branch"}
          </button>
        )}
      </div>

      {ancestors.length > 0 && (
        <div className="spawn-tree-section">
          <div className="spawn-tree-section-label">Ancestors (root → parent)</div>
          <div className="spawn-tree-ancestor-chain">
            {ancestors.map((a, i) => (
              <div key={a.goroutine_id} className="spawn-tree-ancestor-row">
                {i > 0 && <span className="spawn-tree-ancestor-arrow">↓</span>}
                <GChip goroutine={a} onSelect={onSelectGoroutine} />
              </div>
            ))}
            <div className="spawn-tree-ancestor-row spawn-tree-ancestor-row--self">
              <span className="spawn-tree-ancestor-arrow">↓</span>
              <GChip goroutine={goroutine} onSelect={onSelectGoroutine} />
              <span className="spawn-tree-self-badge">selected</span>
            </div>
          </div>
        </div>
      )}

      {children.length > 0 && (
        <div className="spawn-tree-section">
          <div className="spawn-tree-section-label">
            Descendants ({collectDescendantIds(goroutine.goroutine_id, allGoroutines).size - 1} total)
          </div>
          <div className="spawn-tree-descendants">
            {children.map((c) => (
              <SpawnTreeNode
                key={c.goroutine_id}
                goroutine={c}
                allGoroutines={allGoroutines}
                depth={0}
                onSelect={onSelectGoroutine}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
