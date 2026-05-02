//go:build linux

package tray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AppIndicatorExtensionUUID is the well-known shell-extension uuid that
// the gnome-shell-extension-appindicator package installs. The
// `gnome-extensions enable/list` CLI keys off this exact string.
const AppIndicatorExtensionUUID = "appindicatorsupport@rgcjonas.gmail.com"

// IsGnomeSession reports whether the current desktop session is Gnome.
//
// Detection uses XDG_CURRENT_DESKTOP, which is the freedesktop standard
// session identifier. Vanilla Gnome sets it to "GNOME"; Ubuntu sets it
// to "ubuntu:GNOME"; Gnome Classic to "GNOME-Classic:GNOME". A simple
// case-insensitive Contains check covers all variants without false-
// positiving on KDE/XFCE/MATE/Cinnamon (none of those contain "gnome").
func IsGnomeSession() bool {
	return strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "GNOME")
}

// AppIndicatorExtensionInstalled reports whether the AppIndicator shell
// extension is installed somewhere gnome-shell will find it. Looks in
// the system-wide path (where the .deb's Recommends puts it) and the
// per-user path (where users self-install via extensions.gnome.org).
func AppIndicatorExtensionInstalled() bool {
	for _, p := range appIndicatorExtensionSearchPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func appIndicatorExtensionSearchPaths() []string {
	paths := []string{
		"/usr/share/gnome-shell/extensions/" + AppIndicatorExtensionUUID,
	}
	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths,
			filepath.Join(home, ".local/share/gnome-shell/extensions", AppIndicatorExtensionUUID),
		)
	}
	return paths
}

// AppIndicatorExtensionEnabled reports whether `gnome-extensions list
// --enabled` includes the AppIndicator uuid. Note: "enabled in dconf"
// does not mean "loaded by the running gnome-shell" — the user may
// still need to log out + back in for a freshly-enabled extension to
// activate. Use Available() to test the actual runtime state.
func AppIndicatorExtensionEnabled(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gnome-extensions", "list", "--enabled")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == AppIndicatorExtensionUUID {
			return true
		}
	}
	return false
}

// EnableAppIndicatorExtension flips the dconf key that gnome-shell
// reads at startup to populate its enabled-extensions list. Runs as
// the current user (which is what Mosaic is running as), so the dconf
// write lands in the right profile. The user still needs to restart
// the shell for the change to take effect — Mosaic's UI surfaces that
// requirement after this returns.
func EnableAppIndicatorExtension(ctx context.Context) error {
	if _, err := exec.LookPath("gnome-extensions"); err != nil {
		return errors.New("gnome-extensions CLI not found on PATH")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gnome-extensions", "enable", AppIndicatorExtensionUUID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gnome-extensions enable: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GnomePromptStatus is the high-level state the SPA renders against.
type GnomePromptStatus string

const (
	// GnomePromptStatusNotApplicable: not Linux, not Gnome, OR a tray
	// watcher is already serving (so a tray icon will render — no
	// prompt needed).
	GnomePromptStatusNotApplicable GnomePromptStatus = "not_applicable"
	// GnomePromptStatusNeedsInstall: Gnome session, no watcher, the
	// AppIndicator extension isn't on disk. We can't install it
	// ourselves (apt requires root); the SPA shows copy-pasteable
	// instructions.
	GnomePromptStatusNeedsInstall GnomePromptStatus = "needs_install"
	// GnomePromptStatusNeedsEnable: extension installed but not enabled
	// in the user's dconf profile. EnableAppIndicatorExtension flips it.
	GnomePromptStatusNeedsEnable GnomePromptStatus = "needs_enable"
	// GnomePromptStatusNeedsRestart: extension enabled in dconf but the
	// running gnome-shell hasn't picked it up yet. User needs to log
	// out + back in (Wayland) or `Alt+F2 → r` (X11).
	GnomePromptStatusNeedsRestart GnomePromptStatus = "needs_restart"
)

// EvaluateGnomePromptStatus inspects the live session and returns the
// status the SPA should render. Cheap to call (a couple of stat() calls
// + at most one short-timeout `gnome-extensions list --enabled`
// subprocess); safe to call on every startup tick.
func EvaluateGnomePromptStatus(ctx context.Context) GnomePromptStatus {
	if !IsGnomeSession() {
		return GnomePromptStatusNotApplicable
	}
	if Available() {
		return GnomePromptStatusNotApplicable
	}
	if !AppIndicatorExtensionInstalled() {
		return GnomePromptStatusNeedsInstall
	}
	if AppIndicatorExtensionEnabled(ctx) {
		return GnomePromptStatusNeedsRestart
	}
	return GnomePromptStatusNeedsEnable
}
