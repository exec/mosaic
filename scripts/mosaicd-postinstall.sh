#!/bin/bash
# Auto-enables + starts the systemd unit on .deb / .rpm install. Idempotent
# on upgrades (uses systemctl try-restart so a running instance picks up the
# new binary; a stopped one stays stopped). Mirrors qBittorrent-nox's
# behavior: a daemon you installed should run.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true

    # Fresh install vs. upgrade is detected by whether the unit is already
    # enabled. New install → enable + start. Upgrade → just try-restart so
    # we don't surprise an operator who deliberately stopped it.
    if ! systemctl is-enabled --quiet mosaicd.service 2>/dev/null; then
        systemctl enable mosaicd.service >/dev/null 2>&1 || true
        systemctl start mosaicd.service >/dev/null 2>&1 || true
    else
        systemctl try-restart mosaicd.service >/dev/null 2>&1 || true
    fi
fi

cat <<'EOF'

Mosaic daemon installed and started.

  Find the temporary web-interface password (regenerated each restart):
    sudo journalctl -u mosaicd -e | grep -A4 'temporary web-interface password' | tail -8

  Or watch live:
    sudo journalctl -u mosaicd -f

  Status / restart / stop:
    sudo systemctl status mosaicd
    sudo systemctl restart mosaicd
    sudo systemctl stop mosaicd

The temporary password rotates on every restart. Log in once and change it
via Settings → Web Interface in the browser to make it persist.

Documentation: https://mosaic.byexec.com/docs/daemon/

EOF
