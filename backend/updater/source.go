// Package updater wraps creativeprojects/go-selfupdate behind a small Source +
// Updater seam so the rest of the app can check for / install upgrades without
// importing the third-party API directly. Tests inject fake Sources.
package updater

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// ChecksumManifestFilename names the asset that every release is expected to
// publish — a SHA-256 manifest (one `<hex>  <filename>` line per artifact, the
// shape `make-checksums.sh` emits). The lib's ChecksumValidator pulls it down,
// finds the row matching the release asset, and verifies the hash before
// `Updater.UpdateTo` overwrites the running binary.
const ChecksumManifestFilename = "SHA256SUMS"

// Source abstracts the release fetcher so tests can supply fakes.
type Source interface {
	// DetectLatest returns (versionTag, downloadURL, assetFilename, error).
	// versionTag is "v"-prefixed (e.g. "v0.8.0"); downloadURL points to the
	// platform-appropriate asset; assetFilename is the canonical filename
	// including extension (e.g. "mosaic_v0.8.0_darwin_arm64.tar.gz") —
	// go-selfupdate keys archive decompression off the extension at install
	// time, so passing it through preserves correctness. An empty tag with
	// nil error means "no release available for this OS/arch".
	DetectLatest(ctx context.Context) (version string, assetURL string, assetFilename string, err error)

	// Install downloads + verifies + applies the latest release detected by
	// the most recent successful DetectLatest call, replacing the binary at
	// exe. Source implementations carry the verification (e.g. SHA-256
	// manifest) so the Updater layer stays transport-agnostic.
	Install(ctx context.Context, exe string) error
}

// GitHubSource wraps go-selfupdate's GitHub source. It lazy-initializes the
// underlying *selfupdate.Updater on first DetectLatest, configures the lib's
// ChecksumValidator (which auto-fetches `SHA256SUMS` from each release and
// verifies SHA-256 before swap), and caches the most recently detected
// *selfupdate.Release so Install can hand it back to the lib's validating
// UpdateTo method form.
type GitHubSource struct {
	Owner string
	Repo  string
	// Channel is "stable" (default) or "beta"; beta accepts pre-release tags.
	Channel string

	mu          sync.Mutex
	cached      *selfupdate.Updater
	lastRelease *selfupdate.Release
}

func (s *GitHubSource) lazyInit() (*selfupdate.Updater, error) {
	if s.cached != nil {
		return s.cached, nil
	}
	src, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, fmt.Errorf("github source: %w", err)
	}
	cfg := selfupdate.Config{
		Source:     src,
		Validator:  &selfupdate.ChecksumValidator{UniqueFilename: ChecksumManifestFilename},
		Prerelease: s.Channel == "beta",
	}
	// Asset selection per platform — our release artifacts use a fixed naming
	// scheme (`Mosaic-vX.Y.Z-<os>-<arch[-variant]>.<ext>`) and we publish
	// multiple files per OS. Without these hints go-selfupdate's default
	// matcher (which expects runtime.GOARCH literally in the filename) fails
	// to find any asset on macOS, and ambiguously picks one on linux+windows.
	switch runtime.GOOS {
	case "darwin":
		// macOS ships a universal .dmg; runtime.GOARCH is arm64 or amd64 but
		// the asset is "darwin-universal". UniversalArch alone proved
		// insufficient in v0.1.9 testing (lib's default darwin matcher didn't
		// pick up our .dmg) — pin the asset with an explicit filter, same
		// pattern as linux/windows below.
		cfg.UniversalArch = "universal"
		cfg.Filters = []string{`darwin-universal\.dmg$`}
	case "linux":
		// We publish .deb + .rpm + .AppImage; only the AppImage is a single
		// self-contained ELF the lib can swap with the running binary.
		cfg.Filters = []string{`linux-amd64\.AppImage$`}
	case "windows":
		// We publish an NSIS installer + a portable .exe; the portable .exe
		// is the swap-in-place candidate.
		cfg.Filters = []string{`windows-amd64-portable\.exe$`}
	}
	u, err := selfupdate.NewUpdater(cfg)
	if err != nil {
		return nil, fmt.Errorf("new updater: %w", err)
	}
	s.cached = u
	return u, nil
}

func (s *GitHubSource) DetectLatest(ctx context.Context) (string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, err := s.lazyInit()
	if err != nil {
		return "", "", "", err
	}
	rel, found, err := u.DetectLatest(ctx, selfupdate.ParseSlug(fmt.Sprintf("%s/%s", s.Owner, s.Repo)))
	if err != nil {
		return "", "", "", err
	}
	if !found || rel == nil {
		s.lastRelease = nil
		return "", "", "", nil
	}
	if s.Channel != "beta" && rel.Prerelease {
		s.lastRelease = nil
		return "", "", "", nil
	}
	s.lastRelease = rel
	// rel.Version() comes back from Masterminds/semver as bare ("0.8.0"); the
	// rest of the app stores the build-time version with a "v" prefix, so
	// normalize at this boundary so all downstream version comparisons + UI
	// strings agree.
	return "v" + rel.Version(), rel.AssetURL, rel.AssetName, nil
}

// Install runs the lib's validating UpdateTo against the cached *Release. The
// ChecksumValidator configured on the underlying *selfupdate.Updater pulls
// SHA256SUMS from the release, finds the row for AssetName, and aborts the
// swap if the hash doesn't match — so a tampered asset can't replace the
// running binary.
func (s *GitHubSource) Install(ctx context.Context, exe string) error {
	s.mu.Lock()
	u := s.cached
	rel := s.lastRelease
	s.mu.Unlock()
	if u == nil || rel == nil {
		return fmt.Errorf("install: no release detected; call DetectLatest first")
	}
	return u.UpdateTo(ctx, rel, exe)
}
