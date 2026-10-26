#!/usr/bin/env bash
# Build the headless Linux daemon (mosaicd) + a tarball + nfpm-driven
# .deb / .rpm. Mirrors scripts/build-linux.sh but without Wails — mosaicd is
# a plain Go binary, no GTK / WebKit dependency, so the resulting packages
# install cleanly on minimal server distros.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${VERSION:-v$(cat "${ROOT}/VERSION")}"
BIN_DIR="${ROOT}/build/bin"
mkdir -p "${BIN_DIR}"

cd "${ROOT}"

# Build separately: desktop assets contain updater controls and cannot be reused.
echo "==> build daemon frontend"
(cd frontend && npm run build:daemon)

echo "==> prime module cache"
go mod download

# A daemon must never link the desktop self-updater or its download backend.
if go list -deps ./cmd/mosaicd | grep -E '^(mosaic/backend/updater|github.com/creativeprojects/go-selfupdate)(/|$)' >/dev/null; then
    echo "mosaicd unexpectedly depends on the desktop updater" >&2
    exit 1
fi

echo "==> go build mosaicd"
GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.version=${VERSION} -s -w" \
    -o "${BIN_DIR}/mosaicd" \
    ./cmd/mosaicd

ELF="${BIN_DIR}/mosaicd"
file "${ELF}" | grep -q "ELF 64-bit"

# nfpm — .deb + .rpm
echo "==> nfpm package (deb + rpm)"
TMPL="${ROOT}/scripts/nfpm.mosaicd.yaml.tmpl"
NFPM_YAML="${BIN_DIR}/nfpm.mosaicd.yaml"
sed "s/\${VERSION}/${VERSION#v}/" "${TMPL}" > "${NFPM_YAML}"

nfpm package --packager deb --target "${BIN_DIR}/Mosaic-${VERSION}-linux-amd64-mosaicd.deb" --config "${NFPM_YAML}"
nfpm package --packager rpm --target "${BIN_DIR}/Mosaic-${VERSION}-linux-amd64-mosaicd.rpm" --config "${NFPM_YAML}"

# Raw tarball for Docker / non-systemd installs
echo "==> tarball"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT
PKG="mosaicd-${VERSION}-linux-amd64"
mkdir -p "${STAGE}/${PKG}/dist"
cp "${ELF}" "${STAGE}/${PKG}/mosaicd"
chmod +x "${STAGE}/${PKG}/mosaicd"
cp -R "${ROOT}/frontend/dist-daemon/." "${STAGE}/${PKG}/dist/"
cp "${ROOT}/build/linux/mosaicd.service" "${STAGE}/${PKG}/mosaicd.service"
cat > "${STAGE}/${PKG}/README.txt" <<'README'
Mosaic daemon (mosaicd) — Linux amd64 tarball
=============================================

Contents:
  mosaicd             headless daemon binary
  dist/               SolidJS SPA assets served over HTTPS+WS
  mosaicd.service     example systemd unit (adjust ExecStart paths to taste)
  README.txt          this file

Quick start (no systemd):
  ./mosaicd --assets-dir ./dist --data-dir ./data --port 8080

The first launch prints a temporary web-interface password to stdout. Log in
at http://127.0.0.1:8080, then change the password from Settings -> Users.
Use --bind-all to serve HTTPS on other interfaces. The temporary password
changes on each restart until you set a permanent one.

Quick start (systemd):
  sudo cp mosaicd /usr/bin/mosaicd
  sudo mkdir -p /usr/share/mosaicd && sudo cp -R dist /usr/share/mosaicd/dist
  sudo cp mosaicd.service /etc/systemd/system/mosaicd.service
  sudo useradd --system --no-create-home --shell /usr/sbin/nologin mosaic || true
  sudo systemctl daemon-reload
  sudo systemctl enable --now mosaicd

Documentation: https://mosaic.byexec.com/docs/daemon/
README

TARBALL="${BIN_DIR}/${PKG}.tar.gz"
tar -C "${STAGE}" -czf "${TARBALL}" "${PKG}"

echo "==> done:"
echo "    ${BIN_DIR}/Mosaic-${VERSION}-linux-amd64-mosaicd.deb"
echo "    ${BIN_DIR}/Mosaic-${VERSION}-linux-amd64-mosaicd.rpm"
echo "    ${TARBALL}"
