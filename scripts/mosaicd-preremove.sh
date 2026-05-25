#!/usr/bin/env bash
# Runs before the .deb / .rpm removes files. On real removals we stop + disable
# so systemd isn't left thrashing on a deleted ELF. On upgrades (dpkg passes
# "upgrade <new-version>"; rpm passes "1") we leave the unit alone — dpkg will
# replace the binary and the new package's postinst handles the daemon-reload
# + try-restart. Disabling on every upgrade left mosaicd silently stopped and
# masked from boot after `apt upgrade`.
set -e

action="${1:-}"

case "$action" in
    upgrade|failed-upgrade|deconfigure|1)
        # Upgrade flow — keep the unit enabled. postinst handles the restart.
        exit 0
        ;;
esac

if command -v systemctl >/dev/null 2>&1; then
    if systemctl is-active --quiet mosaicd; then
        systemctl stop mosaicd || true
    fi
    if systemctl is-enabled --quiet mosaicd 2>/dev/null; then
        systemctl disable mosaicd || true
    fi
fi
