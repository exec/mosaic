//go:build !darwin

package platform

// InstallAppleEventHandlers is a no-op outside macOS. Linux + Windows pass
// file paths and URLs via argv (or via SingleInstanceLock's
// OnSecondInstanceLaunch when a second process is launched), so they don't
// need an Apple Event bridge.
func InstallAppleEventHandlers(onFile, onURL func(string)) {}
