export const LS_PINNED = "goroscope:pinned";

export type PinnedMap = Map<number, string>; // goroutine_id → note (may be "")

export function loadPinned(): PinnedMap {
  try {
    const raw = localStorage.getItem(LS_PINNED);
    if (!raw) return new Map();
    const entries: [number, string][] = JSON.parse(raw);
    return new Map(entries);
  } catch {
    return new Map();
  }
}

export function savePinned(m: PinnedMap) {
  localStorage.setItem(LS_PINNED, JSON.stringify([...m.entries()]));
}
