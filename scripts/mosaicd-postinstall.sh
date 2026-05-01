#!/usr/bin/env bash
# Runs after the .deb / .rpm unpacks files. Refreshes the systemd unit cache
# but does NOT auto-start the service — let the operator opt in explicitly so
# we don't surprise anyone with a freshly-bound TCP listener.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

cat <<'EOF'

Mosaic daemon installed.

  Enable + start now:    sudo systemctl enable --now mosaicd
  View status / logs:    sudo systemctl status mosaicd
                         sudo journalctl -u mosaicd -f

The first launch will generate a random web-interface password and write it
to /var/lib/mosaic/mosaicd-credentials (mode 0600, owned by 'mosaic'). It is
also logged to journald — `journalctl -u mosaicd | grep -i password`.

Documentation: https://mosaic.byexec.com/docs/daemon/

EOF
