//go:build !linux

package tray

// Available is true on platforms whose shells own the system tray
// natively. Windows has the explorer notification area; macOS has the
// menu bar (we skip systray on darwin and use NSStatusItem there
// directly via the tray_darwin.* bridge).
func Available() bool { return true }
