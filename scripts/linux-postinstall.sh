#!/usr/bin/env bash
# Runs after the .deb / .rpm installs. Refreshes the system MIME +
# desktop-entry caches so Mosaic shows up as a candidate handler for
# .torrent files and magnet: URLs immediately, then sets it as the default.
# Also writes the apt-managed sentinel file that the in-app updater
# consults to decide whether to defer to dpkg (see backend/updater/
# install_source.go — sentinelPathAPT). Errors are non-fatal: the package
# install must still succeed even on minimal systems where these tools
# are absent.

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

# apt-managed sentinel. The Go-side DetectInstallSource() reads
# /usr/share/mosaic/installed-by-apt to know it should defer upgrades to
# dpkg. Path matches sentinelPathAPT in backend/updater/install_source.go.
#
# dpkg invokes postinst with $1 ∈ {configure, abort-upgrade, abort-remove,
# abort-deconfigure}. We only want to (re)write the sentinel on the
# happy-path `configure` — the abort-* actions mean the install is being
# rolled back, so claiming apt-managed would be wrong.
#
# rpm calls postinst with $1 = "1" (initial install) or "2" (upgrade) and
# no string token at all on some toolchains. The empty/no-token case is
# treated as "rpm install" → write the sentinel.
case "${1:-}" in
    configure|1|2|"")
        mkdir -p /usr/share/mosaic 2>/dev/null || true
        {
            printf 'mosaic %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)" \
                > /usr/share/mosaic/installed-by-apt
        } 2>/dev/null || true
        ;;
esac

exit 0
