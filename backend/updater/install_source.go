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
	// InstallSourceAPT: dpkg owns the running binary. The /var/lib/dpkg
	// .list file for the mosaic package enumerates our exe path.
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

// DetectInstallSource inspects the runtime to classify how the binary
// was installed. Cheap (one env lookup + at most two stat calls); safe
// to call at startup before anything else binds resources.
func DetectInstallSource() InstallSource {
	if os.Getenv("APPIMAGE") != "" {
		return InstallSourceAppImage
	}
	if runtime.GOOS == "linux" && isAPTInstalled() {
		return InstallSourceAPT
	}
	return InstallSourceManual
}

// isAPTInstalled reports whether dpkg believes the running binary
// belongs to the mosaic package. This is the authoritative check —
// /var/lib/dpkg/info/<pkg>.list enumerates every file dpkg installed
// for a package, so the running exe path appearing in there means we
// ARE the apt-managed binary (and updating ourselves out of band would
// desync from dpkg).
func isAPTInstalled() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// /proc/self/exe (what os.Executable resolves to on Linux) is
	// already a symlink-resolved absolute path, but a user could have
	// re-symlinked /usr/bin/mosaic → /opt/something/mosaic. Resolve
	// once more defensively.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}

	// Search all .list files matching mosaic*.list — covers both the
	// plain `mosaic.list` (native package) and `mosaic:amd64.list`
	// (multiarch convention) without us having to know which we are.
	matches, err := filepath.Glob("/var/lib/dpkg/info/mosaic*.list")
	if err != nil || len(matches) == 0 {
		return false
	}
	for _, listFile := range matches {
		data, err := os.ReadFile(listFile)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if line == exe || line == resolved {
				return true
			}
		}
	}
	return false
}
