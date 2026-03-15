# Goroscope Web (React)

React + TypeScript + Vite frontend for the Goroscope timeline UI.

## Development

```bash
# Start goroscope with demo data (in one terminal)
goroscope ui

# Start Vite dev server with API proxy (in another terminal)
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
