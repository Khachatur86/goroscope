import * as vscode from "vscode";
import * as https from "https";
import * as http from "http";

interface Frame {
  func: string;
  file: string;
  line: number;
}

interface Goroutine {
  goroutine_id: number;
  state: string;
  wait_ns?: number;
  reason?: string;
  last_stack?: { frames: Frame[] };
  labels?: Record<string, string>;
}

interface GoroutinesResponse {
  goroutines: Goroutine[];
}

type StateKey = "BLOCKED" | "WAITING" | "SYSCALL" | "RUNNING";

const STATE_COLORS: Record<StateKey, { dark: string; light: string }> = {
  BLOCKED: { dark: "#f87171", light: "#dc2626" },
  WAITING: { dark: "#fbbf24", light: "#d97706" },
  SYSCALL: { dark: "#60a5fa", light: "#2563eb" },
  RUNNING: { dark: "#4ade80", light: "#16a34a" },
};

const POLL_INTERVAL_MS = 3000;

export class AnnotationController implements vscode.Disposable {
  private decorationTypes = new Map<string, vscode.TextEditorDecorationType>();
  private timer: NodeJS.Timeout | undefined;
  private enabled: boolean;
  private addr: string;

  constructor(addr: string, enabled: boolean) {
    this.addr = addr;
    this.enabled = enabled;
    this.buildDecorationTypes();
    if (this.enabled) {
      this.start();
    }
  }

  updateAddr(addr: string): void {
    this.addr = addr;
  }

  toggle(): void {
    this.enabled = !this.enabled;
    if (this.enabled) {
      this.start();
    } else {
      this.stop();
      this.clearAll();
    }
  }

  isEnabled(): boolean {
    return this.enabled;
  }

  private buildDecorationTypes(): void {
    for (const [state, colors] of Object.entries(STATE_COLORS)) {
      const dt = vscode.window.createTextEditorDecorationType({
        after: {
          margin: "0 0 0 1.5em",
          color: new vscode.ThemeColor("editorCodeLens.foreground"),
        },
        light: {
          after: { color: colors.light },
        },
        dark: {
          after: { color: colors.dark },
        },
        rangeBehavior: vscode.DecorationRangeBehavior.ClosedOpen,
      });
      this.decorationTypes.set(state, dt);
    }
  }

  private start(): void {
    if (this.timer) return;
    this.poll();
    this.timer = setInterval(() => this.poll(), POLL_INTERVAL_MS);
  }

  private stop(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  private clearAll(): void {
    const editors = vscode.window.visibleTextEditors;
    for (const [, dt] of this.decorationTypes) {
      for (const editor of editors) {
        editor.setDecorations(dt, []);
      }
    }
  }

  private poll(): void {
    this.fetchGoroutines()
      .then((goroutines) => this.applyDecorations(goroutines))
      .catch(() => {
        // Server not running yet — clear stale decorations silently
        this.clearAll();
      });
  }

  private fetchGoroutines(): Promise<Goroutine[]> {
    return new Promise((resolve, reject) => {
      const url = `http://${this.addr}/api/v1/goroutines?limit=500`;
      const transport = url.startsWith("https") ? https : http;
      const req = transport.get(url, { timeout: 2000 }, (res) => {
        if (res.statusCode !== 200) {
          res.resume();
          reject(new Error(`HTTP ${res.statusCode}`));
          return;
        }
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
        res.on("end", () => {
          try {
            const body = Buffer.concat(chunks).toString("utf8");
            const parsed = JSON.parse(body) as GoroutinesResponse;
            resolve(parsed.goroutines ?? []);
          } catch (e) {
            reject(e);
          }
        });
      });
      req.on("error", reject);
      req.on("timeout", () => {
        req.destroy();
        reject(new Error("timeout"));
      });
    });
  }

  private applyDecorations(goroutines: Goroutine[]): void {
    const editors = vscode.window.visibleTextEditors;
    if (!editors.length) return;

    // Group decorations by state then by file
    const byStateByFile = new Map<
      string,
      Map<string, vscode.DecorationOptions[]>
    >();

    for (const g of goroutines) {
      const frame = g.last_stack?.frames?.[0];
      if (!frame?.file) continue;

      const state = normalizeState(g.state);
      if (!this.decorationTypes.has(state)) continue;

      if (!byStateByFile.has(state)) {
        byStateByFile.set(state, new Map());
      }
      const byFile = byStateByFile.get(state)!;

      if (!byFile.has(frame.file)) {
        byFile.set(frame.file, []);
      }

      const line = Math.max(0, (frame.line ?? 1) - 1);
      const waitText = g.wait_ns ? ` ${formatNs(g.wait_ns)}` : "";
      const label = `← G${g.goroutine_id}${waitText}`;

      const hoverLines = [
        `**Goroutine ${g.goroutine_id}** — \`${state}\``,
        g.reason ? `Reason: \`${g.reason}\`` : "",
        g.wait_ns ? `Wait: ${formatNs(g.wait_ns)}` : "",
        frame.func ? `At: \`${frame.func}\`` : "",
      ]
        .filter(Boolean)
        .join("\n\n");

      byFile.get(frame.file)!.push({
        range: new vscode.Range(line, Number.MAX_SAFE_INTEGER, line, Number.MAX_SAFE_INTEGER),
        renderOptions: {
          after: { contentText: label },
        },
        hoverMessage: new vscode.MarkdownString(hoverLines),
      });
    }

    for (const editor of editors) {
      const filePath = editor.document.uri.fsPath;

      for (const [state, dt] of this.decorationTypes) {
        const fileMap = byStateByFile.get(state);
        const decorations = fileMap?.get(filePath) ?? [];
        editor.setDecorations(dt, decorations);
      }
    }
  }

  dispose(): void {
    this.stop();
    for (const [, dt] of this.decorationTypes) {
      dt.dispose();
    }
    this.decorationTypes.clear();
  }
}

function normalizeState(state: string): string {
  const upper = state.toUpperCase();
  if (upper.includes("BLOCK")) return "BLOCKED";
  if (upper.includes("WAIT")) return "WAITING";
  if (upper.includes("SYSCALL")) return "SYSCALL";
  if (upper.includes("RUN")) return "RUNNING";
  return upper;
}

function formatNs(ns: number): string {
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(1)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(1)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(0)}µs`;
  return `${ns}ns`;
}
