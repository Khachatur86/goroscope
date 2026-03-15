# Goroscope Web (React)

React + TypeScript + Vite frontend for the Goroscope timeline UI.

## Development

**Option A — React on port 7070 (same as vanilla UI):**

```bash
make web
goroscope ui -ui=react -open-browser
```

Or use the convenience target: `make ui-react`

**Option B — Vite dev server (hot reload):**

```bash
# Terminal 1: goroscope with demo data
goroscope ui

# Terminal 2: Vite dev server with API proxy
cd web && npm run dev
```

Open http://localhost:5173 — Vite proxies `/api` and `/healthz` to the goroscope server.

## Build

```bash
npm run build
```

Output: `dist/`

## Preview

```bash
# Ensure goroscope is running first
goroscope ui

npm run build && npm run preview
```

Preview serves on port 4173 and proxies API requests to goroscope.
