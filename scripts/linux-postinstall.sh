#!/usr/bin/env bash
# Runs after the .deb / .rpm installs. Refreshes the system MIME +
# desktop-entry caches so Mosaic shows up as a candidate handler for
# .torrent files and magnet: URLs immediately, then sets it as the default.
# Errors are non-fatal: the package install must still succeed even on
# minimal systems where these tools are absent.

set -e

if command -v update-mime-database >/dev/null 2>&1; then
    update-mime-database /usr/share/mime || true
fi

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database /usr/share/applications || true
fi

if command -v xdg-mime >/dev/null 2>&1; then
    xdg-mime default mosaic.desktop application/x-bittorrent || true
    xdg-mime default mosaic.desktop x-scheme-handler/magnet || true
fi
