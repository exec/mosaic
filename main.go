package main

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"mosaic/backend/api"
	"mosaic/backend/config"
	"mosaic/backend/engine"
	"mosaic/backend/logging"
	"mosaic/backend/persistence"
	"mosaic/backend/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	paths, err := platform.Paths("Mosaic")
	if err != nil {
		panic(err)
	}
	for _, d := range []string{paths.ConfigDir, paths.DataDir, paths.LogDir} {
		_ = os.MkdirAll(d, 0o755)
	}

	debug := os.Getenv("MOSAIC_DEBUG") == "1"
	closer, err := logging.Init(paths.LogDir, debug)
	if err != nil {
		panic(err)
	}
	defer closer.Close()

	cfg, err := config.Load(filepath.Join(paths.ConfigDir, "mosaic.yaml"))
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	ctx := context.Background()
	db, err := persistence.Open(ctx, filepath.Join(paths.DataDir, "mosaic.db"))
	if err != nil {
		log.Fatal().Err(err).Msg("open db")
	}
	defer db.Close()

	backend, err := engine.NewAnacrolixBackend(engine.AnacrolixConfig{
		DataDir:          filepath.Join(paths.DataDir, "engine"),
		ListenPort:       cfg.ListenPort,
		EnableDHT:        cfg.EnableDHT,
		EnableEncryption: cfg.EnableEncryption,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("open engine backend")
	}
	defer backend.Close()

	eng := engine.NewEngine(backend, 500*time.Millisecond)
	defer eng.Close()

	sched := engine.NewScheduler(eng, 0, 0, 2*time.Second) // 0/0 = unlimited until user sets
	defer sched.Close()

	svc := api.NewService(eng,
		persistence.NewTorrents(db),
		persistence.NewCategories(db),
		persistence.NewTags(db),
		persistence.NewSettings(db),
		sched,
		cfg.DefaultSavePath)
	if err := svc.RestoreOnStartup(ctx); err != nil {
		log.Warn().Err(err).Msg("restore on startup")
	}
	app := NewApp(svc)

	err = wails.Run(&options.App{
		Title:  "Mosaic",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
		},
		OnStartup: app.startup,
		Bind: []any{
			app,
		},
	})
	if err != nil {
		log.Fatal().Err(err).Msg("wails run")
	}
}
