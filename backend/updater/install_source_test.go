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
	withSentinelPaths(t, []string{filepath.Join(t.TempDir(), "absent")})
	withDpkgGlob(t, filepath.Join(t.TempDir(), "no-such-*.list"))

	got := DetectInstallSource()
	if runtime.GOOS != "linux" {
		if got != InstallSourceManual {
			t.Errorf("non-Linux should default to manual; got %q", got)
		}
		return
	}
	if got != InstallSourceManual {
		t.Errorf("Linux without sentinel or dpkg list should default to manual; got %q", got)
	}
}

// isAPTInstalled is platform-agnostic at the file-IO layer; the Linux-
// gating happens one level up in DetectInstallSource. Stubbing both
// signals lets us exercise it on any host.
func TestIsAPTInstalled_FalseWhenSignalsAbsent(t *testing.T) {
	withSentinelPaths(t, []string{filepath.Join(t.TempDir(), "absent")})
	withDpkgGlob(t, filepath.Join(t.TempDir(), "no-such-*.list"))
	if isAPTInstalled() {
		t.Errorf("isAPTInstalled returned true with no sentinel and no mosaic*.list present")
	}
}

func TestIsAPTInstalled_SentinelPresent(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "installed-by-apt")
	if err := os.WriteFile(sentinel, []byte("mosaic 2026-05-02T00:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	withSentinelPaths(t, []string{sentinel})
	// Point the dpkg fallback somewhere with no matches so we know the
	// positive answer came from the sentinel, not the legacy path.
	withDpkgGlob(t, filepath.Join(t.TempDir(), "no-such-*.list"))

	if !isAPTInstalled() {
		t.Errorf("isAPTInstalled returned false despite sentinel at %q", sentinel)
	}
}

func TestIsAPTInstalled_FallbackToDpkgList(t *testing.T) {
	// Sentinel absent → fall through to the legacy dpkg .list parser.
	// Stage a fake mosaic.list that enumerates the running test binary's
	// path so the matcher succeeds.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dpkgDir := t.TempDir()
	listFile := filepath.Join(dpkgDir, "mosaic.list")
	if err := os.WriteFile(listFile, []byte(exe+"\n"), 0o644); err != nil {
		t.Fatalf("write list file: %v", err)
	}
	withSentinelPaths(t, []string{filepath.Join(t.TempDir(), "absent")})
	withDpkgGlob(t, filepath.Join(dpkgDir, "mosaic*.list"))

	if !isAPTInstalled() {
		t.Errorf("isAPTInstalled returned false despite dpkg .list listing %q", exe)
	}
}

func TestSentinelExists_RejectsDirectory(t *testing.T) {
	// A directory at the sentinel path must NOT count — we wrote a regular
	// file in postinst, anything else there is unexpected and untrusted.
	dir := t.TempDir()
	if sentinelExists(dir) {
		t.Errorf("sentinelExists(%q) returned true for a directory", dir)
	}
}

func TestSentinelExists_FileFound(t *testing.T) {
	f := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(f, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !sentinelExists(f) {
		t.Errorf("sentinelExists(%q) returned false for a regular file", f)
	}
}

// withSentinelPaths swaps the package-level sentinel list for a test and
// restores it on cleanup. The seam is intentionally tiny (a []string
// var) so production callers stay path-clean.
func withSentinelPaths(t *testing.T, paths []string) {
	t.Helper()
	prev := sentinelPaths
	sentinelPaths = paths
	t.Cleanup(func() { sentinelPaths = prev })
}

// withDpkgGlob swaps the legacy dpkg .list glob for a test.
func withDpkgGlob(t *testing.T, glob string) {
	t.Helper()
	prev := dpkgListGlob
	dpkgListGlob = glob
	t.Cleanup(func() { dpkgListGlob = prev })
}
