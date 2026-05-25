// Package bootstrap assembles the shared backend stack used by both the Wails
// desktop app (main.go) and the headless daemon (cmd/mosaicd/main.go).
//
// Previously the two entry points each contained ~200 lines of identical
// engine-init, DAO-construction, service-wiring, RSS/schedule/watchfolder
// setup, and hub creation. Init() centralises that so the entry points only
// contain flavor-specific concerns: Wails window / tray / notifications /
// updater for the desktop, and CLI flags / systemd path handling / ephemeral
// password / signal loop for the daemon.
package bootstrap

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"mosaic/backend/api"
	"mosaic/backend/config"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
	"mosaic/backend/remote"
)

// Config carries everything Init needs. Entry-point-specific concerns (Wails
// options, CLI flags, systemd STATE_DIRECTORY, tray callbacks, etc.) are kept
// in the respective main packages.
type Config struct {
	// DataDir is the root directory for mosaic.db and engine state.
	DataDir string
	// AppVersion is embedded in the BitTorrent handshake string and used for
	// update comparisons. Pass the build-time version var (may be "dev").
	AppVersion string
	// EngineConfig is the merged application config (YAML + env overrides).
	// Engine settings that are not persisted in the DB come from here.
	EngineConfig config.Config
	// AssetsFS is the SPA's static file tree (index.html + assets/).
	// Desktop embeds it via //go:embed; daemon opens it via os.DirFS.
	AssetsFS fs.FS
	// Flavor is remote.FlavorDesktop or remote.FlavorDaemon. Controls CORS,
	// cookie flags, and the /api/bootstrap flavor field.
	Flavor string
}

// Backend is the fully-wired shared backend. Entry points receive one from
// Init and layer their flavor-specific components (tray, notifications,
// updater, CLI flags, signal loop) on top.
type Backend struct {
	DB          *persistence.DB
	Eng         *engine.Engine
	Svc         *api.Service
	Hub         *remote.Hub
	Sessions    *remote.SessionStore
	Server      *remote.Server
	SchedEng    *api.ScheduleEngine
	RSS         *api.RSSPoller
	WatchFolder *api.WatchFolder
}

// Init builds the shared backend and returns a cleanup function that tears
// everything down in reverse construction order. On any error, resources
// allocated so far are cleaned up before returning.
//
// The caller is responsible for invoking the returned cleanup (typically via
// defer) and for the initial Server.Apply() call — that call differs between
// the two flavors (desktop: non-fatal; daemon: fatal), so it stays in the
// entry point.
func Init(ctx context.Context, cfg Config) (*Backend, func(), error) {
	// closers is appended to as resources are allocated. cleanup() runs them
	// in reverse so teardown order mirrors construction order.
	var closers []func()
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}

	// ---- DB ----
	db, err := persistence.Open(ctx, filepath.Join(cfg.DataDir, "mosaic.db"))
	if err != nil {
		return nil, cleanup, fmt.Errorf("open db: %w", err)
	}
	closers = append(closers, func() { db.Close() })

	// ---- Persisted engine settings ----
	// These overlay the YAML/env defaults with whatever the user last saved
	// from the Settings → Connection panel.
	settingsDAO := persistence.NewSettings(db)
	listenPort := cfg.EngineConfig.ListenPort
	enableDHT := cfg.EngineConfig.EnableDHT
	enableEnc := cfg.EngineConfig.EnableEncryption
	enableUPnP := true // default-on; matches anacrolix's NoDefaultPortForwarding=false
	maxPeersPerTorrent := 0
	preallocateFullFiles := false

	if v, _ := settingsDAO.Get(ctx, "peer_listen_port"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			listenPort = n
		}
	}
	if v, _ := settingsDAO.Get(ctx, "peers_max_per_torrent"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxPeersPerTorrent = n
		}
	}
	if v, _ := settingsDAO.Get(ctx, "dht_enabled"); v == "false" {
		enableDHT = false
	}
	if v, _ := settingsDAO.Get(ctx, "encryption_enabled"); v == "false" {
		enableEnc = false
	}
	if v, _ := settingsDAO.Get(ctx, "upnp_enabled"); v == "false" {
		enableUPnP = false
	}
	if v, _ := settingsDAO.Get(ctx, "storage.preallocate_full_files"); v == "true" {
		preallocateFullFiles = true
	}

	// ---- Engine backend ----
	verifySnaps := persistence.NewVerifySnapshots(db)
	engineDir := filepath.Join(cfg.DataDir, "engine")
	anacrolixBackend, err := engine.NewAnacrolixBackend(engine.AnacrolixConfig{
		DataDir:              engineDir,
		ListenPort:           listenPort,
		EnableDHT:            enableDHT,
		EnableEncryption:     enableEnc,
		EnableUPnP:           enableUPnP,
		MaxPeersPerTorrent:   maxPeersPerTorrent,
		SnapshotStore:        &snapshotAdapter{store: verifySnaps},
		ClientVersion:        "Mosaic/" + strings.TrimPrefix(cfg.AppVersion, "v"),
		PreallocateFullFiles: preallocateFullFiles,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("open engine backend: %w", err)
	}
	closers = append(closers, func() { _ = anacrolixBackend.Close() })

	// If the configured port was taken and anacrolix fell back to an OS-picked
	// ephemeral, persist the actual port. Without this the next launch races
	// the same port, loses again, and picks a *different* random — meaning the
	// user's router port-forward never sticks across restarts.
	if actual := anacrolixBackend.ListenPort(); actual > 0 && actual != listenPort {
		log.Info().Int("configured", listenPort).Int("actual", actual).
			Msg("listen port fell back to OS-picked; persisting for next launch")
		if err := settingsDAO.Set(ctx, "peer_listen_port", strconv.Itoa(actual)); err != nil {
			log.Warn().Err(err).Msg("persist fallback listen port")
		}
	}

	eng := engine.NewEngine(anacrolixBackend, 500*time.Millisecond)
	closers = append(closers, func() { _ = eng.Close() })

	sched := engine.NewScheduler(eng, 0, 0, 2*time.Second) // 0/0 = unlimited until user sets
	closers = append(closers, sched.Close)

	// ---- Persistence DAOs ----
	scheduleRules := persistence.NewScheduleRules(db)
	feeds := persistence.NewFeeds(db)
	filters := persistence.NewFilters(db)

	// ---- Service ----
	svc := api.NewService(
		eng,
		persistence.NewTorrents(db),
		persistence.NewCategories(db),
		persistence.NewTags(db),
		settingsDAO,
		scheduleRules,
		feeds,
		filters,
		persistence.NewUsers(db),
		persistence.NewTorrentAccess(db),
		persistence.NewTorrentTrackers(db),
		sched,
		cfg.EngineConfig.DefaultSavePath,
	)
	// Set the build version up-front so AppVersion() / /api/version /
	// the SPA's About pane all see it regardless of whether the updater
	// is wired (the daemon never wires it). Desktop's AttachUpdater
	// later writes the same value into this field; no conflict.
	svc.SetAppVersion(cfg.AppVersion)

	if err := svc.RestoreOnStartup(ctx); err != nil {
		log.Warn().Err(err).Msg("restore on startup")
	}
	// Migrate any pre-0009 plaintext API key into the admin user's hashed key.
	if err := svc.ReconcileLegacyAPIKey(ctx); err != nil {
		log.Warn().Err(err).Msg("reconcile legacy api key")
	}

	// ---- Schedule engine ----
	schedEng := api.NewScheduleEngine(svc, scheduleRules, time.Local)
	closers = append(closers, schedEng.Close)

	// ---- RSS poller ----
	rss := api.NewRSSPoller(svc, feeds, filters)
	closers = append(closers, rss.Close)
	svc.AttachRSSPoller(rss)

	// ---- Watch folder ----
	// Available in both flavors; only started when the user has it configured.
	wf := api.NewWatchFolder(svc)
	closers = append(closers, wf.Stop)
	svc.AttachWatchFolder(wf)
	wfCfg := svc.GetWatchFolder(ctx)
	if wfCfg.Enabled && wfCfg.Path != "" {
		wf.Start(wfCfg.Path, wfCfg.DeleteAfterAdd)
	}

	// ---- Remote interface (Hub + Sessions + Server) ----
	hub := remote.NewHub()
	closers = append(closers, hub.Close)

	sessions := remote.NewSessionStore()
	svc.AttachSessionRevoker(sessions)
	svc.AttachWSRevoker(hub)

	server := remote.NewServer(svc, hub, sessions, cfg.AssetsFS, cfg.DataDir, cfg.Flavor)
	closers = append(closers, server.Stop)

	// Wire the runtime web-config change hook. The initial Apply() is left to
	// the entry point because the two flavors differ: daemon is fatal on bind
	// failure, desktop is not.
	svc.OnWebConfigChange(func(c api.WebConfigDTO) {
		if err := server.Apply(c); err != nil {
			log.Error().Err(err).Int("port", c.Port).
				Msg("web config change: failed to restart listener")
		}
	})

	return &Backend{
		DB:          db,
		Eng:         eng,
		Svc:         svc,
		Hub:         hub,
		Sessions:    sessions,
		Server:      server,
		SchedEng:    schedEng,
		RSS:         rss,
		WatchFolder: wf,
	}, cleanup, nil
}
