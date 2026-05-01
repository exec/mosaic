//go:build !windows

package platform

// EarlyForwardLaunchArgs is a no-op on non-Windows platforms. macOS uses
// Wails's Mac.OnFileOpen / OnUrlOpen at the AppDelegate layer, which fires
// before any heavy init. Linux launches a fresh process whose argv path
// also goes through normal startup.
func EarlyForwardLaunchArgs(uniqueId string) bool {
	return false
}
