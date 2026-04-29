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
	// DetectLatest returns (versionTag, downloadURL, error). versionTag is the
	// raw tag (e.g. "v0.8.0"); downloadURL points to the platform-appropriate
	// asset the caller will hand to Apply. An empty tag with nil error means
	// "no release available for this OS/arch".
	DetectLatest(ctx context.Context) (version string, assetURL string, err error)
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

func (s *GitHubSource) DetectLatest(ctx context.Context) (string, string, error) {
	u, err := s.lazyInit()
	if err != nil {
		return "", "", err
	}
	rel, found, err := u.DetectLatest(ctx, selfupdate.ParseSlug(fmt.Sprintf("%s/%s", s.Owner, s.Repo)))
	if err != nil {
		return "", "", err
	}
	if !found || rel == nil {
		return "", "", nil
	}
	if s.Channel != "beta" && rel.Prerelease {
		return "", "", nil
	}
	return rel.Version(), rel.AssetURL, nil
}
