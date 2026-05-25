#!/usr/bin/env bash
set -euo pipefail

# Build a universal (amd64+arm64) signed .app and wrap it in a .dmg.
# Signing + notarization run only when the required env vars are set.
#
# The .app on disk is renamed to "Mosaic.app" (capital M) before DMG packaging
# so it matches the brand name in Finder. The Mach-O inside stays lowercase
# ("Contents/MacOS/mosaic") because go-selfupdate's case-sensitive matcher
# uses filepath.Base of the running binary to validate the inner tar entry
# — see the tarball block at the bottom.
#
# The .dmg uses a custom layout: drag-to-Applications arrow over a black/
# purple gradient background. The background is generated inline at build
# time via a Swift snippet; the layout itself is applied by AppleScript
# while the writable .dmg is mounted, then the volume is converted to a
# read-only UDZO for distribution.
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
VOLNAME="Mosaic"

cd "${ROOT}"

echo "==> build frontend first so main.go's go:embed has its target"
(cd frontend && npm run build)

echo "==> prime module cache"
# Just download — actually compiling here produced a stray ./mosaic binary at
# repo root that ate enough disk on macos-14 runners (~14GB free) to fail
# 'hdiutil create' downstream three times in a row. -skipembedcreate on the
# wails build below dodges the original go/packages choke this was added for.
go mod download

echo "==> wails build (universal)"
# -skipembedcreate dodges wails's go/packages pre-analysis which chokes on
# anacrolix/torrent's CGO transitive deps (pion/webrtc). Same flag we use
# in 'wails dev' locally.
wails build \
    -platform darwin/universal \
    -ldflags "-X main.version=${VERSION}" \
    -clean \
    -skipbindings \
    -skipembedcreate

# Locate whatever .app wails dropped. Different wails versions name it from
# either `outputfilename` (lowercase "mosaic.app") or `info.productName`
# (capital "Mosaic.app"); the macOS filesystem is case-insensitive by default
# so both resolve to the same inode, but a blind `mv mosaic.app Mosaic.app`
# after an `rm -rf Mosaic.app` self-destructs (the rm removes the source
# inode since the names alias). Resolve the actual on-disk basename via ls
# and only rename when it differs.
APP="${BIN_DIR}/Mosaic.app"
WAILS_OUT=$(ls -d "${BIN_DIR}"/*.app 2>/dev/null | head -1)
if [[ -z "${WAILS_OUT}" ]]; then
    echo "build-macos.sh: no .app found in ${BIN_DIR}" >&2
    exit 1
fi
WAILS_BASENAME=$(basename "${WAILS_OUT}")
if [[ "${WAILS_BASENAME}" != "Mosaic.app" ]]; then
    # Case-only rename on case-insensitive APFS — rename(2) handles it directly.
    mv "${WAILS_OUT}" "${APP}"
fi

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
        ZIP_TMP="$(mktemp -d)/Mosaic.zip"
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

echo "==> free disk before DMG step (macos-14 runners are tight ~14GB free)"
# The Go module + build caches accumulate ~5–8GB by this point on a cold
# runner; clear what we don't need anymore so create-dmg's hdiutil scratch
# space doesn't trip 'No space left'.
df -h / 2>/dev/null | tail -1 || true
go clean -cache 2>/dev/null || true
go clean -modcache 2>/dev/null || true
rm -rf /tmp/go-link-* /tmp/go-build* 2>/dev/null || true
df -h / 2>/dev/null | tail -1 || true

# ─── DMG via sindresorhus/create-dmg ───────────────────────────────────────
# create-dmg auto-generates a clean drag-to-Applications layout (default
# background + arrow, Applications symlink, custom .VolumeIcon.icns derived
# from the app's icon). We previously rolled this by hand with AppleScript +
# hdiutil, and after with dmgbuild + a custom Swift-rendered background — both
# died on modern macOS Finder's TCC / alias-resolution behaviour. The tool
# writes a proper .DS_Store (bwsp + icvp) without any AppleScript dance, so
# CI runners and local macs render the same window. Trade-off accepted: we
# give up the bespoke purple gradient + drawn arrow in exchange for a layout
# that actually works on every macOS version we ship for.
echo "==> install create-dmg"
# create-dmg is a Node.js CLI. The macos runners ship Node by default; if
# absent we'd need 'brew install create-dmg', but global npm install is the
# more portable option for CI.
npm install --global create-dmg 2>&1 | tail -5

# Defensive: detach any leftover /Volumes/Mosaic from a previous CI run.
hdiutil detach "/Volumes/${VOLNAME}" -force 2>/dev/null || true

echo "==> build DMG via create-dmg"
# create-dmg names the output "<AppName> <Version>.dmg" using CFBundleVersion
# from Info.plist. We move it to our release-asset naming convention after.
# --overwrite: clobber any prior file at the same path (re-runs on the same
#              runner without this fail loudly).
# --no-code-sign: signing is handled below if APPLE_DEVELOPER_ID is set; we
#              keep the existing flow rather than splitting it between tools.
DMG_TMP_DIR="$(mktemp -d)"
create-dmg --overwrite --no-code-sign "${APP}" "${DMG_TMP_DIR}"
CREATED_DMG=$(ls "${DMG_TMP_DIR}"/*.dmg | head -1)
[ -n "${CREATED_DMG}" ] || { echo "create-dmg produced no .dmg"; exit 1; }
mv "${CREATED_DMG}" "${DMG_OUT}"

if [[ -n "${APPLE_DEVELOPER_ID:-}" ]]; then
    echo "==> codesign DMG"
    codesign --sign "${APPLE_DEVELOPER_ID}" --timestamp "${DMG_OUT}"
fi

# Auto-update tarball — go-selfupdate's binary swap can't unwrap a .dmg
# (it's a disk image, not an archive). Ship a .tar.gz containing just the
# universal Mach-O binary; fresh installs still go through the .dmg.
#
# Inner filename MUST match the lib's case-sensitive matcher in
# decompress.go:matchExecutableName — it derives cmd from filepath.Base of
# the running binary (i.e. lowercase "mosaic" on disk inside the .app) and
# its regex is `^<cmd>([_-]v?<ver>)?([_-]<os>[_-]<arch>)?(\.exe)?$`. So
# "mosaic" matches; "Mosaic-v0.1.22-darwin-universal" does NOT (capital M).
# Earlier releases shipped the capital-M form and broke auto-update with
# "executable not found in tar: \"mosaic\"" — fixed here.
TAR_OUT="${BIN_DIR}/Mosaic-${VERSION}-darwin-universal.tar.gz"
TAR_TMP="$(mktemp -d)"
INNER="mosaic"
cp "${APP}/Contents/MacOS/mosaic" "${TAR_TMP}/${INNER}"
chmod +x "${TAR_TMP}/${INNER}"
tar -czf "${TAR_OUT}" -C "${TAR_TMP}" "${INNER}"
rm -rf "${TAR_TMP}"

echo "==> done: ${DMG_OUT}"
echo "==> done: ${TAR_OUT}"
