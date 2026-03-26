export type Bookmark = {
  id: string;
  name: string;
  timeNS: number;
};

const LS_KEY = "goroscope_bookmarks";
const URL_PARAM = "bm";

export function loadBookmarks(): Bookmark[] {
  try {
    const params = new URLSearchParams(window.location.search);
    const urlParam = params.get(URL_PARAM);
    if (urlParam) return parseBookmarkParam(urlParam);
    const raw = localStorage.getItem(LS_KEY);
    return raw ? (JSON.parse(raw) as Bookmark[]) : [];
  } catch {
    return [];
  }
}

export function saveBookmarks(bookmarks: Bookmark[]): void {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(bookmarks));
    const params = new URLSearchParams(window.location.search);
    if (bookmarks.length > 0) {
      params.set(URL_PARAM, bookmarksToParam(bookmarks));
    } else {
      params.delete(URL_PARAM);
    }
    const search = params.toString();
    window.history.replaceState(
      null,
      "",
      search ? `${window.location.pathname}?${search}` : window.location.pathname
    );
  } catch {
    // ignore storage errors
  }
}

function bookmarksToParam(bookmarks: Bookmark[]): string {
  return bookmarks
    .map((b) => `${encodeURIComponent(b.name)}:${b.timeNS}`)
    .join(",");
}

function parseBookmarkParam(param: string): Bookmark[] {
  return param.split(",").flatMap((entry) => {
    const colon = entry.lastIndexOf(":");
    if (colon < 1) return [];
    const name = decodeURIComponent(entry.slice(0, colon));
    const timeNS = Number(entry.slice(colon + 1));
    if (!name || !Number.isFinite(timeNS)) return [];
    return [{ id: `bm_${timeNS}`, name, timeNS }];
  });
}
