//go:build linux

package tray

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIsGnomeSession(t *testing.T) {
	cases := []struct {
		desktop string
		want    bool
	}{
		{"GNOME", true},
		{"ubuntu:GNOME", true},
		{"GNOME-Classic:GNOME", true},
		{"gnome", true}, // case-insensitive
		{"KDE", false},
		{"XFCE", false},
		{"X-Cinnamon", false},
		{"MATE", false},
		{"", false},
		{"Pop:GNOME", true}, // Pop!_OS
	}
	for _, tc := range cases {
		t.Setenv("XDG_CURRENT_DESKTOP", tc.desktop)
		got := IsGnomeSession()
		if got != tc.want {
			t.Errorf("IsGnomeSession with XDG_CURRENT_DESKTOP=%q: got %v, want %v", tc.desktop, got, tc.want)
		}
	}
}

func TestAppIndicatorExtensionInstalled_NotPresent(t *testing.T) {
	// Point HOME at an empty temp dir and ensure the system path doesn't
	// exist on this test runner. (The CI containers don't ship gnome.)
	t.Setenv("HOME", t.TempDir())
	if AppIndicatorExtensionInstalled() {
		// If somehow this runs on a Gnome host with the extension installed,
		// skip rather than fail — the test is meant for clean CI.
		t.Skip("AppIndicator extension is installed on this host; skipping negative test")
	}
}

func TestAppIndicatorExtensionInstalled_UserPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local/share/gnome-shell/extensions", AppIndicatorExtensionUUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed extension dir: %v", err)
	}
	if !AppIndicatorExtensionInstalled() {
		t.Errorf("AppIndicatorExtensionInstalled returned false despite a seeded user-path extension at %s", dir)
	}
}

func TestEvaluateGnomePromptStatus_NotApplicableOffGnome(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	got := EvaluateGnomePromptStatus(context.Background())
	if got != GnomePromptStatusNotApplicable {
		t.Errorf("non-Gnome session should be not_applicable, got %q", got)
	}
}
