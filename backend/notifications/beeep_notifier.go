//go:build !linux

package notifications

import (
	"sync"

	"github.com/gen2brain/beeep"
)

// appNameMu serializes writes to beeep.AppName so a transitive dep that
// also imports beeep can't win an init-order race against us. We set
// the field immediately before each Notify call rather than once at
// init time — beeep.AppName is a package-global, and any other importer
// (now or via a future transitive dep) could otherwise stomp it
// depending on import ordering. macOS uses it as the
// `terminal-notifier -group` flag; Windows uses it as the toast AppID.
// A stomped value would ungroup our notifications and break Action
// Center attribution.
var appNameMu sync.Mutex

const mosaicAppName = "Mosaic"

// BeeepNotifier is the production Notifier on macOS + Windows. Linux uses
// the LinuxDBusNotifier instead (see linux_notifier.go) because beeep
// doesn't expose D-Bus hints and Gnome needs the desktop-entry hint to
// attribute notifications to the right app icon / permission entry.
type BeeepNotifier struct{}

// Notify delivers a desktop notification. icon may be a path to a PNG file or
// "" to use a platform default. Errors from the underlying delivery surface
// are surfaced verbatim — the caller decides whether to log or ignore.
func (BeeepNotifier) Notify(title, body, icon string) error {
	appNameMu.Lock()
	beeep.AppName = mosaicAppName
	appNameMu.Unlock()
	if icon == "" {
		return beeep.Notify(title, body, nil)
	}
	return beeep.Notify(title, body, icon)
}

// NewBeeepNotifier returns the platform Notifier. Kept under the
// historical name so main.go's call site doesn't have to know about
// the per-OS split.
func NewBeeepNotifier() Notifier { return BeeepNotifier{} }
