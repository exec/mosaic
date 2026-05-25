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

# wails always outputs <outputfilename>.app from wails.json (here: mosaic.app).
# Rename to Mosaic.app so Finder shows the brand name; the inner binary at
# Contents/MacOS/mosaic stays lowercase for the auto-updater (see comment in
# the tarball block at the bottom of this file).
WAILS_APP="${BIN_DIR}/mosaic.app"
APP="${BIN_DIR}/Mosaic.app"
if [[ -d "${WAILS_APP}" ]]; then
    rm -rf "${APP}"
    mv "${WAILS_APP}" "${APP}"
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

# ─── DMG layout ────────────────────────────────────────────────────────────
# Stage the contents: Mosaic.app, an /Applications symlink, and a hidden
# .background dir with the gradient PNG. AppleScript later positions the two
# visible icons and points the window at the background.
echo "==> stage DMG contents"
DMG_STAGE="$(mktemp -d)/dmg-stage"
mkdir -p "${DMG_STAGE}/.background"
cp -R "${APP}" "${DMG_STAGE}/Mosaic.app"
ln -s /Applications "${DMG_STAGE}/Applications"

echo "==> generate background image (Swift + CoreGraphics)"
# 1320×880 pixels = 2x the 660×440 logical window. Renders crisp on Retina,
# downscales cleanly on non-Retina. All drawing coords below are in PIXELS
# (not points) because we draw straight into an NSBitmapImageRep.
BG_PNG="${DMG_STAGE}/.background/bg.png"
SWIFT_SRC="$(mktemp).swift"
cat > "${SWIFT_SRC}" <<'SWIFT'
import AppKit

let out = CommandLine.arguments[1]
let w = 1320
let h = 880

guard let rep = NSBitmapImageRep(
    bitmapDataPlanes: nil,
    pixelsWide: w, pixelsHigh: h,
    bitsPerSample: 8, samplesPerPixel: 4,
    hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB,
    bytesPerRow: 0, bitsPerPixel: 0
) else { fputs("bitmap alloc failed\n", stderr); exit(1) }

NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
guard let ctx = NSGraphicsContext.current?.cgContext else { exit(1) }

// Diagonal gradient: near-black top-left → muted purple bottom-right.
let colors = [
    NSColor(srgbRed: 0.035, green: 0.030, blue: 0.075, alpha: 1).cgColor,
    NSColor(srgbRed: 0.165, green: 0.090, blue: 0.305, alpha: 1).cgColor,
] as CFArray
let gradient = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(),
                          colors: colors, locations: [0, 1])!
ctx.drawLinearGradient(gradient,
                       start: CGPoint(x: 0, y: CGFloat(h)),
                       end:   CGPoint(x: CGFloat(w), y: 0),
                       options: [])

// Soft radial highlight near the upper-left so the gradient doesn't look flat.
let glowColors = [
    NSColor(srgbRed: 0.35, green: 0.20, blue: 0.60, alpha: 0.18).cgColor,
    NSColor(srgbRed: 0.35, green: 0.20, blue: 0.60, alpha: 0.0).cgColor,
] as CFArray
let glow = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(),
                      colors: glowColors, locations: [0, 1])!
ctx.drawRadialGradient(glow,
                       startCenter: CGPoint(x: CGFloat(w) * 0.25, y: CGFloat(h) * 0.85),
                       startRadius: 0,
                       endCenter:   CGPoint(x: CGFloat(w) * 0.25, y: CGFloat(h) * 0.85),
                       endRadius:   CGFloat(w) * 0.5,
                       options:     [])

// Icons sit at logical (160, 220) and (500, 220) → pixel (320, 440) and (1000, 440).
// (Cocoa origin is bottom-left.) The icons are 96pt → 192px; arrow goes from
// just right of the app icon to just left of the Applications icon.
let arrowStartX: CGFloat = 480
let arrowEndX:   CGFloat = 840
let arrowY:      CGFloat = CGFloat(h) - 440   // mirror "y from top" → Cocoa Y

let path = CGMutablePath()
path.move(to: CGPoint(x: arrowStartX, y: arrowY))
path.addLine(to: CGPoint(x: arrowEndX, y: arrowY))
// Arrowhead — two short strokes from the tip
path.move(to: CGPoint(x: arrowEndX, y: arrowY))
path.addLine(to: CGPoint(x: arrowEndX - 28, y: arrowY + 18))
path.move(to: CGPoint(x: arrowEndX, y: arrowY))
path.addLine(to: CGPoint(x: arrowEndX - 28, y: arrowY - 18))

ctx.setStrokeColor(NSColor.white.withAlphaComponent(0.75).cgColor)
ctx.setLineWidth(5)
ctx.setLineCap(.round)
ctx.setLineJoin(.round)
ctx.addPath(path)
ctx.strokePath()

// Caption below the icons.
let caption = "Drag Mosaic into Applications to install"
let para = NSMutableParagraphStyle()
para.alignment = .center
let attrs: [NSAttributedString.Key: Any] = [
    .font: NSFont.systemFont(ofSize: 26, weight: .medium),
    .foregroundColor: NSColor.white.withAlphaComponent(0.72),
    .paragraphStyle: para,
    .kern: 0.5,
]
let attrStr = NSAttributedString(string: caption, attributes: attrs)
let textRect = CGRect(x: 0, y: 120, width: CGFloat(w), height: 50)
attrStr.draw(in: textRect)

NSGraphicsContext.restoreGraphicsState()

guard let data = rep.representation(using: .png, properties: [:]) else {
    fputs("png encode failed\n", stderr); exit(1)
}
try data.write(to: URL(fileURLWithPath: out))
SWIFT
swift "${SWIFT_SRC}" "${BG_PNG}"
rm -f "${SWIFT_SRC}"

# Defensive: detach any leftover /Volumes/Mosaic from a previous run on the
# same runner. macos-14 runners reuse host state across jobs and a
# half-cleaned-up disk image surfaces as `hdiutil: create failed - Resource
# busy`. -force ignores "not attached" errors.
hdiutil detach "/Volumes/${VOLNAME}" -force 2>/dev/null || true

# hdiutil's auto-sizing from -srcfolder undershoots on universal builds and
# fails copying into the mounted image with the very-misleading 'No space
# left on device'. Compute 3x source size (in MB) + 100MB padding so the
# UDRW/UDZO container has plenty of room. Took five v0.1.* runs to nail down.
APP_SIZE_MB=$(du -sm "${APP}" | awk '{print $1}')
DMG_SIZE_MB=$((APP_SIZE_MB * 3 + 100))

echo "==> create writable DMG (UDRW) for layout pass"
DMG_TMP="$(mktemp -d)/Mosaic-stage.dmg"
for attempt in 1 2 3; do
    if hdiutil create \
        -volname "${VOLNAME}" \
        -srcfolder "${DMG_STAGE}" \
        -ov -format UDRW \
        -size "${DMG_SIZE_MB}m" \
        "${DMG_TMP}"; then
        break
    fi
    if [[ $attempt -eq 3 ]]; then
        echo "==> hdiutil create failed after 3 attempts" >&2
        exit 1
    fi
    echo "==> hdiutil attempt ${attempt} failed, detaching + retrying after 5s" >&2
    hdiutil detach "/Volumes/${VOLNAME}" -force 2>/dev/null || true
    sleep 5
done

echo "==> mount + apply Finder layout"
MOUNT_OUT=$(hdiutil attach -readwrite -noverify -noautoopen "${DMG_TMP}")
MOUNT_PATH=$(echo "${MOUNT_OUT}" | tail -1 | awk '{print $3}')
echo "    mounted at ${MOUNT_PATH}"

# Give Finder a moment to notice the mount before AppleScript talks to it.
sleep 2

# The bounds {x1, y1, x2, y2} are screen coords. 660×440 window centered
# vertically near the top of the screen. icon size 96 matches Big Sur+ default;
# positions place the .app on the left and the Applications symlink on the
# right with the arrow flowing between them on the generated background.
osascript <<APPLESCRIPT
tell application "Finder"
    tell disk "${VOLNAME}"
        open
        set current view of container window to icon view
        set toolbar visible of container window to false
        set statusbar visible of container window to false
        set sidebar width of container window to 0
        set the bounds of container window to {400, 120, 1060, 560}
        set viewOptions to the icon view options of container window
        set arrangement of viewOptions to not arranged
        set icon size of viewOptions to 96
        set text size of viewOptions to 13
        set background picture of viewOptions to file ".background:bg.png"
        set position of item "Mosaic.app" of container window to {160, 220}
        set position of item "Applications" of container window to {500, 220}
        update without registering applications
        delay 1
        close
    end tell
end tell
APPLESCRIPT

# Make the .DS_Store reflect what we just set, then unmount.
sync
hdiutil detach "${MOUNT_PATH}" -force 2>/dev/null || hdiutil detach "${MOUNT_PATH}" || true

echo "==> convert writable -> compressed UDZO"
hdiutil convert "${DMG_TMP}" -format UDZO -imagekey zlib-level=9 -ov -o "${DMG_OUT}"
rm -f "${DMG_TMP}"

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
