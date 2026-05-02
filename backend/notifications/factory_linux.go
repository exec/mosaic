//go:build linux

package notifications

// NewBeeepNotifier returns the Linux notifier. We use a hand-rolled
// dbus client instead of beeep here so we can pass the desktop-entry
// hint that Gnome needs for proper notification attribution. Kept under
// the "Beeep" name so main.go's call site doesn't have to switch.
func NewBeeepNotifier() Notifier { return newLinuxDBusNotifier() }
