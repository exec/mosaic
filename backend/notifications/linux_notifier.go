//go:build linux

package notifications

import (
	"fmt"
	"sync"
	"time"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
)

// LinuxDBusNotifier delivers notifications by talking directly to the
// freedesktop.org Notifications service over the D-Bus session bus. We
// don't use beeep on Linux because:
//
//   1. beeep.AppName is a package-global that defaults to "DefaultAppName".
//      Even setting it to "Mosaic" doesn't get us proper Gnome integration
//      because beeep doesn't expose hints, so the desktop-entry hint that
//      ties the notification back to /usr/share/applications/mosaic.desktop
//      is never sent. Without that, Gnome groups our notifications under a
//      generic header (no icon, no per-app permission entry in
//      Settings → Notifications), which on some configurations causes the
//      user to miss them entirely.
//
//   2. We can use the freedesktop "themed icon name" form for AppIcon —
//      passing the bare string "mosaic" — and the notification daemon
//      resolves it via the user's icon theme to whatever variant of
//      mosaic.png is best for the rendered size. No need to hard-code an
//      absolute path that varies between apt installs / AppImage / dev
//      runs.
//
// macOS and Windows still use beeep (see notifier_other.go); their default
// notification surfaces don't have these issues.
type LinuxDBusNotifier struct {
	mu   sync.Mutex
	conn *dbus.Conn // lazily connected, reused across notifications
}

func newLinuxDBusNotifier() *LinuxDBusNotifier {
	return &LinuxDBusNotifier{}
}

// Notify sends one notification. icon is forwarded as-is; pass "" to fall
// back to the default ("mosaic" themed icon name).
func (n *LinuxDBusNotifier) Notify(title, body, icon string) error {
	conn, err := n.session()
	if err != nil {
		return fmt.Errorf("notifications: dbus session bus: %w", err)
	}

	if icon == "" {
		icon = defaultLinuxIconName
	}

	note := notify.Notification{
		AppName:       linuxAppName,
		AppIcon:       icon,
		Summary:       title,
		Body:          body,
		ExpireTimeout: 5 * time.Second,
	}
	// desktop-entry hint links this notification to mosaic.desktop so
	// gnome-shell can attribute it to the correct app icon, expose
	// per-app permission controls in Settings → Notifications, and
	// honor the user's per-app DND choices. The hint key is exactly
	// "desktop-entry" per the freedesktop notification spec.
	note.Hints = map[string]dbus.Variant{
		"desktop-entry": dbus.MakeVariant(linuxDesktopEntry),
	}

	_, err = notify.SendNotification(conn, note)
	if err != nil {
		return fmt.Errorf("notifications: send: %w", err)
	}
	return nil
}

func (n *LinuxDBusNotifier) session() (*dbus.Conn, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.conn != nil {
		return n.conn, nil
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	n.conn = conn
	return conn, nil
}

const (
	// linuxAppName is what shows above the notification body in shells
	// that render an app-name header (KDE Plasma, some XFCE themes).
	// Gnome itself derives the displayed app name from desktop-entry,
	// not this field, but other shells use it.
	linuxAppName = "Mosaic"
	// linuxDesktopEntry MUST match the basename of our .desktop file
	// (without the .desktop suffix). The deb installs
	// /usr/share/applications/mosaic.desktop, so this is "mosaic".
	linuxDesktopEntry = "mosaic"
	// defaultLinuxIconName uses freedesktop-spec themed icon names.
	// The deb ships /usr/share/icons/hicolor/512x512/apps/mosaic.png,
	// which the icon theme resolves from the bare name "mosaic".
	defaultLinuxIconName = "mosaic"
)
