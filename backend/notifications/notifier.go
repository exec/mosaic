// Package notifications fans engine + updater state transitions out to OS-level
// desktop notifications. The actual delivery surface is wrapped in the Notifier
// interface so tests can inject a fake without touching DBus / NSUserNotification
// / Win32 APIs.
package notifications

// Notifier is the minimal surface the subscriber needs to deliver an alert.
// title is the headline ("Download complete"), body is a short detail
// ("ubuntu-24.04.iso"), capped to ≈120 chars by the caller. icon is an
// optional path to a PNG; pass "" to use the OS default.
type Notifier interface {
	Notify(title, body, icon string) error
}

// NoopNotifier is a Notifier that does nothing. Useful in tests and as a
// safe default before the real beeep-backed notifier is constructed.
type NoopNotifier struct{}

func (NoopNotifier) Notify(_, _, _ string) error { return nil }
