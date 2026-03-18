# Changelog

## [0.1.0] - 2026-03-18

### Added

- **Goroscope: Run Current Package** — Run `goroscope run` for the package of the current file
- **Goroscope: Run Selected Main** — Run `goroscope run` for the directory of the active file
- **Goroscope: Attach to Current Session** — Open the timeline webview (connects to existing goroscope server)
- **Goroscope: Stop Session** — Stop the goroscope process started by the extension
- **Goroscope: Open Timeline** — Open the timeline webview
- Session panel in the activity bar showing current session status
- Stack frame click opens source file in editor via `vscode.workspace.openTextDocument`
- Configuration: `goroscope.addr`, `goroscope.binaryPath`
