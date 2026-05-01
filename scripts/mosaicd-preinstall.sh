#!/usr/bin/env bash
# Runs before the .deb / .rpm unpacks files. Creates the dedicated 'mosaic'
# system user + group used by the systemd unit. Idempotent — repeated installs
# / upgrades are no-ops if the user already exists.
set -e

if ! getent group mosaic >/dev/null 2>&1; then
    groupadd --system mosaic || true
fi

if ! getent passwd mosaic >/dev/null 2>&1; then
    useradd --system \
        --gid mosaic \
        --no-create-home \
        --home-dir /var/lib/mosaic \
        --shell /usr/sbin/nologin \
        --comment "Mosaic BitTorrent daemon" \
        mosaic || true
fi
