#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${ROOT}/build/bin"

cd "${ROOT}"

echo "==> build frontend first so main.go's go:embed has its target"
(cd frontend && npm run build)

echo "==> prime module cache"
go mod download

echo "==> wails build linux/amd64"
wails build \
    -platform linux/amd64 \
    -ldflags "-X main.version=${VERSION}" \
    -clean \
    -skipbindings \
    -skipembedcreate

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
# appimagetool requires the icon (matching the .desktop's `Icon=mosaic`) at the
# AppDir root in addition to the hicolor location.
cp "${ROOT}/build/appicon.png" "${APPDIR}/mosaic.png"
cp "${ROOT}/build/appicon.png" "${APPDIR}/usr/share/icons/hicolor/512x512/apps/mosaic.png"

if [[ ! -L "${APPDIR}/.DirIcon" ]]; then
    ln -sf "usr/share/icons/hicolor/512x512/apps/mosaic.png" "${APPDIR}/.DirIcon"
fi

APPIMAGE_OUT="${BIN_DIR}/Mosaic-${VERSION}-linux-amd64.AppImage"
ARCH=x86_64 appimagetool "${APPDIR}" "${APPIMAGE_OUT}"

echo "==> done: ${BIN_DIR}/Mosaic-${VERSION}-linux-amd64.{deb,rpm,AppImage}"
