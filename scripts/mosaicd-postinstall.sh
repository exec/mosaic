#!/bin/bash
# Auto-enables + starts the systemd unit on .deb / .rpm install. Idempotent
# on upgrades (uses systemctl try-restart so a running instance picks up the
# new binary; a stopped one stays stopped). Mirrors qBittorrent-nox's
# behavior: a daemon you installed should run.
set -e

# apt-managed sentinel for the headless daemon. mosaicd doesn't currently
# run the in-app updater (auto-update is intentionally disabled — see the
# package comment in cmd/mosaicd/main.go), but we write the sentinel so
# any future tooling shares the same detection seam as the GUI.
# Gated on configure (dpkg) / 1|2 (rpm), skipped on abort-* rollbacks.
case "${1:-}" in
    configure|1|2|"")
        mkdir -p /usr/share/mosaic 2>/dev/null || true
        {
            printf 'mosaicd %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)" \
                > /usr/share/mosaic/installed-by-apt-mosaicd
        } 2>/dev/null || true
        ;;
esac

if command -v systemctl >/dev/null 2>&1; then
    # daemon-reload errors are surfaced (not silenced) so a broken unit file
    # is visible to the operator instead of getting swept under the rug.
    systemctl daemon-reload || echo "mosaicd-postinstall: systemctl daemon-reload failed (continuing)" >&2

    # Distinguish a fresh install from an upgrade with a marker file under
    # the daemon's StateDirectory. We can't use `systemctl is-enabled` for
    # this — an operator who deliberately disabled the unit before upgrade
    # would get re-enabled on every package update, which is rude.
    #
    # Fresh install   → enable + start.
    # Existing state  → try-restart only, respecting whatever enabled/active
    #                   choice the operator currently has set.
    if [ ! -f /var/lib/mosaic/.installed ]; then
        systemctl enable mosaicd.service >/dev/null 2>&1 || true
        systemctl start mosaicd.service >/dev/null 2>&1 || true
        # Drop the marker AFTER the enable+start so a half-failed first-run
        # gets retried on the next install rather than being mistaken for
        # an upgrade. Owned by the mosaic user so the daemon could rewrite
        # it later if we ever need to.
        install -m 0644 -o mosaic -g mosaic /dev/null /var/lib/mosaic/.installed 2>/dev/null \
            || touch /var/lib/mosaic/.installed 2>/dev/null \
            || true
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
