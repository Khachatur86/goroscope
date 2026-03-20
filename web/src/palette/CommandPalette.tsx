import {
  useEffect,
  useRef,
  useState,
  useCallback,
  useMemo,
} from "react";
import type { Goroutine } from "../api/client";

// ── Types ────────────────────────────────────────────────────────────────────

export interface Command {
  id: string;
  label: string;
  description?: string;
  keywords?: string[];
  action: () => void;
  /** Optional right-side hint (e.g. key binding shown in palette) */
  hint?: string;
  /** Icon character/emoji shown left of label */
  icon?: string;
  /** Group header label */
  group?: string;
}

interface Props {
  open: boolean;
  onClose: () => void;
  commands: Command[];
  goroutines: Goroutine[];
  onSelectGoroutine: (id: number) => void;
}

// ── Score a command against a query ─────────────────────────────────────────

function score(cmd: Command | GoroutineEntry, query: string): number {
  if (!query) return 1;
  const q = query.toLowerCase();
  const label = cmd.label.toLowerCase();
  const desc = ("description" in cmd && cmd.description ? cmd.description : "").toLowerCase();
  const kw = ("keywords" in cmd && cmd.keywords ? cmd.keywords.join(" ") : "").toLowerCase();
  if (label.startsWith(q)) return 100;
  if (label.includes(q)) return 80;
  if (desc.includes(q)) return 50;
  if (kw.includes(q)) return 30;
  return 0;
}

interface GoroutineEntry {
  kind: "goroutine";
  id: number;
  label: string;
  description: string;
  group: string;
}

type Entry =
  | (Command & { kind: "command" })
  | GoroutineEntry;

// ── Component ─────────────────────────────────────────────────────────────────

/** ⌘K command palette — fuzzy-search over commands + goroutines. */
export function CommandPalette({ open, onClose, commands, goroutines, onSelectGoroutine }: Props) {
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);

  // Reset state every time the palette opens.
  useEffect(() => {
    if (open) {
      setQuery("");
      setCursor(0);
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [open]);

  // Build goroutine entries.
  const goroutineEntries = useMemo<GoroutineEntry[]>(() =>
    goroutines.map((g) => ({
      kind: "goroutine",
      id: g.goroutine_id,
      label: `G${g.goroutine_id}`,
      description: [g.state, g.reason, g.resource_id].filter(Boolean).join(" · "),
      group: "Goroutines",
    })),
    [goroutines]
  );

  // Build tagged command entries.
  const commandEntries = useMemo<(Command & { kind: "command" })[]>(() =>
    commands.map((c) => ({ ...c, kind: "command" as const })),
    [commands]
  );

  // Merged + filtered list.
  const entries = useMemo<Entry[]>(() => {
    const all: Entry[] = [...commandEntries, ...goroutineEntries];
    if (!query.trim()) {
      // No query → show all commands, limit goroutines.
      const cmds = commandEntries;
      const gs = goroutineEntries.slice(0, 20);
      return [...cmds, ...gs];
    }
    return all
      .map((e) => ({ entry: e, s: score(e, query) }))
      .filter((x) => x.s > 0)
      .sort((a, b) => b.s - a.s)
      .slice(0, 30)
      .map((x) => x.entry);
  }, [query, commandEntries, goroutineEntries]);

  // Clamp cursor.
  useEffect(() => {
    setCursor((c) => Math.min(c, Math.max(0, entries.length - 1)));
  }, [entries.length]);

  // Scroll selected item into view.
  useEffect(() => {
    const li = listRef.current?.children[cursor] as HTMLElement | undefined;
    li?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const confirm = useCallback((idx: number) => {
    const e = entries[idx];
    if (!e) return;
    if (e.kind === "goroutine") {
      onSelectGoroutine(e.id);
    } else {
      e.action();
    }
    onClose();
  }, [entries, onSelectGoroutine, onClose]);

  const handleKey = useCallback((ev: React.KeyboardEvent) => {
    switch (ev.key) {
      case "ArrowDown":
        ev.preventDefault();
        setCursor((c) => Math.min(c + 1, entries.length - 1));
        break;
      case "ArrowUp":
        ev.preventDefault();
        setCursor((c) => Math.max(c - 1, 0));
        break;
      case "Enter":
        ev.preventDefault();
        confirm(cursor);
        break;
      case "Escape":
        ev.preventDefault();
        onClose();
        break;
    }
  }, [cursor, entries.length, confirm, onClose]);

  if (!open) return null;

  // Render entries grouped by group label.
  let lastGroup: string | undefined;

  return (
    <div className="palette-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="palette-modal" role="dialog" aria-label="Command palette">
        <div className="palette-search-row">
          <span className="palette-search-icon">⌘</span>
          <input
            ref={inputRef}
            className="palette-input"
            type="text"
            placeholder="Search commands or goroutines…"
            value={query}
            onChange={(e) => { setQuery(e.target.value); setCursor(0); }}
            onKeyDown={handleKey}
            spellCheck={false}
            autoComplete="off"
          />
          {query && (
            <button
              type="button"
              className="palette-clear"
              onClick={() => { setQuery(""); inputRef.current?.focus(); }}
              title="Clear"
            >
              ✕
            </button>
          )}
        </div>

        {entries.length === 0 ? (
          <div className="palette-empty">No results for "{query}"</div>
        ) : (
          <ul ref={listRef} className="palette-list" role="listbox">
            {entries.map((e, i) => {
              const group = e.kind === "goroutine" ? "Goroutines" : (e.group ?? "Commands");
              const showGroup = group !== lastGroup;
              lastGroup = group;
              const isActive = i === cursor;

              return (
                <>
                  {showGroup && (
                    <li key={`hdr-${group}`} className="palette-group-header">
                      {group}
                    </li>
                  )}
                  <li
                    key={e.kind === "goroutine" ? `g-${e.id}` : `c-${e.id}`}
                    role="option"
                    aria-selected={isActive}
                    className={`palette-item ${isActive ? "active" : ""}`}
                    onMouseEnter={() => setCursor(i)}
                    onMouseDown={(ev) => { ev.preventDefault(); confirm(i); }}
                  >
                    {e.kind === "command" && e.icon && (
                      <span className="palette-item-icon">{e.icon}</span>
                    )}
                    {e.kind === "goroutine" && (
                      <span className="palette-item-icon palette-item-icon--goroutine">G</span>
                    )}
                    <span className="palette-item-body">
                      <span className="palette-item-label">{e.label}</span>
                      {e.description && (
                        <span className="palette-item-desc">{e.description}</span>
                      )}
                    </span>
                    {e.kind === "command" && e.hint && (
                      <kbd className="palette-item-kbd">{e.hint}</kbd>
                    )}
                  </li>
                </>
              );
            })}
          </ul>
        )}

        <div className="palette-footer">
          <span><kbd>↑↓</kbd> navigate</span>
          <span><kbd>↵</kbd> select</span>
          <span><kbd>Esc</kbd> close</span>
        </div>
      </div>
    </div>
  );
}
