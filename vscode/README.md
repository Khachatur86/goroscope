# Goroscope VS Code Extension

Runs the [Goroscope](https://github.com/Khachatur86/goroscope) concurrency debugger from VS Code and displays the timeline in a webview. Inspect goroutines, blocking behavior, and stack snapshots with click-to-open source navigation.

## Requirements

- Go workspace with `goroscope` binary in `bin/goroscope` or in PATH
- Build with `make build` from the project root, or `go install github.com/Khachatur86/goroscope/cmd/goroscope@latest`

## Commands

- **Goroscope: Run Current Package** — Run `goroscope run` for the package of the current file
- **Goroscope: Run Selected Main** — Run `goroscope run` for the directory of the active file
- **Goroscope: Attach to Current Session** — Open the timeline webview (connects to existing goroscope server)
- **Goroscope: Stop Session** — Stop the goroscope process started by the extension
- **Goroscope: Open Timeline** — Open the timeline webview

## Configuration

- `goroscope.addr` — HTTP address (default: `127.0.0.1:7070`)
- `goroscope.binaryPath` — Path to goroscope binary (default: workspace `bin/goroscope` or PATH)

## Development

```bash
cd vscode
npm install
npm run compile
```

Press F5 in VS Code to launch the Extension Development Host.

## Publishing

```bash
npm install -g @vscode/vsce
cd vscode
vsce package          # creates .vsix
vsce publish          # requires: vsce login goroscope + Azure DevOps PAT
```

For Open VSX: `npx ovsx publish -p <token>`
