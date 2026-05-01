//go:build !windows

package platform

// EnsureFileAssociations is a no-op on non-Windows platforms. macOS uses
// CFBundleDocumentTypes / CFBundleURLTypes in Info.plist (set at .app build
// time, not runtime); Linux uses .desktop files installed by the package
// manager. Both are inherently install-time concerns there.
func EnsureFileAssociations(exePath string) error {
	return nil
}
