import * as vscode from "vscode";
import * as https from "https";
import * as http from "http";

const DEFAULT_ADDR = "127.0.0.1:7070";

interface SessionInfo {
  id: string;
  name: string;
  target: string;
  status: string;
  started_at?: string;
  error?: string;
}

export class SessionPanelProvider implements vscode.TreeDataProvider<SessionTreeItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<SessionTreeItem | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private pollTimer: NodeJS.Timeout | null = null;
  private lastSession: SessionInfo | null = null;

  constructor() {
    this.startPolling();
  }

  dispose(): void {
    this.stopPolling();
  }

  private startPolling(): void {
    this.stopPolling();
    this.pollTimer = setInterval(() => {
      this.refresh();
    }, 2000);
  }

  private stopPolling(): void {
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
  }

  refresh(): void {
    this._onDidChangeTreeData.fire();
  }

  getTreeItem(element: SessionTreeItem): vscode.TreeItem {
    return element;
  }

  async getChildren(element?: SessionTreeItem): Promise<SessionTreeItem[]> {
    if (element) {
      return [];
    }

    const config = vscode.workspace.getConfiguration("goroscope");
    const addr = config.get<string>("addr") ?? DEFAULT_ADDR;

    try {
      const session = await fetchSession(addr);
      this.lastSession = session;

      if (!session) {
        return [
          new SessionTreeItem(
            "No active session",
            vscode.TreeItemCollapsibleState.None,
            "Run a target with Goroscope to see session info."
          ),
        ];
      }

      const items: SessionTreeItem[] = [
        new SessionTreeItem(`Session: ${session.name}`, vscode.TreeItemCollapsibleState.None, session.id),
        new SessionTreeItem(`Target: ${session.target}`, vscode.TreeItemCollapsibleState.None),
        new SessionTreeItem(`Status: ${session.status}`, vscode.TreeItemCollapsibleState.None),
      ];

      if (session.started_at) {
        items.push(
          new SessionTreeItem(`Started: ${session.started_at}`, vscode.TreeItemCollapsibleState.None)
        );
      }
      if (session.error) {
        items.push(
          new SessionTreeItem(`Error: ${session.error}`, vscode.TreeItemCollapsibleState.None, undefined, "error")
        );
      }

      return items;
    } catch {
      return [
        new SessionTreeItem(
          "Cannot connect to Goroscope",
          vscode.TreeItemCollapsibleState.None,
          undefined,
          "error"
        ),
      ];
    }
  }
}

class SessionTreeItem extends vscode.TreeItem {
  constructor(
    label: string,
    collapsibleState: vscode.TreeItemCollapsibleState,
    description?: string,
    icon?: "error"
  ) {
    super(label, collapsibleState);
    this.description = description;
    if (icon === "error") {
      this.iconPath = new vscode.ThemeIcon("error", new vscode.ThemeColor("errorForeground"));
    }
  }
}

async function fetchSession(addr: string): Promise<SessionInfo | null> {
  const url = `http://${addr}/api/v1/session/current`;
  return new Promise((resolve) => {
    const client = addr.startsWith("https") ? https : http;
    const req = client.get(url, (res) => {
      let data = "";
      res.on("data", (chunk) => {
        data += chunk;
      });
      res.on("end", () => {
        try {
          const json = JSON.parse(data);
          resolve({
            id: json.id ?? "",
            name: json.name ?? "",
            target: json.target ?? "",
            status: json.status ?? "unknown",
            started_at: json.started_at,
            error: json.error,
          });
        } catch {
          resolve(null);
        }
      });
    });
    req.on("error", () => resolve(null));
    req.setTimeout(2000, () => {
      req.destroy();
      resolve(null);
    });
  });
}
