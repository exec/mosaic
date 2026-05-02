package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDetectInstallSource_AppImageEnvWins(t *testing.T) {
	t.Setenv("APPIMAGE", "/tmp/Mosaic.AppImage")
	if got := DetectInstallSource(); got != InstallSourceAppImage {
		t.Errorf("APPIMAGE set should yield appimage; got %q", got)
	}
}

func TestDetectInstallSource_DefaultIsManual(t *testing.T) {
	// Off-Linux always falls through to manual. On Linux test runners
	// without dpkg-installed mosaic, also manual. Confirm the default
	// path doesn't false-positive when neither signal is present.
	t.Setenv("APPIMAGE", "")
	got := DetectInstallSource()
	if runtime.GOOS != "linux" {
		if got != InstallSourceManual {
			t.Errorf("non-Linux should default to manual; got %q", got)
		}
		return
	}
	// On a Linux test runner without /var/lib/dpkg/info/mosaic*.list, we
	// expect manual. If the list file IS present (someone is running
	// tests on a mosaic-installed box), skip rather than fail.
	matches, _ := filepath.Glob("/var/lib/dpkg/info/mosaic*.list")
	if len(matches) > 0 {
		t.Skip("dpkg has mosaic*.list on this host; skipping default-is-manual test")
	}
	if got != InstallSourceManual {
		t.Errorf("Linux without dpkg list should default to manual; got %q", got)
	}
}

// On non-Linux hosts the dpkg-list parsing is dead code. This test
// uses a runtime check rather than build tags so the test file stays
// portable.
func TestIsAPTInstalled_FalseWhenListMissing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isAPTInstalled is Linux-only behavior")
	}
	if _, err := os.Stat("/var/lib/dpkg/info/mosaic.list"); err == nil {
		t.Skip("dpkg list present on host; skipping negative test")
	}
	if isAPTInstalled() {
		t.Errorf("isAPTInstalled returned true with no mosaic*.list present")
	}
}
