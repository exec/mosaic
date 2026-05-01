#!/usr/bin/env bash
# Runs before the .deb / .rpm removes files. Stops + disables the unit if it
# is currently running so we don't leave systemd thrashing on a deleted ELF.
set -e

if command -v systemctl >/dev/null 2>&1; then
    if systemctl is-active --quiet mosaicd; then
        systemctl stop mosaicd || true
    fi
    if systemctl is-enabled --quiet mosaicd 2>/dev/null; then
        systemctl disable mosaicd || true
    fi
fi
