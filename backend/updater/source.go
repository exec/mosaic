// Package updater wraps creativeprojects/go-selfupdate behind a small Source +
// Updater seam so the rest of the app can check for / install upgrades without
// importing the third-party API directly. Tests inject fake Sources.
package updater

import (
	"context"
	"fmt"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

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
}

// GitHubSource wraps go-selfupdate's GitHub source. It lazy-initializes the
// underlying *selfupdate.Updater on first DetectLatest.
type GitHubSource struct {
	Owner string
	Repo  string
	// Channel is "stable" (default) or "beta"; beta accepts pre-release tags.
	Channel string

	cached *selfupdate.Updater
}

func (s *GitHubSource) lazyInit() (*selfupdate.Updater, error) {
	if s.cached != nil {
		return s.cached, nil
	}
	src, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, fmt.Errorf("github source: %w", err)
	}
	u, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: src,
		// Validator: nil — Plan 8 will plug in checksum/signature validators.
		Prerelease: s.Channel == "beta",
	})
	if err != nil {
		return nil, fmt.Errorf("new updater: %w", err)
	}
	s.cached = u
	return u, nil
}

func (s *GitHubSource) DetectLatest(ctx context.Context) (string, string, string, error) {
	u, err := s.lazyInit()
	if err != nil {
		return "", "", "", err
	}
	rel, found, err := u.DetectLatest(ctx, selfupdate.ParseSlug(fmt.Sprintf("%s/%s", s.Owner, s.Repo)))
	if err != nil {
		return "", "", "", err
	}
	if !found || rel == nil {
		return "", "", "", nil
	}
	if s.Channel != "beta" && rel.Prerelease {
		return "", "", "", nil
	}
	// rel.Version() comes back from Masterminds/semver as bare ("0.8.0"); the
	// rest of the app stores the build-time version with a "v" prefix, so
	// normalize at this boundary so all downstream version comparisons + UI
	// strings agree.
	return "v" + rel.Version(), rel.AssetURL, rel.AssetName, nil
}
