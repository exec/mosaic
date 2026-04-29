package config

import (
	"errors"
	"fmt"
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
	home, _ := os.UserHomeDir()
	return Config{
		ListenPort:       6881,
		DefaultSavePath:  filepath.Join(home, "Downloads"),
		EnableDHT:        true,
		EnableEncryption: true,
	}
}

// Load returns config built from defaults, then overlaid with the YAML file at
// `path` (if it exists), then overlaid with env vars (prefix MOSAIC_).
// Missing files are not an error.
func Load(path string) (Config, error) {
	cfg := defaults()

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
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

	return cfg, nil
}
