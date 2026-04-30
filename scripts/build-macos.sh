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

# Auto-update tarball — go-selfupdate's binary swap can't unwrap a .dmg
# (it's a disk image, not an archive). Ship a .tar.gz containing just the
# universal Mach-O binary, named to match what go-selfupdate expects after
# stripping the .tar.gz extension. Used by Plan-7 auto-update on macOS;
# fresh installs still go through the .dmg.
TAR_OUT="${BIN_DIR}/Mosaic-${VERSION}-darwin-universal.tar.gz"
TAR_TMP="$(mktemp -d)"
INNER="Mosaic-${VERSION}-darwin-universal"
cp "${APP}/Contents/MacOS/mosaic" "${TAR_TMP}/${INNER}"
chmod +x "${TAR_TMP}/${INNER}"
tar -czf "${TAR_OUT}" -C "${TAR_TMP}" "${INNER}"
rm -rf "${TAR_TMP}"

echo "==> done: ${DMG_OUT}"
echo "==> done: ${TAR_OUT}"
