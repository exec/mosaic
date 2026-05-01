package notifications

import "github.com/gen2brain/beeep"

// BeeepNotifier is the production Notifier — it forwards to gen2brain/beeep
// which dispatches to terminal-notifier/osascript on macOS, libnotify/DBus
// on Linux, and Windows toast on Windows.
type BeeepNotifier struct{}

func NewBeeepNotifier() *BeeepNotifier { return &BeeepNotifier{} }

// Notify delivers a desktop notification. icon may be a path to a PNG file or
// "" to use a platform default. Errors from the underlying delivery surface
// are surfaced verbatim — the caller decides whether to log or ignore.
func (BeeepNotifier) Notify(title, body, icon string) error {
	if icon == "" {
		return beeep.Notify(title, body, nil)
	}
	return beeep.Notify(title, body, icon)
}
