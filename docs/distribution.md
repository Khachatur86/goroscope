# Distribution Setup Guide

This document covers the one-time manual steps required to publish Goroscope to the various distribution channels. Once these steps are complete, **every push of a `v*` tag triggers fully automated publishing**.

---

## 1 — Visual Studio Marketplace

### 1.1 Create a publisher

1. Sign in at <https://marketplace.visualstudio.com/manage> with a Microsoft account.
2. Click **Create publisher**.
3. Set the publisher ID to **`goroscope`** (must match `"publisher"` in `vscode/package.json`).
4. Fill in display name and description, then click **Create**.

### 1.2 Create a Personal Access Token (PAT)

1. Go to <https://dev.azure.com> → top-right avatar → **Personal access tokens**.
2. Click **New Token**.
3. Set **Organization** to `All accessible organizations`.
4. Under **Scopes**, expand **Marketplace** and tick **Publish**.
5. Set an expiry (maximum: 1 year — add a calendar reminder to rotate it).
6. Copy the token immediately; it won't be shown again.

### 1.3 Add secret to GitHub

In the Goroscope repository on GitHub:

```
Settings → Secrets and variables → Actions → New repository secret
  Name:  VSCE_PAT
  Value: <your Azure DevOps PAT>
```

The `publish-vscode.yml` workflow reads `secrets.VSCE_PAT` and skips the publish step gracefully if the secret is absent.

---

## 2 — Open VSX Registry

Open VSX is used by VSCodium, Gitpod, Eclipse Theia, and the JetBrains Marketplace.

### 2.1 Create an account

1. Visit <https://open-vsx.org> and sign in with GitHub.
2. Go to **Your profile → Access Tokens → Generate new token**.
3. Give the token a descriptive name (e.g. `goroscope-ci`) and copy it.

### 2.2 Add secret to GitHub

```
Settings → Secrets and variables → Actions → New repository secret
  Name:  OVSX_PAT
  Value: <your Open VSX token>
```

---

## 3 — Homebrew Tap

The GoReleaser config in `.goreleaser.yaml` auto-updates the formula in a separate Homebrew tap repository after each release.

### 3.1 Create the tap repository

1. Create a **public** GitHub repository named `homebrew-goroscope` under the `Khachatur86` account (or your organisation).
   > The repository name *must* start with `homebrew-` for `brew tap` to work.
2. The repository can start empty; GoReleaser will push the formula file on first release.

### 3.2 Create a PAT for the tap

1. Go to GitHub → Settings → Developer settings → **Personal access tokens → Fine-grained tokens**.
2. Click **Generate new token**.
3. Set **Repository access** to `Khachatur86/homebrew-goroscope` only.
4. Grant **Contents: Read and write** permission.
5. Copy the token.

### 3.3 Add secret to the release repository

```
Settings → Secrets and variables → Actions → New repository secret
  Name:  HOMEBREW_TAP_TOKEN
  Value: <PAT from step 3.2>
```

### 3.4 Test the tap (after first release)

```bash
brew tap Khachatur86/goroscope
brew install goroscope
goroscope version
```

---

## 4 — Trigger a release

Once the secrets are configured, push a semver tag to kick off all publishing workflows simultaneously:

```bash
git tag v0.2.0
git push origin v0.2.0
```

This triggers:

| Workflow | What it does |
|----------|--------------|
| `release.yml` | GoReleaser: builds binaries, creates GitHub Release, updates Homebrew tap |
| `publish-vscode.yml` | Packages and publishes the VS Code extension to VS Marketplace + Open VSX |

---

## 5 — Token rotation

| Secret | Rotation period | Action |
|--------|-----------------|--------|
| `VSCE_PAT` | ≤ 1 year (Azure PAT max) | Regenerate in Azure DevOps, update GitHub secret |
| `OVSX_PAT` | No expiry (rotate annually) | Regenerate on open-vsx.org, update GitHub secret |
| `HOMEBREW_TAP_TOKEN` | No expiry (rotate annually) | Regenerate fine-grained PAT, update GitHub secret |

---

## 6 — Verifying a release

After a successful release:

- **Binary**: `go install github.com/Khachatur86/goroscope/cmd/goroscope@latest` installs the new version.
- **Homebrew**: `brew upgrade goroscope` installs the new version.
- **VS Marketplace**: search `goroscope` in the Extensions panel or visit <https://marketplace.visualstudio.com/items?itemName=goroscope.goroscope>.
- **Open VSX**: visit <https://open-vsx.org/extension/goroscope/goroscope>.
