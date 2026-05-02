#!/bin/sh
# Runs after the .deb / .rpm removes files. On `purge` (full uninstall) we
# clean up the apt-managed sentinel so a future manual / AppImage install
# isn't mis-classified as apt-managed by the in-app updater.
#
# Critically gated on `purge`, NOT `remove`: dpkg fires `remove`+`install`
# during downgrades and major-version upgrades, and we don't want to thrash
# the sentinel in that case (the subsequent postinst will rewrite it, but
# briefly clearing it could race with the running app).
#
# Sentinel path matches sentinelPathAPT in backend/updater/install_source.go.

set -e

case "$1" in
    purge)
        rm -f /usr/share/mosaic/installed-by-apt 2>/dev/null || true
        # Best-effort rmdir — leaves the directory if mosaicd's sentinel
        # (or any other file) is still present.
        rmdir /usr/share/mosaic 2>/dev/null || true
        ;;
    *)
        # remove / upgrade / failed-upgrade / abort-* — leave the sentinel
        # in place. The corresponding postinst (either ours on reinstall
        # or noop on plain remove) handles the canonical state.
        :
        ;;
esac

exit 0
