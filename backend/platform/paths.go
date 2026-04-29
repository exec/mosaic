package platform

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// Paths holds OS-appropriate directories for an app.
type AppPaths struct {
	ConfigDir string // user config (e.g. ~/Library/Application Support/Mosaic on macOS)
	DataDir   string // user data (db file lives here)
	LogDir    string // log files
}

// Paths returns app-qualified directories. Directories are not created; callers
// must mkdir as needed.
func Paths(app string) (AppPaths, error) {
	cfg := filepath.Join(xdg.ConfigHome, app)
	data := filepath.Join(xdg.DataHome, app)
	logs := filepath.Join(xdg.StateHome, app, "logs")
	return AppPaths{ConfigDir: cfg, DataDir: data, LogDir: logs}, nil
}
