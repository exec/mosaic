package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallSource describes how the running binary got onto disk. The
// auto-updater consults this to decide whether to run at all: an
// apt-installed Mosaic should defer upgrades to apt (modifying dpkg's
// view of the installed version is root-only, breaks dpkg --verify,
// and would be clobbered on the next `apt upgrade` anyway), while
// AppImage / manual installs rely on the in-app updater.
type InstallSource string

const (
	// InstallSourceAPT: dpkg owns the running binary. Detected via the
	// sentinel file written by our .deb postinst (see scripts/
	// linux-postinstall.sh) with a fallback to globbing dpkg's bookkeeping.
	InstallSourceAPT InstallSource = "apt"
	// InstallSourceAppImage: the AppImage runtime is wrapping us. The
	// AppImage env var is set by the runtime before exec'ing the
	// payload binary; checking for it is the recommended detection per
	// the AppImage spec (https://docs.appimage.org/packaging-guide/environment-variables.html).
	InstallSourceAppImage InstallSource = "appimage"
	// InstallSourceManual: anything else — local cargo/go build,
	// extracted tarball, portable .exe, .dmg-installed .app, etc.
	// The auto-updater is the only upgrade path here, so it stays on.
	InstallSourceManual InstallSource = "manual"
)

// sentinelPathAPT is the canonical apt-managed marker file. The .deb
// postinst writes it on `configure`; the postremove deletes it on `purge`.
// We own both ends, so this is the authoritative signal — no parsing,
// no path-matching, just a stat call.
//
// Mirrored as a literal in scripts/linux-postinstall.sh and
// scripts/linux-postremove.sh — keep all three in sync.
const sentinelPathAPT = "/usr/share/mosaic/installed-by-apt"

// sentinelPaths lets tests inject alternative marker paths via t.TempDir
// without touching the real /usr/share/mosaic. Production code reads
// the package-level default; tests override and restore.
var sentinelPaths = []string{sentinelPathAPT}

// dpkgListGlob is the legacy fallback for users who installed mosaic from
// a .deb that predates the sentinel-writing postinst (≤ v0.4.3). Once the
// install base has rolled past that release we can drop this. Exposed as
// a var (not const) so tests can point it elsewhere.
var dpkgListGlob = "/var/lib/dpkg/info/mosaic*.list"

// DetectInstallSource inspects the runtime to classify how the binary
// was installed. Cheap (one env lookup + at most two stat calls); safe
// to call at startup before anything else binds resources.
func DetectInstallSource() InstallSource {
	// AppImage check MUST come first: an AppImage payload running on a
	// host that previously had a .deb installed (and never purged it)
	// would otherwise mis-detect as apt-managed and disable in-app
	// updates that the AppImage actually needs.
	if os.Getenv("APPIMAGE") != "" {
		return InstallSourceAppImage
	}
	if runtime.GOOS == "linux" && isAPTInstalled() {
		return InstallSourceAPT
	}
	return InstallSourceManual
}

// isAPTInstalled reports whether the running binary is managed by apt.
//
// Primary signal: a sentinel file written by our postinst. We own both
// ends of that contract so there's nothing to misinterpret — file
// present = apt managed.
//
// Fallback signal: the legacy dpkg .list glob. Retained for backward
// compatibility with users upgrading from ≤ v0.4.3 whose installed .deb
// never ran the new postinst. Safe to delete in a future release once
// the install base has rolled forward (and the cleaner, sentinel-only
// path is what every new install has been on for several versions).
func isAPTInstalled() bool {
	for _, p := range sentinelPaths {
		if sentinelExists(p) {
			return true
		}
	}
	return aptListMatch()
}

// sentinelExists reports whether path exists and is a regular file.
// Symlinks, directories, and other types are intentionally rejected:
// the sentinel is a small text file we wrote ourselves, anything else
// at that path is suspicious and we'd rather not trust it.
func sentinelExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// maxDpkgListSize caps how large a dpkg .list file we're willing to read
// into memory before line-scanning. Real mosaic*.list files are a few KB;
// anything multi-megabyte is corrupt or hostile, so we refuse it rather
// than risk a large allocation. dpkg .list files for normal packages are
// well under this.
const maxDpkgListSize = 4 << 20 // 4 MiB

// aptListMatch is the legacy detection path: search /var/lib/dpkg/info/
// mosaic*.list for a line that names the running exe path. Kept for users
// on pre-sentinel installs; remove once those have rolled forward.
//
// Paths are normalized on both sides (filepath.Clean, plus EvalSymlinks
// where the target exists) so a symlinked install or a /usr/local shim
// — e.g. /usr/bin/mosaic → /opt/mosaic/mosaic, or a dpkg line recorded
// as a relative-ish path — isn't misclassified as a manual install. We
// stay conservative: only a confident path match returns apt-managed.
func aptListMatch() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// /proc/self/exe (what os.Executable resolves to on Linux) is
	// already a symlink-resolved absolute path, but a user could have
	// re-symlinked /usr/bin/mosaic → /opt/something/mosaic. Resolve
	// once more defensively, then Clean both forms.
	exeClean := filepath.Clean(exe)
	resolved := exeClean
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		resolved = filepath.Clean(r)
	}

	// Search all .list files matching mosaic*.list — covers both the
	// plain `mosaic.list` (native package) and `mosaic:amd64.list`
	// (multiarch convention) without us having to know which we are.
	matches, err := filepath.Glob(dpkgListGlob)
	if err != nil || len(matches) == 0 {
		return false
	}
	for _, listFile := range matches {
		info, err := os.Stat(listFile)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxDpkgListSize {
			continue
		}
		data, err := os.ReadFile(listFile)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Normalize each dpkg-recorded path the same way we
			// normalized the exe: Clean, plus EvalSymlinks when the
			// path exists on disk (catches /usr/bin/mosaic shims that
			// point at the real binary).
			lineClean := filepath.Clean(line)
			if lineClean == exeClean || lineClean == resolved {
				return true
			}
			if lineResolved, err := filepath.EvalSymlinks(line); err == nil {
				lr := filepath.Clean(lineResolved)
				if lr == exeClean || lr == resolved {
					return true
				}
			}
		}
	}
	return false
}
