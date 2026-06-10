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

func TestLoad_UnknownYAMLKeyFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.yaml")
	// "listen-port" is a typo of "listen_port" — must fail loudly rather
	// than silently keeping the default.
	require.NoError(t, os.WriteFile(path, []byte(`listen-port: 51413`), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "listen-port")
}

func TestLoad_EmptyFileKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.yaml")
	require.NoError(t, os.WriteFile(path, []byte("# comments only\n"), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 6881, cfg.ListenPort)
}

func TestLoad_ListenPortRangeValidated(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		env  string
		ok   bool
	}{
		{name: "negative yaml", yaml: "listen_port: -1", ok: false},
		{name: "too large yaml", yaml: "listen_port: 65536", ok: false},
		{name: "too large env", env: "70000", ok: false},
		{name: "zero means OS-picked", yaml: "listen_port: 0", ok: true},
		{name: "max valid", yaml: "listen_port: 65535", ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "mosaic.yaml")
			if tc.yaml != "" {
				require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o644))
			}
			if tc.env != "" {
				t.Setenv("MOSAIC_LISTEN_PORT", tc.env)
			}
			_, err := Load(path)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), "listen_port")
			}
		})
	}
}

func TestLoad_NoHomeDirFailsWithoutConfiguredSavePath(t *testing.T) {
	// With $HOME unset os.UserHomeDir errors; the old code silently fell
	// back to a CWD-relative "Downloads". Now it must fail loudly...
	t.Setenv("HOME", "")
	dir := t.TempDir()

	_, err := Load(filepath.Join(dir, "missing.yaml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "default_save_path")

	// ...unless the config (or env) supplies a save path explicitly.
	path := filepath.Join(dir, "mosaic.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`default_save_path: /srv/dl`), 0o644))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "/srv/dl", cfg.DefaultSavePath)
}
