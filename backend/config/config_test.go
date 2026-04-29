package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_ReturnsDefaultsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing.yaml"))
	require.NoError(t, err)

	require.Equal(t, 6881, cfg.ListenPort)
	require.NotEmpty(t, cfg.DefaultSavePath)
	require.True(t, cfg.EnableDHT)
	require.True(t, cfg.EnableEncryption)
}

func TestLoad_OverridesFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
listen_port: 51413
default_save_path: /tmp/dl
enable_dht: false
enable_encryption: false
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, 51413, cfg.ListenPort)
	require.Equal(t, "/tmp/dl", cfg.DefaultSavePath)
	require.False(t, cfg.EnableDHT)
	require.False(t, cfg.EnableEncryption)
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`listen_port: 51413`), 0o644))

	t.Setenv("MOSAIC_LISTEN_PORT", "9999")
	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, 9999, cfg.ListenPort)
}
