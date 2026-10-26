package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"mosaic/backend/updater"
)

type desktopUpdateSource struct {
	installed  bool
	installErr error
}

func (s *desktopUpdateSource) DetectLatest(context.Context) (string, string, string, error) {
	return "v1.10.0", "https://example.invalid/update", "mosaic.tar.gz", nil
}
func (s *desktopUpdateSource) Install(context.Context, string) error {
	s.installed = true
	return s.installErr
}

func TestDesktopUpdaterAdapter(t *testing.T) {
	source := &desktopUpdateSource{}
	d := desktopUpdater{updater.New(updater.Config{CurrentVersion: "v1.9.0", Source: source}), "v1.9.0"}
	info, err := d.Check(context.Background())
	require.NoError(t, err)
	require.True(t, info.Available)
	require.Equal(t, "v1.9.0", info.CurrentVersion)
	require.Equal(t, "v1.10.0", info.LatestVersion)
	require.Equal(t, "mosaic.tar.gz", info.AssetFilename)
	require.Positive(t, info.CheckedAt)
	version, err := d.Install(context.Background())
	require.NoError(t, err)
	require.True(t, source.installed)
	require.Equal(t, info.LatestVersion, version)
	source.installErr = errors.New("install failed")
	_, err = d.Install(context.Background())
	require.ErrorIs(t, err, source.installErr)
}
