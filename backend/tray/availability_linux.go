//go:build linux

package tray

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

// Available reports whether a StatusNotifierItem watcher is currently
// listening on the session bus, meaning a tray icon emitted by us will
// actually render somewhere.
//
// On vanilla Gnome (Debian, Fedora) no watcher owns the well-known name
// because the shell deliberately ignores the protocol; users have to
// install gnome-shell-extension-appindicator (which provides the watcher)
// before tray icons appear. KDE Plasma owns the name natively. XFCE,
// MATE, Cinnamon, and Budgie either own it natively or via a packaged
// extension that's enabled by default.
//
// We use this to decide:
//   - whether to spin up the tray goroutine at all (skip if no watcher),
//   - whether to honor close-to-tray (force off if no watcher — hiding
//     into a non-existent tray would orphan the window).
//
// The check uses NameHasOwner on org.kde.StatusNotifierWatcher: it
// returns true iff a process currently owns that bus name. The check is
// cheap (single round-trip) and short-circuits on bus connection failure.
func Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		// No session bus reachable — headless / sandboxed / SSH session
		// without DISPLAY. Fall back to "tray unavailable" so close-to-tray
		// gets disabled for the session.
		return false
	}
	defer conn.Close()

	var hasOwner bool
	err = conn.BusObject().CallWithContext(
		ctx,
		"org.freedesktop.DBus.NameHasOwner",
		0,
		"org.kde.StatusNotifierWatcher",
	).Store(&hasOwner)
	if err != nil {
		return false
	}
	return hasOwner
}
