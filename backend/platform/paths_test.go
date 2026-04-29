package platform

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaths_ReturnsAppQualifiedDirs(t *testing.T) {
	p, err := Paths("Mosaic")
	require.NoError(t, err)

	require.True(t, filepath.IsAbs(p.ConfigDir), "ConfigDir should be absolute")
	require.True(t, filepath.IsAbs(p.DataDir), "DataDir should be absolute")
	require.True(t, filepath.IsAbs(p.LogDir), "LogDir should be absolute")

	// All three should include the app name as a path segment so we don't
	// pollute the user's directories.
	require.True(t, strings.Contains(p.ConfigDir, "Mosaic"))
	require.True(t, strings.Contains(p.DataDir, "Mosaic"))
	require.True(t, strings.Contains(p.LogDir, "Mosaic"))
}
