//go:build windows

package platform

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// EnsureFileAssociations writes the HKCU registry entries that register
// Mosaic as a .torrent file handler and a magnet: URL-scheme handler.
// Mirrors build/windows/installer/project.nsi exactly so the runtime path
// produces the same result as a fresh install. Idempotent — safe to call
// every startup.
//
// This exists because go-selfupdate's binary swap doesn't run any installer
// code; users who installed an older version (pre-v0.1.13, before NSIS
// included the file-association block) and have been auto-updating since
// will never have these entries until they reinstall via the .exe — unless
// the running app writes them itself, which is what this does.
//
// We don't touch HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\
// FileExts\.torrent\UserChoice — that's where Windows records the user's
// explicit "always open with X" choice, and we don't want to override it.
// Our writes just register Mosaic as an option; the user still confirms
// the default via Settings → Default apps.
func EnsureFileAssociations(exePath string) error {
	return ensureFileAssociations(registry.CURRENT_USER, exePath)
}

func ensureFileAssociations(root registry.Key, exePath string) error {
	cmd := fmt.Sprintf(`"%s" "%%1"`, exePath)
	icon := fmt.Sprintf(`%s,0`, exePath)
	const classes = `Software\Classes`

	writes := []struct {
		path  string
		name  string
		value string
	}{
		// Register our own handler without replacing another client's defaults.
		{classes + `\.torrent\OpenWithProgids`, "MosaicTorrent", ""},
		{classes + `\MosaicTorrent`, "", "BitTorrent file"},
		{classes + `\MosaicTorrent`, "URL Protocol", ""},
		{classes + `\MosaicTorrent\DefaultIcon`, "", icon},
		{classes + `\MosaicTorrent\shell\open\command`, "", cmd},

		// RegisteredApplications + Capabilities — surfaces Mosaic in
		// Settings → Default apps for the user to confirm.
		{`Software\RegisteredApplications`, "Mosaic", `Software\Mosaic\Capabilities`},
		{`Software\Mosaic\Capabilities`, "ApplicationName", "Mosaic"},
		{`Software\Mosaic\Capabilities`, "ApplicationDescription", "BitTorrent client"},
		{`Software\Mosaic\Capabilities\FileAssociations`, ".torrent", "MosaicTorrent"},
		{`Software\Mosaic\Capabilities\URLAssociations`, "magnet", "MosaicTorrent"},
	}

	for _, w := range writes {
		k, _, err := registry.CreateKey(root, w.path, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("create %q: %w", w.path, err)
		}
		err = k.SetStringValue(w.name, w.value)
		k.Close()
		if err != nil {
			return fmt.Errorf("set %q.%q: %w", w.path, w.name, err)
		}
	}
	return nil
}
