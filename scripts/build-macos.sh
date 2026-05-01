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

echo "==> free disk before hdiutil (macos-14 runners are tight ~14GB free)"
# hdiutil's UDZO format needs scratch ~= 2x the .app while compressing. The
# go module + build caches accumulate ~5–8GB by this point on a cold runner;
# clear what we don't need anymore so we don't trip 'No space left'. Three
# v0.1.* runs in a row failed here.
df -h / 2>/dev/null | tail -1 || true
go clean -cache 2>/dev/null || true
go clean -modcache 2>/dev/null || true
rm -rf /tmp/go-link-* /tmp/go-build* 2>/dev/null || true
df -h / 2>/dev/null | tail -1 || true

echo "==> create DMG"
# hdiutil's auto-sizing from -srcfolder undershoots on universal builds and
# fails copying into the mounted image with the very-misleading 'No space
# left on device'. Compute 3x source size (in MB) + 100MB padding so the
# UDZO container has plenty of room. Took five v0.1.* runs to nail down.
APP_SIZE_MB=$(du -sm "${APP}" | awk '{print $1}')
DMG_SIZE_MB=$((APP_SIZE_MB * 3 + 100))

# Defensive: detach any leftover /Volumes/Mosaic from a previous run on the
# same runner. macos-14 runners reuse host state across jobs and a
# half-cleaned-up disk image surfaces as `hdiutil: create failed - Resource
# busy`. -force ignores "not attached" errors.
hdiutil detach "/Volumes/Mosaic" -force 2>/dev/null || true

# Retry up to 3 times on transient hdiutil failures (Resource busy or
# stray diskimages-help processes from prior runs). Each retry pauses to
# let the kernel release the device.
for attempt in 1 2 3; do
    if hdiutil create \
        -volname "Mosaic" \
        -srcfolder "${APP}" \
        -ov -format UDZO \
        -size "${DMG_SIZE_MB}m" \
        "${DMG_OUT}"; then
        break
    fi
    if [[ $attempt -eq 3 ]]; then
        echo "==> hdiutil create failed after 3 attempts" >&2
        exit 1
    fi
    echo "==> hdiutil attempt ${attempt} failed, detaching + retrying after 5s" >&2
    hdiutil detach "/Volumes/Mosaic" -force 2>/dev/null || true
    sleep 5
done

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
