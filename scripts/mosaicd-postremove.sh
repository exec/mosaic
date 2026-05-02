#!/usr/bin/env bash
# Runs after the .deb / .rpm removes files. Refresh systemd's view of unit
# files. We deliberately DO NOT delete /var/lib/mosaic — preserving the
# database + verify snapshots across uninstall/reinstall cycles is what users
# expect from server packages. Operators who want a clean slate can `rm -rf`
# explicitly.
#
# On `purge` we clean up the apt-managed sentinel (gated on purge, not
# remove, since downgrades fire remove+install and we don't want to thrash).
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

case "$1" in
    purge)
        rm -f /usr/share/mosaic/installed-by-apt-mosaicd 2>/dev/null || true
        rmdir /usr/share/mosaic 2>/dev/null || true
        ;;
esac

exit 0
