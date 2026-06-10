#!/usr/bin/env bash
set -euo pipefail
# Emit a SHA-256 manifest for every release artifact.
# Output: build/bin/SHA256SUMS

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${ROOT}/build/bin"

cd "${BIN_DIR}"
shopt -s nullglob

ARTIFACTS=()
# *.tar.gz covers the macOS auto-update tarball (Mosaic-<ver>-darwin-
# universal.tar.gz from build-macos.sh) — the darwin updater validates it
# against this manifest, and a missing row is a hard checksum failure.
for f in *.dmg *.deb *.rpm *.AppImage *.tar.gz *-installer.exe *-portable.exe; do
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
