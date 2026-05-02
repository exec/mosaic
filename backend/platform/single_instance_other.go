//go:build !windows && !linux

package platform

// EarlyForwardLaunchArgs is a no-op on darwin and the BSDs. macOS uses
// Wails's Mac.OnFileOpen / OnUrlOpen at the AppDelegate layer, which fires
// before any heavy init.
func EarlyForwardLaunchArgs(uniqueId string) bool {
	return false
}

// StartSecondInstanceListener is a no-op on platforms whose Wails IPC
// already routes second-instance launches reliably (macOS via the
// AppDelegate). Linux owns its own implementation in
// single_instance_linux.go.
func StartSecondInstanceListener(uniqueId string, onArgs func(args []string)) error {
	return nil
}

// CleanupSingleInstance is a no-op on platforms without a Mosaic-owned
// IPC socket to unlink.
func CleanupSingleInstance() {}
