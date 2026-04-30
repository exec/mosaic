# Mosaic — Plan 8: Packaging, Signing, CI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce signed, notarized, distributable artifacts on every semver tag, picked up automatically by Plan 7's auto-updater. macOS users get a notarized `.dmg`; Windows users get a code-signed NSIS installer + portable `.exe`; Linux users get a `.deb`, `.rpm`, and `.AppImage`. CI runs the full test suite on every PR; release builds run only on `v*.*.*` tags. The work primarily produces YAML, build scripts, entitlements, and integration glue — the actual signing requires user-provided secrets that get plugged into GitHub Actions later.

**Architecture:** A two-workflow GitHub Actions setup — `test.yml` runs on every PR (`go test`, `npm test`, `npm run build`, lint), and `release.yml` runs on `v*.*.*` tag push, builds via a `[macos-14, ubuntu-22.04, windows-latest]` matrix, signs each platform's artifact (notarytool / AzureSignTool / unsigned-Linux), uploads to a draft GitHub Release, and publishes once all jobs succeed. The Wails project gains hardened-runtime entitlements, a Linux `.desktop` file, AppImage assets, NSIS customization. A new `backend/updater/validator.go` plugs into `go-selfupdate`'s `Validator` field to enforce SHA-256 manifests published in each release. Local-build helper scripts live under `scripts/` so contributors can produce the same artifacts as CI.

**Tech additions:**
- `gon` (or `notarytool` direct + `codesign`) for macOS signing+notarization
- `AzureSignTool` for Windows code signing (cloud HSM)
- `nfpm` for `.deb` + `.rpm` (single-source descriptor)
- `appimagetool` for `.AppImage`
- `softprops/action-gh-release` GitHub Action for the release upload step
- No new Go deps — SHA-256 is stdlib

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §9 (Packaging, Signing, CI).

**Aesthetic continuity:** macOS `.dmg` background art uses the same accent gradient + Inter-text styling as the in-app theme. Windows installer welcome screen + Linux `.desktop` icon all derive from `build/appicon.png` (the existing Mosaic icon).

---

## Out of Scope (deferred / future)

- **Multi-arch Windows.** v1 ships amd64 only; arm64 Windows demand is low.
- **Multi-arch Linux.** v1 ships amd64 only.
- **Flatpak / Snap.** AppImage covers the universal-binary need.
- **Auto-update of Mosaic from inside a Linux AppImage.** AppImageUpdate is a separate tool; out of scope for v1.
- **Signed Linux packages** (Debian apt-key, RPM GPG). v1 ships unsigned `.deb` / `.rpm` / `.AppImage`; users get checksum manifests.
- **CI-driven security scanning** (gosec, npm audit beyond build). Add later.
- **Reproducible builds.** v1 doesn't pin nondeterministic build flags; goal is "it runs on a clean CI machine."
- **Plan 7 user-smoke (Task 11)** — runs once Plan 8's release pipeline produces real artifacts.

---

## Out of Reach Without User-Provided Secrets

These tasks land scaffolding (workflow YAML, scripts, env-var slots) but the actual signing happens only after the user adds the corresponding GitHub Actions repository secrets. The plan calls them out explicitly and leaves the workflow steps in a "skip if secret unset" guard so PR-triggered runs (which don't have secret access on forks) don't fail catastrophically.

| Secret | Used by | Notes |
|---|---|---|
| `APPLE_DEVELOPER_ID_CERT_P12_BASE64` | macOS signing | Base64'd `.p12` exported from Keychain |
| `APPLE_DEVELOPER_ID_CERT_PASSWORD` | macOS signing | The `.p12` password |
| `APPLE_ID` | macOS notarization | Developer Apple ID email |
| `APPLE_TEAM_ID` | macOS notarization | 10-char team ID |
| `APPLE_APP_SPECIFIC_PASSWORD` | macOS notarization | https://appleid.apple.com → App-Specific Passwords |
| `AZURE_KEY_VAULT_URI` | Windows signing | Azure Key Vault URL |
| `AZURE_KEY_VAULT_CERT_NAME` | Windows signing | Cert friendly name in the vault |
| `AZURE_TENANT_ID` / `AZURE_CLIENT_ID` / `AZURE_CLIENT_SECRET` | Windows signing | Service principal for AzureSignTool |
| `GITHUB_TOKEN` | Release upload | Auto-provided by Actions |

Document setup in `.github/SIGNING.md` (Task 13).

---

## File Structure (final state)

```
.github/
├── workflows/
│   ├── test.yml                       # NEW — PR validation
│   └── release.yml                    # NEW — tag-driven matrix build + sign + release
├── SIGNING.md                         # NEW — secrets setup walkthrough
└── ISSUE_TEMPLATE.md                  # not in scope (skip)

build/
├── appicon.png                        # existing (mirror to .icns / .ico if not already)
├── README.md                          # update with platform-specific build notes
├── darwin/
│   ├── Info.plist                     # MODIFY: hardened runtime + URL scheme handler
│   ├── entitlements.plist             # NEW
│   └── dmg-bg.png                     # NEW (optional; cosmetic)
├── linux/
│   ├── mosaic.desktop                 # NEW
│   └── AppDir/                        # NEW: AppImage assembly directory
│       └── usr/share/applications/mosaic.desktop
├── windows/
│   ├── installer/project.nsi          # MODIFY: per-machine vs per-user, registry magnet handler
│   ├── icon.ico                       # existing
│   └── wails.exe.manifest             # existing — no change

scripts/
├── build-macos.sh                     # NEW: universal-binary `wails build` + DMG packaging
├── build-linux.sh                     # NEW: amd64 `wails build` + nfpm + appimagetool
├── build-windows.ps1                  # NEW: amd64 `wails build` + NSIS + portable .exe
└── make-checksums.sh                  # NEW: SHA-256 manifest for releases

backend/updater/
├── validator.go                       # NEW: SHA-256 manifest fetch + verify
└── validator_test.go                  # NEW

main.go                                # MODIFY: pass validator into updater.Config
```

---

## Tasks

### Section A — Wails project configuration

#### Task 1: Hardened runtime entitlements + Info.plist enhancements

**Files:**
- Create: `build/darwin/entitlements.plist`
- Modify: `build/darwin/Info.plist`
- Modify: `wails.json`

**Background:** macOS hardened runtime is mandatory for notarization. Without entitlements, the app crashes when JIT, dynamic linking, or library validation kicks in — Mosaic uses none of those, so the entitlements file is minimal.

- [ ] **Step 1: Write `build/darwin/entitlements.plist`**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <!-- Enable hardened runtime (required for notarization). -->
    <key>com.apple.security.cs.allow-jit</key>
    <false/>
    <key>com.apple.security.cs.allow-unsigned-executable-memory</key>
    <false/>
    <key>com.apple.security.cs.disable-library-validation</key>
    <false/>

    <!-- Network client (BitTorrent). -->
    <key>com.apple.security.network.client</key>
    <true/>

    <!-- Network server (BitTorrent + optional Plan 6 web UI). -->
    <key>com.apple.security.network.server</key>
    <true/>

    <!-- File picker access. -->
    <key>com.apple.security.files.user-selected.read-write</key>
    <true/>

    <!-- Hardened runtime can need this for some webview internals. -->
    <key>com.apple.security.cs.allow-dyld-environment-variables</key>
    <false/>
</dict>
</plist>
```

- [ ] **Step 2: Update `build/darwin/Info.plist`**

Add a magnet URL scheme handler so macOS sees Mosaic as a candidate "default app for magnet links." Inside the existing `<dict>` block, add (between `LSMinimumSystemVersion` and the existing `CFBundleDocumentTypes` block — keep it sorted alphabetically by key):

```xml
<key>CFBundleURLTypes</key>
<array>
    <dict>
        <key>CFBundleURLName</key>
        <string>Magnet Link</string>
        <key>CFBundleURLSchemes</key>
        <array>
            <string>magnet</string>
        </array>
    </dict>
</array>

<key>NSAppTransportSecurity</key>
<dict>
    <key>NSAllowsArbitraryLoads</key>
    <true/>
</dict>
```

(`NSAllowsArbitraryLoads=true` is required because BitTorrent peer connections aren't HTTPS. Hardened runtime + ATS bypass is the standard Wails pattern.)

Bump `LSMinimumSystemVersion` to `11.0.0` (the actual minimum for arm64 + universal binary).

- [ ] **Step 3: Update `wails.json`**

Add the `info` block (Wails uses these for build-time substitution into Info.plist's `{{.Info.*}}` placeholders):

```json
{
    "$schema": "https://wails.io/schemas/config.v2.json",
    "name": "mosaic",
    "outputfilename": "mosaic",
    "frontend:install": "npm install",
    "frontend:build": "npm run build",
    "frontend:dev:watcher": "npm run dev",
    "frontend:dev:serverUrl": "auto",
    "author": {
        "name": "Dylan Hart",
        "email": "execxd@icloud.com"
    },
    "info": {
        "companyName": "Dylan Hart",
        "productName": "Mosaic",
        "productVersion": "0.0.0",
        "copyright": "© 2026 Dylan Hart",
        "comments": "A polished cross-platform BitTorrent client."
    }
}
```

Note: `productVersion` here is overwritten at build time by `wails build -ldflags "-X main.version=v0.7.0"` per Plan 7. Keep this as `0.0.0` so accidental local builds without ldflags don't claim a real version.

- [ ] **Step 4: Verify the build works**

```bash
wails build -platform darwin/arm64 -skipembedcreate
file build/bin/mosaic.app/Contents/MacOS/mosaic
```

Should print Mach-O 64-bit executable arm64.

- [ ] **Step 5: Commit**

```bash
git add build/darwin wails.json
git commit -m "feat(build): macOS hardened-runtime entitlements + magnet URL scheme"
```

---

#### Task 2: Linux .desktop + AppImage assets

**Files:**
- Create: `build/linux/mosaic.desktop`
- Create: `build/linux/AppDir/usr/share/applications/mosaic.desktop` (symlink or copy of above)
- Create: `build/linux/AppDir/usr/share/icons/hicolor/512x512/apps/mosaic.png` (copy of `build/appicon.png`)
- Create: `build/linux/AppDir/AppRun` (executable shell stub)
- Create: `build/linux/AppDir/mosaic.desktop` (top-level — AppImage requires this at the AppDir root too)
- Create: `build/linux/AppDir/.DirIcon` (symlink → mosaic.png)

- [ ] **Step 1: `build/linux/mosaic.desktop`**

```desktop
[Desktop Entry]
Type=Application
Name=Mosaic
GenericName=BitTorrent Client
Comment=A polished cross-platform BitTorrent client.
Exec=mosaic %U
Icon=mosaic
Categories=Network;FileTransfer;P2P;
MimeType=application/x-bittorrent;x-scheme-handler/magnet;
StartupNotify=true
```

- [ ] **Step 2: `build/linux/AppDir/AppRun`**

```bash
#!/usr/bin/env bash
HERE="$(dirname "$(readlink -f "${0}")")"
export PATH="${HERE}/usr/bin:${PATH}"
exec "${HERE}/usr/bin/mosaic" "$@"
```

`chmod +x` it.

- [ ] **Step 3: Layout**

The build script (Task 6) will copy the freshly-built `mosaic` binary into `build/linux/AppDir/usr/bin/mosaic` at package time. Don't commit a binary; only commit the `AppDir` skeleton.

Final committed `AppDir`:
```
build/linux/AppDir/
├── AppRun                              # shell stub, +x
├── mosaic.desktop                      # copy of the top-level .desktop
├── .DirIcon                            # → usr/share/icons/.../mosaic.png
└── usr/
    ├── bin/
    │   └── .gitkeep                    # empty placeholder so git tracks the dir
    └── share/
        ├── applications/
        │   └── mosaic.desktop
        └── icons/hicolor/512x512/apps/
            └── mosaic.png              # copy of build/appicon.png
```

- [ ] **Step 4: Commit**

```bash
git add build/linux
git commit -m "feat(build): Linux .desktop + AppImage skeleton"
```

---

#### Task 3: Windows NSIS customizations

**Files:**
- Modify: `build/windows/installer/project.nsi`

The default Wails-generated NSIS handles `mosaic.exe` install. We need:
- Per-machine OR per-user install (let user choose)
- Magnet URL scheme registration in the Windows registry
- Start Menu shortcut + Desktop shortcut (optional, off by default)
- Uninstaller cleanup of registry entries

- [ ] **Step 1: Inspect the current `project.nsi`**

Read `build/windows/installer/project.nsi`. Wails generates a baseline that includes `wails_tools.nsh` plus a basic install / uninstall section. Keep that structure. Add a custom section for magnet handler registration.

- [ ] **Step 2: Add magnet handler section**

After the main install section, append:

```nsh
Section "Magnet Handler" SecMagnet
    ; Register Mosaic as a magnet: handler
    WriteRegStr HKCR "magnet" "" "URL:Magnet Protocol"
    WriteRegStr HKCR "magnet" "URL Protocol" ""
    WriteRegStr HKCR "magnet\DefaultIcon" "" "$INSTDIR\mosaic.exe,0"
    WriteRegStr HKCR "magnet\shell\open\command" "" '"$INSTDIR\mosaic.exe" "%1"'
SectionEnd

LangString DESC_SecMagnet ${LANG_ENGLISH} "Register Mosaic as the default app for magnet links."
!insertmacro MUI_DESCRIPTION_TEXT ${SecMagnet} $(DESC_SecMagnet)
```

In the Uninstall section, add registry cleanup:

```nsh
DeleteRegKey HKCR "magnet"
```

- [ ] **Step 3: Verify NSIS template syntax**

We can't run NSIS on macOS without Wine. CI will validate. Local check: ensure no obvious typos and that macros referenced (`MUI_DESCRIPTION_TEXT`, `LANG_ENGLISH`) come from the `wails_tools.nsh` include. If unsure, leave the magnet section commented out and only ship per-user install for v1 — registry magic can wait.

- [ ] **Step 4: Commit**

```bash
git add build/windows
git commit -m "feat(build): NSIS magnet handler + cleanup"
```

---

### Section B — Local build scripts

#### Task 4: `scripts/build-macos.sh` — universal-binary + DMG

**Files:**
- Create: `scripts/build-macos.sh`

- [ ] **Step 1: Script body**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Build a universal (amd64+arm64) signed .app and wrap it in a .dmg.
# Signing + notarization run only when the required env vars are set.
#
# Required env:
#   VERSION                              e.g. v0.8.0 (defaults to "dev")
#
# Optional (set together for signing):
#   APPLE_DEVELOPER_ID                   "Developer ID Application: Dylan Hart (ABCDE12345)"
#
# Optional (set together for notarization, only if signing is on):
#   APPLE_ID
#   APPLE_TEAM_ID
#   APPLE_APP_SPECIFIC_PASSWORD

VERSION="${VERSION:-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${ROOT}/build/bin"

cd "${ROOT}"

echo "==> wails build (universal)"
wails build \
    -platform darwin/universal \
    -ldflags "-X main.version=${VERSION}" \
    -clean \
    -skipbindings

APP="${BIN_DIR}/mosaic.app"
DMG_OUT="${BIN_DIR}/Mosaic-${VERSION}-darwin-universal.dmg"

echo "==> verify lipo"
lipo -info "${APP}/Contents/MacOS/mosaic"

if [[ -n "${APPLE_DEVELOPER_ID:-}" ]]; then
    echo "==> codesign with hardened runtime"
    codesign --deep --force --verbose \
        --options runtime \
        --entitlements "${ROOT}/build/darwin/entitlements.plist" \
        --sign "${APPLE_DEVELOPER_ID}" \
        --timestamp \
        "${APP}"

    codesign --verify --verbose --strict --deep "${APP}"

    if [[ -n "${APPLE_ID:-}" && -n "${APPLE_TEAM_ID:-}" && -n "${APPLE_APP_SPECIFIC_PASSWORD:-}" ]]; then
        echo "==> notarytool submit + wait"
        ZIP_TMP="$(mktemp -d)/mosaic.zip"
        ditto -c -k --keepParent "${APP}" "${ZIP_TMP}"
        xcrun notarytool submit "${ZIP_TMP}" \
            --apple-id "${APPLE_ID}" \
            --team-id "${APPLE_TEAM_ID}" \
            --password "${APPLE_APP_SPECIFIC_PASSWORD}" \
            --wait

        echo "==> staple"
        xcrun stapler staple "${APP}"
    else
        echo "==> notarization skipped (APPLE_ID/APPLE_TEAM_ID/APPLE_APP_SPECIFIC_PASSWORD not all set)"
    fi
else
    echo "==> codesign skipped (APPLE_DEVELOPER_ID unset) — UNSIGNED dev build"
fi

echo "==> create DMG"
hdiutil create \
    -volname "Mosaic" \
    -srcfolder "${APP}" \
    -ov -format UDZO \
    "${DMG_OUT}"

if [[ -n "${APPLE_DEVELOPER_ID:-}" ]]; then
    echo "==> codesign DMG"
    codesign --sign "${APPLE_DEVELOPER_ID}" --timestamp "${DMG_OUT}"
fi

echo "==> done: ${DMG_OUT}"
```

`chmod +x scripts/build-macos.sh`.

- [ ] **Step 2: Commit**

```bash
git add scripts/build-macos.sh
git commit -m "feat(build): macOS build script — universal + sign + notarize + DMG"
```

---

#### Task 5: `scripts/build-linux.sh`

**Files:**
- Create: `scripts/build-linux.sh`
- Create: `scripts/nfpm.yaml.tmpl`

- [ ] **Step 1: nfpm template `scripts/nfpm.yaml.tmpl`**

```yaml
name: mosaic
arch: amd64
platform: linux
version: ${VERSION}
section: net
priority: optional
maintainer: Dylan Hart <execxd@icloud.com>
description: A polished cross-platform BitTorrent client.
vendor: Dylan Hart
homepage: https://github.com/exec/mosaic
license: MIT
contents:
  - src: build/bin/mosaic
    dst: /usr/bin/mosaic
  - src: build/linux/mosaic.desktop
    dst: /usr/share/applications/mosaic.desktop
  - src: build/appicon.png
    dst: /usr/share/icons/hicolor/512x512/apps/mosaic.png
overrides:
  deb:
    depends:
      - libgtk-3-0
      - libwebkit2gtk-4.0-37
  rpm:
    depends:
      - gtk3
      - webkit2gtk4.0
```

- [ ] **Step 2: Script body**

```bash
#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${ROOT}/build/bin"

cd "${ROOT}"

echo "==> wails build linux/amd64"
wails build \
    -platform linux/amd64 \
    -ldflags "-X main.version=${VERSION}" \
    -clean \
    -skipbindings

ELF="${BIN_DIR}/mosaic"
file "${ELF}" | grep -q "ELF 64-bit"

# nfpm — .deb + .rpm
echo "==> nfpm package (deb + rpm)"
TMPL="${ROOT}/scripts/nfpm.yaml.tmpl"
NFPM_YAML="${BIN_DIR}/nfpm.yaml"
sed "s/\${VERSION}/${VERSION#v}/" "${TMPL}" > "${NFPM_YAML}"

nfpm package --packager deb --target "${BIN_DIR}/Mosaic-${VERSION}-linux-amd64.deb" --config "${NFPM_YAML}"
nfpm package --packager rpm --target "${BIN_DIR}/Mosaic-${VERSION}-linux-amd64.rpm" --config "${NFPM_YAML}"

# AppImage assembly
echo "==> AppImage"
APPDIR="${ROOT}/build/linux/AppDir"
cp "${ELF}" "${APPDIR}/usr/bin/mosaic"
chmod +x "${APPDIR}/usr/bin/mosaic"
cp "${ROOT}/build/linux/mosaic.desktop" "${APPDIR}/mosaic.desktop"
cp "${ROOT}/build/appicon.png" "${APPDIR}/usr/share/icons/hicolor/512x512/apps/mosaic.png"

if [[ ! -L "${APPDIR}/.DirIcon" ]]; then
    ln -sf "usr/share/icons/hicolor/512x512/apps/mosaic.png" "${APPDIR}/.DirIcon"
fi

APPIMAGE_OUT="${BIN_DIR}/Mosaic-${VERSION}-linux-amd64.AppImage"
ARCH=x86_64 appimagetool "${APPDIR}" "${APPIMAGE_OUT}"

echo "==> done: ${BIN_DIR}/Mosaic-${VERSION}-linux-amd64.{deb,rpm,AppImage}"
```

`chmod +x scripts/build-linux.sh`.

- [ ] **Step 3: Commit**

```bash
git add scripts/build-linux.sh scripts/nfpm.yaml.tmpl
git commit -m "feat(build): Linux build script — deb + rpm + AppImage"
```

---

#### Task 6: `scripts/build-windows.ps1`

**Files:**
- Create: `scripts/build-windows.ps1`

- [ ] **Step 1: Script body**

```powershell
# Build a signed mosaic.exe + NSIS installer.
#
# Required env:
#   VERSION  e.g. v0.8.0 (defaults to "dev")
#
# Optional (all required for signing):
#   AZURE_KEY_VAULT_URI
#   AZURE_KEY_VAULT_CERT_NAME
#   AZURE_TENANT_ID
#   AZURE_CLIENT_ID
#   AZURE_CLIENT_SECRET

$ErrorActionPreference = "Stop"
$Version = $env:VERSION
if (-not $Version) { $Version = "dev" }

$Root = Resolve-Path "$PSScriptRoot/.."
Set-Location $Root
$BinDir = "$Root/build/bin"

Write-Host "==> wails build windows/amd64"
wails build `
    -platform windows/amd64 `
    -ldflags "-X main.version=$Version" `
    -nsis `
    -clean `
    -skipbindings
if ($LASTEXITCODE -ne 0) { throw "wails build failed" }

$Exe = "$BinDir/mosaic.exe"
$Installer = "$BinDir/Mosaic-${Version}-windows-amd64-installer.exe"
$Portable  = "$BinDir/Mosaic-${Version}-windows-amd64-portable.exe"

# wails -nsis emits the installer at $BinDir/mosaic-amd64-installer.exe
Move-Item "$BinDir/mosaic-amd64-installer.exe" $Installer -Force
Copy-Item $Exe $Portable -Force

if ($env:AZURE_KEY_VAULT_URI -and $env:AZURE_KEY_VAULT_CERT_NAME) {
    Write-Host "==> AzureSignTool: sign exe + installer"
    $Args = @(
        "sign",
        "-kvu", $env:AZURE_KEY_VAULT_URI,
        "-kvc", $env:AZURE_KEY_VAULT_CERT_NAME,
        "-kvt", $env:AZURE_TENANT_ID,
        "-kvi", $env:AZURE_CLIENT_ID,
        "-kvs", $env:AZURE_CLIENT_SECRET,
        "-tr",  "http://timestamp.digicert.com",
        "-td",  "sha256",
        "-fd",  "sha256"
    )
    AzureSignTool @Args $Portable
    AzureSignTool @Args $Installer
} else {
    Write-Host "==> Windows signing skipped (AZURE_* not all set) — UNSIGNED dev build"
}

Write-Host "==> done: $Installer"
Write-Host "==> done: $Portable"
```

- [ ] **Step 2: Commit**

```bash
git add scripts/build-windows.ps1
git commit -m "feat(build): Windows build script — NSIS + AzureSignTool"
```

---

#### Task 7: `scripts/make-checksums.sh`

**Files:**
- Create: `scripts/make-checksums.sh`

- [ ] **Step 1: Script body**

```bash
#!/usr/bin/env bash
set -euo pipefail
# Emit a SHA-256 manifest for every release artifact.
# Output: build/bin/SHA256SUMS

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${ROOT}/build/bin"

cd "${BIN_DIR}"
shopt -s nullglob

ARTIFACTS=()
for f in *.dmg *.deb *.rpm *.AppImage *-installer.exe *-portable.exe; do
    if [[ -f "${f}" ]]; then
        ARTIFACTS+=("${f}")
    fi
done

if [[ ${#ARTIFACTS[@]} -eq 0 ]]; then
    echo "no artifacts found in ${BIN_DIR}" >&2
    exit 1
fi

# sha256sum on Linux, shasum -a 256 on macOS — pick whichever exists
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${ARTIFACTS[@]}" > SHA256SUMS
else
    shasum -a 256 "${ARTIFACTS[@]}" > SHA256SUMS
fi

echo "==> wrote ${BIN_DIR}/SHA256SUMS:"
cat SHA256SUMS
```

`chmod +x scripts/make-checksums.sh`.

- [ ] **Step 2: Commit**

```bash
git add scripts/make-checksums.sh
git commit -m "feat(build): SHA-256 manifest generator for releases"
```

---

### Section C — Updater integration

#### Task 8: SHA-256 validator

**Files:**
- Create: `backend/updater/validator.go`
- Create: `backend/updater/validator_test.go`
- Modify: `backend/updater/source.go` (wire validator into selfupdate.Config)

`go-selfupdate` accepts a `Validator` that runs after download but before binary swap. We use it to fetch `SHA256SUMS` from the same release and compare.

- [ ] **Step 1: Failing test**

`backend/updater/validator_test.go`:

```go
package updater

import (
    "crypto/sha256"
    "encoding/hex"
    "testing"
)

func TestSHA256Validator_Match(t *testing.T) {
    payload := []byte("hello mosaic")
    sum := sha256.Sum256(payload)
    manifest := hex.EncodeToString(sum[:]) + "  Mosaic-v0.8.0-darwin-universal.dmg\n"

    v := SHA256Validator{ManifestBytes: []byte(manifest)}
    if err := v.Validate(payload, "Mosaic-v0.8.0-darwin-universal.dmg"); err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
}

func TestSHA256Validator_Mismatch(t *testing.T) {
    manifest := "0000000000000000000000000000000000000000000000000000000000000000  test.dmg\n"
    v := SHA256Validator{ManifestBytes: []byte(manifest)}
    if err := v.Validate([]byte("wrong"), "test.dmg"); err == nil {
        t.Fatal("expected mismatch error")
    }
}

func TestSHA256Validator_FileMissing(t *testing.T) {
    manifest := "0000000000000000000000000000000000000000000000000000000000000000  other.dmg\n"
    v := SHA256Validator{ManifestBytes: []byte(manifest)}
    if err := v.Validate([]byte("anything"), "test.dmg"); err == nil {
        t.Fatal("expected file-not-in-manifest error")
    }
}
```

Run: `go test ./backend/updater -run TestSHA256` → expect compile failure.

- [ ] **Step 2: Implement**

`backend/updater/validator.go`:

```go
package updater

import (
    "bufio"
    "bytes"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "strings"
)

// SHA256Validator verifies a downloaded asset against a SHA256SUMS manifest.
// ManifestBytes is the raw text of the SHA256SUMS file (newline-separated
// "<hex64>  <filename>" lines, the standard `sha256sum` / `shasum -a 256` shape).
type SHA256Validator struct {
    ManifestBytes []byte
}

// Validate is the signature go-selfupdate's Config.Validator expects.
// payload is the asset bytes; assetFilename is the canonical name.
func (v SHA256Validator) Validate(payload []byte, assetFilename string) error {
    expected, ok := lookupSum(v.ManifestBytes, assetFilename)
    if !ok {
        return fmt.Errorf("validator: %q not in manifest", assetFilename)
    }
    sum := sha256.Sum256(payload)
    actual := hex.EncodeToString(sum[:])
    if actual != expected {
        return fmt.Errorf("validator: SHA-256 mismatch for %q (expected %s, got %s)",
            assetFilename, expected, actual)
    }
    return nil
}

func lookupSum(manifest []byte, filename string) (string, bool) {
    sc := bufio.NewScanner(bytes.NewReader(manifest))
    for sc.Scan() {
        line := strings.TrimSpace(sc.Text())
        if line == "" || strings.HasPrefix(line, "#") { continue }
        // Format: "<hex64>  <filename>"  (two spaces; sha256sum default)
        parts := strings.SplitN(line, "  ", 2)
        if len(parts) != 2 { continue }
        if parts[1] == filename {
            return strings.ToLower(parts[0]), true
        }
    }
    return "", false
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./backend/updater -run TestSHA256 -v
```

Expect 3/3 PASS.

- [ ] **Step 4: Wire into selfupdate.Config in `source.go`**

The full integration requires fetching the manifest at install-time (it lives at the GitHub Release URL alongside the asset). The cleanest path:

- `Updater.Install` now also fetches `SHA256SUMS` from `<asset_url_dir>/SHA256SUMS` before invoking `selfupdate.UpdateTo`.
- It passes `ManifestBytes` into a `SHA256Validator` and stores the validator on the underlying `selfupdate.Updater` via the lib's `Config.Validator` field.

Since `Config` is captured at `NewUpdater` time and the manifest is per-release, the simplest seam is:

1. In `GitHubSource.DetectLatest`, also return the manifest URL (or fetch the manifest content) alongside the asset URL.
2. Add `ManifestBytes []byte` to `Info` (carried through to `Install`).
3. In `Updater.Install`, build a fresh `selfupdate.Updater` for that one call with `Config{Validator: SHA256Validator{ManifestBytes: info.ManifestBytes}}` and call its `UpdateTo`.

Sketch (adjust to match the lib's actual API):

```go
// In Install:
cfg := selfupdate.Config{
    Source: selfupdate.NewGitHubSource(selfupdate.GitHubConfig{}),
    Validator: SHA256Validator{ManifestBytes: info.ManifestBytes},
}
up, err := selfupdate.NewUpdater(cfg)
if err != nil { return err }
exe, err := selfupdate.ExecutablePath()
if err != nil { return err }
return up.UpdateTo(ctx, info.AssetURL, info.AssetFilename, exe)
```

If go-selfupdate's `Validator` field isn't a function-style interface, fall back to verifying `info.ManifestBytes` against the downloaded file bytes ourselves before swap — which means we'd need to hold the bytes in memory rather than streaming. Use whatever the lib supports. **DM team-lead** if the integration shape is non-obvious — the validator code itself is locked-in via tests; only the wire-up is fluid.

- [ ] **Step 5: Add manifest fetch to `GitHubSource.DetectLatest`**

```go
// after determining rel.Version() and rel.AssetURL:
manifestURL := strings.TrimSuffix(rel.AssetURL, rel.AssetName) + "SHA256SUMS"
manifest, err := httpGet(ctx, manifestURL)
if err != nil {
    // Manifest missing → continue without validator (degrade to lib's stock checks).
    manifest = nil
}
```

`httpGet` is a 5-line helper using `http.NewRequestWithContext`. Don't over-engineer.

- [ ] **Step 6: Tests**

Add a `TestCheck_ManifestPropagated` that uses a fake source returning a manifest blob, then asserts `info.ManifestBytes` is populated.

- [ ] **Step 7: Commit**

```bash
git add backend/updater
git commit -m "feat(updater): SHA-256 validator wired into Install"
```

---

### Section D — GitHub Actions

#### Task 9: `.github/workflows/test.yml` — PR validation

**Files:**
- Create: `.github/workflows/test.yml`

- [ ] **Step 1: Workflow**

```yaml
name: Test

on:
  push:
    branches: [main]
  pull_request:

jobs:
  go:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
      - name: Install Wails system deps
        run: |
          sudo apt-get update
          sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config
      - run: go test ./... -race -count=1 -timeout=300s

  frontend:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: frontend/package-lock.json
      - run: cd frontend && npm ci
      - run: cd frontend && npm test
      - run: cd frontend && npm run build
      - run: cd frontend && npx tsc --noEmit
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "ci: PR validation — go test + npm test"
```

---

#### Task 10: `.github/workflows/release.yml` — tag-driven matrix release

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Workflow**

```yaml
name: Release

on:
  push:
    tags: ['v*.*.*']

permissions:
  contents: write

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - os: macos-14
            platform: macos
            artifact-glob: build/bin/Mosaic-*-darwin-universal.dmg
          - os: ubuntu-22.04
            platform: linux
            artifact-glob: |
              build/bin/Mosaic-*-linux-amd64.deb
              build/bin/Mosaic-*-linux-amd64.rpm
              build/bin/Mosaic-*-linux-amd64.AppImage
          - os: windows-latest
            platform: windows
            artifact-glob: |
              build/bin/Mosaic-*-windows-amd64-installer.exe
              build/bin/Mosaic-*-windows-amd64-portable.exe
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: frontend/package-lock.json
      - run: cd frontend && npm ci
      - run: go install github.com/wailsapp/wails/v2/cmd/wails@latest

      - name: Linux system deps
        if: matrix.platform == 'linux'
        run: |
          sudo apt-get update
          sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config nfpm
          # appimagetool
          wget -O /usr/local/bin/appimagetool \
            https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-x86_64.AppImage
          chmod +x /usr/local/bin/appimagetool

      - name: Windows AzureSignTool
        if: matrix.platform == 'windows'
        run: dotnet tool install --global AzureSignTool

      - name: Build (macOS)
        if: matrix.platform == 'macos'
        env:
          VERSION: ${{ github.ref_name }}
          APPLE_DEVELOPER_ID: ${{ secrets.APPLE_DEVELOPER_ID }}
          APPLE_ID: ${{ secrets.APPLE_ID }}
          APPLE_TEAM_ID: ${{ secrets.APPLE_TEAM_ID }}
          APPLE_APP_SPECIFIC_PASSWORD: ${{ secrets.APPLE_APP_SPECIFIC_PASSWORD }}
        run: bash scripts/build-macos.sh

      - name: Build (Linux)
        if: matrix.platform == 'linux'
        env:
          VERSION: ${{ github.ref_name }}
        run: bash scripts/build-linux.sh

      - name: Build (Windows)
        if: matrix.platform == 'windows'
        env:
          VERSION: ${{ github.ref_name }}
          AZURE_KEY_VAULT_URI: ${{ secrets.AZURE_KEY_VAULT_URI }}
          AZURE_KEY_VAULT_CERT_NAME: ${{ secrets.AZURE_KEY_VAULT_CERT_NAME }}
          AZURE_TENANT_ID: ${{ secrets.AZURE_TENANT_ID }}
          AZURE_CLIENT_ID: ${{ secrets.AZURE_CLIENT_ID }}
          AZURE_CLIENT_SECRET: ${{ secrets.AZURE_CLIENT_SECRET }}
        run: pwsh scripts/build-windows.ps1

      - uses: actions/upload-artifact@v4
        with:
          name: mosaic-${{ matrix.platform }}
          path: ${{ matrix.artifact-glob }}
          if-no-files-found: error

  release:
    needs: build
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/download-artifact@v4
        with:
          path: dist
      - name: Flatten + checksum
        run: |
          mkdir -p out
          find dist -type f -exec cp {} out/ \;
          cd out
          sha256sum * > SHA256SUMS
      - name: Draft GitHub release
        uses: softprops/action-gh-release@v2
        with:
          draft: true
          files: |
            out/*
          generate_release_notes: true
          body: |
            ## Mosaic ${{ github.ref_name }}

            ### Downloads

            - **macOS:** `Mosaic-${{ github.ref_name }}-darwin-universal.dmg`
            - **Linux:** `.deb`, `.rpm`, `.AppImage`
            - **Windows:** installer + portable `.exe`

            See `SHA256SUMS` for checksum verification.

            (Auto-update users: Mosaic will pick this up within 24h. Use Settings → Updates → Check now to fetch immediately.)
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: tag-driven matrix release pipeline"
```

---

### Section E — Documentation

#### Task 11: `.github/SIGNING.md`

**Files:**
- Create: `.github/SIGNING.md`

- [ ] **Step 1: Doc body**

```markdown
# Signing & Release Setup

This repo's `release.yml` workflow signs and notarizes release artifacts.
For signing to actually run, you must add the following GitHub Actions
**repository secrets** (Settings → Secrets and variables → Actions → New repository secret).

If a secret is missing, the corresponding signing step is **skipped** and
the workflow produces an unsigned artifact (still uploaded to the draft
release; you'll see a warning in the build log).

---

## macOS — Apple Developer ID + notarization

| Secret | What it is | How to get it |
|---|---|---|
| `APPLE_DEVELOPER_ID` | The certificate Common Name, e.g. `Developer ID Application: Dylan Hart (ABCDE12345)` | Visible in Keychain Access for the imported `.p12`. |
| `APPLE_ID` | Your Apple Developer account email | https://developer.apple.com |
| `APPLE_TEAM_ID` | 10-char team ID | https://developer.apple.com/account → Membership |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password (NOT your Apple ID password) | https://appleid.apple.com → App-Specific Passwords |

The `.p12` cert itself must also be imported into the runner's Keychain.
The simplest path: encode the `.p12` to base64 and add as
`APPLE_DEVELOPER_ID_CERT_P12_BASE64` + `APPLE_DEVELOPER_ID_CERT_PASSWORD`,
then add a workflow step that decodes + imports before `build-macos.sh`:

```yaml
- name: Import Apple cert
  if: secrets.APPLE_DEVELOPER_ID_CERT_P12_BASE64 != ''
  env:
    P12_B64: ${{ secrets.APPLE_DEVELOPER_ID_CERT_P12_BASE64 }}
    P12_PASS: ${{ secrets.APPLE_DEVELOPER_ID_CERT_PASSWORD }}
  run: |
    KEYCHAIN=mosaic-build.keychain
    P12=$RUNNER_TEMP/cert.p12
    echo "$P12_B64" | base64 --decode > $P12
    security create-keychain -p ci $KEYCHAIN
    security default-keychain -s $KEYCHAIN
    security unlock-keychain -p ci $KEYCHAIN
    security import $P12 -k $KEYCHAIN -P "$P12_PASS" -T /usr/bin/codesign
    security set-key-partition-list -S apple-tool:,apple: -s -k ci $KEYCHAIN
```

Add this step to `release.yml` before the `Build (macOS)` step.

---

## Windows — Azure Key Vault + AzureSignTool

| Secret | Source |
|---|---|
| `AZURE_KEY_VAULT_URI` | `https://<your-vault>.vault.azure.net` |
| `AZURE_KEY_VAULT_CERT_NAME` | The cert friendly name in the vault |
| `AZURE_TENANT_ID` | Service principal tenant |
| `AZURE_CLIENT_ID` | Service principal client ID |
| `AZURE_CLIENT_SECRET` | Service principal client secret |

The cert itself must be uploaded to Azure Key Vault as an OV (or EV)
code-signing cert. Spin up the SP via:

```bash
az ad sp create-for-rbac --name mosaic-signing --role Reader --scopes <vault-resource-id>
az keyvault set-policy --name <vault-name> --spn <sp-app-id> --certificate-permissions get --key-permissions sign
```

---

## Linux

No secrets required — `.deb`, `.rpm`, and `.AppImage` ship unsigned. Users
verify via `SHA256SUMS` (auto-generated, attached to every release).

---

## Cutting a release

1. Bump version in commit subject + tag: `git tag v0.8.0 && git push origin v0.8.0`
2. Watch the `Release` workflow. ~10-15 min for the matrix to complete.
3. Visit the GitHub Releases page → the draft release is waiting.
4. Click "Edit" → review notes → "Publish release."
5. Mosaic clients running v<0.8.0 will pick up the new release within 24h
   (or immediately via Settings → Updates → Check now).
```

- [ ] **Step 2: Commit**

```bash
git add .github/SIGNING.md
git commit -m "docs: signing + release pipeline setup guide"
```

---

#### Task 12: Update `build/README.md` + repo-root README hint

**Files:**
- Modify: `build/README.md` (Wails-generated; add platform notes)

- [ ] **Step 1: Append to `build/README.md`**

```markdown
---

## Local builds

Run the platform-specific helper script:

- **macOS:** `bash scripts/build-macos.sh`
- **Linux:** `bash scripts/build-linux.sh`
- **Windows:** `pwsh scripts/build-windows.ps1`

Set `VERSION=v0.8.0` to override the build-time version constant.

Signing requires the env vars documented in `.github/SIGNING.md`. Without
them the script produces an unsigned binary (clearly logged).

## CI

- `test.yml` — runs on every PR (Go + frontend tests, build).
- `release.yml` — runs on `v*.*.*` tag push; produces the matrix of
  signed/notarized artifacts and drafts a GitHub release.
```

- [ ] **Step 2: Commit**

```bash
git add build/README.md
git commit -m "docs(build): local + CI build instructions"
```

---

### Section F — Smoke

#### Task 13: User-driven release smoke

This task is the natural conclusion of Plan 8 + the unblocking of Plan 7's Task 11.

- [ ] Verify the local macOS build produces a working `.app`:
  ```bash
  VERSION=v0.7.0 bash scripts/build-macos.sh
  open build/bin/mosaic.app
  ```
  App launches; Settings → Updates shows "Version v0.7.0".

- [ ] Push a real release tag:
  ```bash
  git tag v0.7.0
  git push origin v0.7.0
  ```
  Visit GitHub Actions; the Release workflow builds across the matrix. Watch for failures (signing skipped is OK; outright build failure is not).

- [ ] Once the draft release is on GitHub: edit the notes if desired, then publish.

- [ ] Run a previous Mosaic build (locally compiled with `VERSION=v0.6.0` for testing) and observe Settings → Updates → "Check now" surface the v0.7.0 release. Click Install. Verify the binary swap works and the app relaunches at v0.7.0.

- [ ] Tag `plan-8-packaging-complete`, push.

---

## Dispatch summary (suggested batches)

- **Batch 1 (macOS config):** Tasks 1, 2, 3 — entitlements + Info.plist + Linux .desktop + AppImage skeleton + NSIS magnet handler. 3 commits.
- **Batch 2 (build scripts):** Tasks 4, 5, 6, 7 — macOS + Linux + Windows + checksums. 4 commits.
- **Batch 3 (validator):** Task 8 — SHA-256 validator + integration. 1 commit.
- **Batch 4 (CI):** Tasks 9, 10 — test.yml + release.yml. 2 commits.
- **Batch 5 (docs):** Tasks 11, 12 — SIGNING.md + build/README.md update. 2 commits.
- **Batch 6:** Task 13 — user smoke (gated on user being home + having signing certs).

---

**End of Plan 8.**
