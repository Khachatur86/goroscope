# Publishing Goroscope VS Code Extension

## Prerequisites

1. **Publisher account**: Create a publisher at [Visual Studio Marketplace](https://marketplace.visualstudio.com/manage)
   - Publisher ID: `goroscope` (or your choice; extension ID will be `publisher.name` = `goroscope.goroscope`)

2. **Azure DevOps PAT**: Create a Personal Access Token with **Marketplace (Publish)** scope at [Azure DevOps](https://dev.azure.com)

## Package

```bash
cd vscode
npm install
npm run package
# Creates goroscope-0.1.0.vsix
```

## Publish to VS Code Marketplace

```bash
npm install -g @vscode/vsce
vsce login goroscope   # Use your Azure DevOps PAT when prompted
vsce publish          # Or: vsce publish minor
```

## Publish to Open VSX (JetBrains, etc.)

1. Create account at [Open VSX](https://open-vsx.org)
2. Get a Personal Access Token
3. Set the token as an environment variable (do not pass it on the command line—process listings and shell history may expose it):

```bash
export OVSX_PAT=your_token_here
npx ovsx publish
```

The `ovsx` CLI reads the token from `OVSX_PAT` when set. Alternatively, check whether your `ovsx` version supports `--pat` with stdin or a config file.

## Manual install (for testing)

1. Build: `cd vscode && npm run package`
2. In VS Code: Extensions → ⋮ → Install from VSIX → select `goroscope-0.1.0.vsix`

## Verification

- Extension installable via `ext install goroscope.goroscope` (or Install from VSIX)
- **Goroscope: Run Current Package** starts `goroscope run` and opens webview
- Stack frame click in Inspector opens source file in editor
