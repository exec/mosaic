#!/usr/bin/env bash
# Runs after the .deb / .rpm removes files. Refresh systemd's view of unit
# files. We deliberately DO NOT delete /var/lib/mosaic — preserving the
# database + verify snapshots across uninstall/reinstall cycles is what users
# expect from server packages. Operators who want a clean slate can `rm -rf`
# explicitly.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi
