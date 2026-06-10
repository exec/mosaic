package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config is the merged application configuration.
type Config struct {
	ListenPort       int    `yaml:"listen_port"`
	DefaultSavePath  string `yaml:"default_save_path"`
	EnableDHT        bool   `yaml:"enable_dht"`
	EnableEncryption bool   `yaml:"enable_encryption"`
}

func defaults() Config {
	cfg := Config{
		ListenPort:       6881,
		EnableDHT:        true,
		EnableEncryption: true,
	}
	// os.UserHomeDir fails when $HOME is unset (stripped-down service
	// environments, odd launchers). Joining "" with "Downloads" would yield
	// a CWD-relative path that scatters downloads wherever the process
	// happened to start; leave the default empty instead and let Load fail
	// loudly if neither YAML nor env supplies a path.
	if home, err := os.UserHomeDir(); err == nil {
		cfg.DefaultSavePath = filepath.Join(home, "Downloads")
	}
	return cfg
}

// Load returns config built from defaults, then overlaid with the YAML file at
// `path` (if it exists), then overlaid with env vars (prefix MOSAIC_).
// Missing files are not an error; unknown YAML keys and out-of-range values
// are (a typo'd key silently falling back to defaults is worse than a
// startup failure — consistent with corrupt YAML already being fatal).
func Load(path string) (Config, error) {
	cfg := defaults()

	if data, err := os.ReadFile(path); err == nil {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		// io.EOF means the file is empty (or only comments) — keep defaults.
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}

	if v := os.Getenv("MOSAIC_LISTEN_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("MOSAIC_LISTEN_PORT: %w", err)
		}
		cfg.ListenPort = n
	}
	if v := os.Getenv("MOSAIC_DEFAULT_SAVE_PATH"); v != "" {
		cfg.DefaultSavePath = v
	}
	if v := os.Getenv("MOSAIC_ENABLE_DHT"); v != "" {
		cfg.EnableDHT = v == "true" || v == "1"
	}
	if v := os.Getenv("MOSAIC_ENABLE_ENCRYPTION"); v != "" {
		cfg.EnableEncryption = v == "true" || v == "1"
	}

	// Validate the merged result (defaults + YAML + env). Mirrors mosaicd's
	// --port flag validation; 0 means "let the OS pick".
	if cfg.ListenPort < 0 || cfg.ListenPort > 65535 {
		return cfg, fmt.Errorf("listen_port must be between 0 and 65535 (got %d)", cfg.ListenPort)
	}
	if cfg.DefaultSavePath == "" {
		return cfg, errors.New("no default save path: home directory could not be resolved — set default_save_path in mosaic.yaml or MOSAIC_DEFAULT_SAVE_PATH")
	}

	return cfg, nil
}
