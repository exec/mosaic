//go:build !linux

package notifications

import "github.com/gen2brain/beeep"

func init() {
	// AppName is a package-global in beeep that defaults to
	// "DefaultAppName". On macOS it's used as the -group flag for
	// terminal-notifier; on Windows it's the toast AppID. Set it once at
	// import time so every Notify call carries the right brand.
	beeep.AppName = "Mosaic"
}

// BeeepNotifier is the production Notifier on macOS + Windows. Linux uses
// the LinuxDBusNotifier instead (see linux_notifier.go) because beeep
// doesn't expose D-Bus hints and Gnome needs the desktop-entry hint to
// attribute notifications to the right app icon / permission entry.
type BeeepNotifier struct{}

// Notify delivers a desktop notification. icon may be a path to a PNG file or
// "" to use a platform default. Errors from the underlying delivery surface
// are surfaced verbatim — the caller decides whether to log or ignore.
func (BeeepNotifier) Notify(title, body, icon string) error {
	if icon == "" {
		return beeep.Notify(title, body, nil)
	}
	return beeep.Notify(title, body, icon)
}

// NewBeeepNotifier returns the platform Notifier. Kept under the
// historical name so main.go's call site doesn't have to know about
// the per-OS split.
func NewBeeepNotifier() Notifier { return BeeepNotifier{} }
