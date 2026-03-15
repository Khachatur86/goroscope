import * as vscode from "vscode";
import * as path from "path";
import * as childProcess from "child_process";
import * as fs from "fs";

const DEFAULT_ADDR = "127.0.0.1:7070";
let goroscopeProcess: childProcess.ChildProcess | null = null;

export function activate(context: vscode.ExtensionContext): void {
  context.subscriptions.push(
    vscode.commands.registerCommand("goroscope.runCurrentPackage", () =>
      runCurrentPackage()
    ),
    vscode.commands.registerCommand("goroscope.runSelectedMain", () =>
      runSelectedMain()
    ),
    vscode.commands.registerCommand("goroscope.attachToSession", () =>
      openTimeline()
    ),
    vscode.commands.registerCommand("goroscope.stopSession", stopSession),
    vscode.commands.registerCommand("goroscope.openTimeline", openTimeline)
  );
}

export function deactivate(): void {
  stopSession();
}

async function getGoroscopePath(): Promise<string | null> {
  const config = vscode.workspace.getConfiguration("goroscope");
  const binaryPath = config.get<string>("binaryPath");
  if (binaryPath && binaryPath.trim() !== "") {
    if (fs.existsSync(binaryPath)) {
      return binaryPath;
    }
    return null;
  }

  const workspaceFolders = vscode.workspace.workspaceFolders;
  if (workspaceFolders?.length) {
    const root = workspaceFolders[0].uri.fsPath;
    const workspaceBin = path.join(root, "bin", "goroscope");
    if (fs.existsSync(workspaceBin)) {
      return workspaceBin;
    }
  }

  return "goroscope";
}

async function runCurrentPackage(): Promise<void> {
  const workspaceFolders = vscode.workspace.workspaceFolders;
  const editor = vscode.window.activeTextEditor;
  let target = ".";

  if (editor && workspaceFolders?.length) {
    const root = workspaceFolders[0].uri.fsPath;
    const dir = path.dirname(editor.document.uri.fsPath);
    const relative = path.relative(root, dir);
    target = relative ? `./${relative}` : ".";
  }

  await runGoroscope(["run", target]);
}

async function runGoroscope(args: string[]): Promise<void> {
  const goroscopePath = await getGoroscopePath();
  if (!goroscopePath) {
    vscode.window.showErrorMessage(
      "Goroscope binary not found. Set goroscope.binaryPath or build with 'make build'."
    );
    return;
  }

  const config = vscode.workspace.getConfiguration("goroscope");
  const addr = config.get<string>("addr") ?? DEFAULT_ADDR;

  const fullArgs = [...args, "--addr", addr];
  const workspaceFolders = vscode.workspace.workspaceFolders;
  const cwd = workspaceFolders?.[0]?.uri.fsPath ?? process.cwd();

  goroscopeProcess = childProcess.spawn(goroscopePath, fullArgs, {
    cwd,
    stdio: ["ignore", "pipe", "pipe"],
  });

  goroscopeProcess.stdout?.on("data", (data) => {
    console.log(`[goroscope] ${data.toString().trim()}`);
  });
  goroscopeProcess.stderr?.on("data", (data) => {
    console.error(`[goroscope] ${data.toString().trim()}`);
  });
  goroscopeProcess.on("exit", (code) => {
    goroscopeProcess = null;
    if (code !== 0 && code !== null) {
      vscode.window.showWarningMessage(`Goroscope exited with code ${code}`);
    }
  });

  await vscode.commands.executeCommand("goroscope.openTimeline");
}

async function runSelectedMain(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    vscode.window.showErrorMessage("No file selected. Open a main.go or package.");
    return;
  }

  const doc = editor.document;
  const filePath = doc.uri.fsPath;
  const dir = path.dirname(filePath);

  const workspaceFolders = vscode.workspace.getWorkspaceFolder(doc.uri);
  const root = workspaceFolders?.uri.fsPath ?? path.dirname(filePath);

  const relativePath = path.relative(root, dir);
  const target = relativePath ? `./${relativePath}` : ".";

  await runGoroscope(["run", target]);
}

function stopSession(): void {
  if (goroscopeProcess) {
    goroscopeProcess.kill("SIGTERM");
    goroscopeProcess = null;
    vscode.window.showInformationMessage("Goroscope session stopped.");
  } else {
    vscode.window.showInformationMessage("No active Goroscope session.");
  }
}

function openTimeline(): void {
  const config = vscode.workspace.getConfiguration("goroscope");
  const addr = config.get<string>("addr") ?? DEFAULT_ADDR;
  const url = `http://${addr}`;

  const panel = vscode.window.createWebviewPanel(
    "goroscopeTimeline",
    "Goroscope Timeline",
    vscode.ViewColumn.One,
    {
      enableScripts: true,
      retainContextWhenHidden: true,
    }
  );

  panel.webview.html = getWebviewHtml(url);
}

function getWebviewHtml(apiUrl: string): string {
  const csp = `default-src 'none'; frame-src ${apiUrl};`;
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy" content="${csp}">
  <style>
    body, html { margin: 0; padding: 0; height: 100%; }
    iframe { width: 100%; height: 100%; border: none; }
  </style>
</head>
<body>
  <iframe src="${apiUrl}" title="Goroscope UI"></iframe>
</body>
</html>`;
}
