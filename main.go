package main

import (
	"context"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"mosaic/backend/api"
	"mosaic/backend/config"
	"mosaic/backend/engine"
	"mosaic/backend/logging"
	"mosaic/backend/notifications"
	"mosaic/backend/persistence"
	"mosaic/backend/platform"
	"mosaic/backend/remote"
	"mosaic/backend/tray"
	"mosaic/backend/updater"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is overridden at build time with `-ldflags "-X main.version=v0.7.0"`.
// Defaults to "dev" so `wails dev` runs cleanly.
var version = "dev"

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

	// Persisted connection settings (Settings → Connection → Peers) override
	// the YAML/CLI defaults for the current run. Read directly from settings
	// table since the Service hasn't been constructed yet at this point.
	settingsDAO := persistence.NewSettings(db)
	listenPort := cfg.ListenPort
	enableDHT := cfg.EnableDHT
	enableEnc := cfg.EnableEncryption
	maxPeersPerTorrent := 0
	if v, _ := settingsDAO.Get(ctx, "peer_listen_port"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			listenPort = n
		}
	}
	if v, _ := settingsDAO.Get(ctx, "peers_max_per_torrent"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			maxPeersPerTorrent = n
		}
	}
	if v, _ := settingsDAO.Get(ctx, "dht_enabled"); v == "false" {
		enableDHT = false
	}
	if v, _ := settingsDAO.Get(ctx, "encryption_enabled"); v == "false" {
		enableEnc = false
	}

	backend, err := engine.NewAnacrolixBackend(engine.AnacrolixConfig{
		DataDir:            filepath.Join(paths.DataDir, "engine"),
		ListenPort:         listenPort,
		EnableDHT:          enableDHT,
		EnableEncryption:   enableEnc,
		MaxPeersPerTorrent: maxPeersPerTorrent,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("open engine backend")
	}
	defer backend.Close()

	eng := engine.NewEngine(backend, 500*time.Millisecond)
	defer eng.Close()

	sched := engine.NewScheduler(eng, 0, 0, 2*time.Second) // 0/0 = unlimited until user sets
	defer sched.Close()

	scheduleRules := persistence.NewScheduleRules(db)
	feeds := persistence.NewFeeds(db)
	filters := persistence.NewFilters(db)
	svc := api.NewService(eng,
		persistence.NewTorrents(db),
		persistence.NewCategories(db),
		persistence.NewTags(db),
		settingsDAO,
		scheduleRules,
		feeds,
		filters,
		sched,
		cfg.DefaultSavePath)
	if err := svc.RestoreOnStartup(ctx); err != nil {
		log.Warn().Err(err).Msg("restore on startup")
	}
	scheduleEngine := api.NewScheduleEngine(svc, scheduleRules, time.Local)
	defer scheduleEngine.Close()
	rssPoller := api.NewRSSPoller(svc, feeds, filters)
	defer rssPoller.Close()

	// Optional HTTPS+WS remote interface. Reads its enabled/port/bind state
	// from settings; restarts whenever SetWebConfig fires the change hook.
	staticFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal().Err(err).Msg("embed sub frontend/dist")
	}
	hub := remote.NewHub()
	defer hub.Close()
	sessions := remote.NewSessionStore()
	svc.AttachSessionRevoker(sessions)
	remoteSrv := remote.NewServer(svc, hub, sessions, staticFS, paths.DataDir)
	defer remoteSrv.Stop()
	svc.OnWebConfigChange(remoteSrv.Apply)
	remoteSrv.Apply(svc.GetWebConfig(ctx))

	app := NewApp(svc, hub)

	// Desktop integration: notifications subscriber + system tray.
	//
	// The subscriber consumes engine events and fires beeep notifications on
	// torrent completion / error transitions; the tray exposes show / pause-all
	// / alt-speed / settings / quit affordances on Linux+Windows (no-op stub on
	// macOS, see backend/tray/tray_darwin.go).
	desktopCfg := svc.GetDesktopIntegration(ctx)
	notifSub := notifications.NewSubscriber(notifications.NewBeeepNotifier(), notifications.Settings{
		NotifyOnComplete: desktopCfg.NotifyOnComplete,
		NotifyOnError:    desktopCfg.NotifyOnError,
		NotifyOnUpdate:   desktopCfg.NotifyOnUpdate,
	})
	notifSub.Start(ctx, eng)
	defer notifSub.Stop()
	svc.AttachUpdateInstalledNotifier(notifSub)

	// Tray callbacks dispatch into svc + app. We never block the systray
	// event-loop goroutine: the engine calls below are cheap, but the
	// "Pause all" path iterates every torrent so we run it in a goroutine
	// to be safe.
	trayHandle := tray.New(tray.Callbacks{
		OnShow:           app.ShowWindow,
		OnPauseAll:       func() { go svc.PauseAll(ctx) },
		OnResumeAll:      func() { go svc.ResumeAll(ctx) },
		OnToggleAltSpeed: func() { go func() { _, _ = svc.ToggleAltSpeed(ctx) }() },
		OnOpenSettings:   app.ShowSettings,
		OnQuit:           app.QuitFully,
	})

	// Reflect initial alt-speed state in the tray label.
	if l, err := svc.GetLimits(ctx); err == nil {
		trayHandle.SetAltSpeedActive(l.AltActive)
	}

	if desktopCfg.TrayEnabled {
		trayHandle.Start()
	}
	defer trayHandle.Stop()

	// When the user toggles desktop preferences in Settings, push the new
	// notification toggles into the live subscriber. Tray on/off changes
	// require an app restart (energye/systray's nativeLoop binds to a
	// process-lifetime OS thread; teardown + re-create at runtime is not
	// supported reliably). We log a hint so the frontend can show "restart
	// to apply" copy.
	svc.OnDesktopIntegrationChange(func(c api.DesktopIntegrationDTO) {
		notifSub.SetSettings(notifications.Settings{
			NotifyOnComplete: c.NotifyOnComplete,
			NotifyOnError:    c.NotifyOnError,
			NotifyOnUpdate:   c.NotifyOnUpdate,
		})
	})

	// Auto-update: GitHub-backed updater, fan out new releases to both the
	// Wails desktop session and any connected browser sessions. Schedule only
	// runs the goroutine when the user hasn't disabled checks in Settings.
	upd := updater.New(updater.Config{
		CurrentVersion: version,
		Source: &updater.GitHubSource{
			Owner:   "exec",
			Repo:    "mosaic",
			Channel: svc.UpdaterChannel(ctx),
		},
		OnAvailable: func(info updater.Info) {
			dto := svc.MakeUpdateInfoDTO(info)
			if hub != nil {
				hub.PublishUpdate(dto)
			}
			app.NotifyUpdateAvailable(dto)
		},
	})
	svc.AttachUpdater(upd, version)
	if svc.UpdaterEnabled(ctx) {
		go upd.Schedule(ctx)
	}

	// Close-to-tray on Linux/Windows is gated on desktop.tray_enabled +
	// desktop.close_to_tray. On macOS we set HideWindowOnClose: true below,
	// which routes the X button through Wails's WindowDelegate to
	// [NSApp hide:nil] — hides the whole app like Cmd+H. Dock click then
	// auto-unhides (system behavior), so reopen "just works" without an
	// applicationShouldHandleReopen handler. OnBeforeClose only fires from
	// Cmd+Q / dock right-click → Quit on darwin, both of which we want to
	// honor — return false so Wails proceeds with termination.
	onBeforeClose := func(_ context.Context) (prevent bool) {
		if app.QuittingFully() {
			return false
		}
		if goruntime.GOOS == "darwin" {
			return false
		}
		cfg := svc.GetDesktopIntegration(ctx)
		if !cfg.TrayEnabled || !cfg.CloseToTray {
			return false
		}
		if app.ctx != nil {
			wailsruntime.WindowHide(app.ctx)
		}
		return true
	}

	opts := &options.App{
		Title:  "Mosaic",
		Width:  1200,
		Height: 800,
		// StartHidden honors the desktop.start_minimized preference. The
		// frontend still mounts and connects to the WS / fetches state on
		// load — only the OS window is hidden until the user opens it from
		// the tray.
		StartHidden: desktopCfg.StartMinimized && desktopCfg.TrayEnabled && goruntime.GOOS != "darwin",
		// On macOS, X button hides the app (Cmd+H equivalent) at the AppKit
		// layer instead of triggering OnBeforeClose. Dock-click auto-unhides.
		// Cmd+Q and dock right-click → Quit still terminate cleanly via the
		// default Wails app menu → applicationShouldTerminate → OnBeforeClose
		// (which returns false on darwin per the hook above).
		HideWindowOnClose: goruntime.GOOS == "darwin",
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
			// Wails's AppDelegate implements application:openFile: and routes
			// magnet:// via continueUserActivity / its own AppleEvent handler.
			// Both intercept the AppKit dispatch BEFORE our NSAppleEventManager
			// +load handler can see kAEOpenDocuments / kAEGetURL — that's why
			// our custom bridge never fired in v0.1.13–v0.2.4. Use the proper
			// Wails hooks here. Buffered until OnStartup (Wails enforces this),
			// so app.HandleLaunchArgs is safe to call.
			OnFileOpen: func(path string) { app.HandleLaunchArgs([]string{path}) },
			OnUrlOpen:  func(url string) { app.HandleLaunchArgs([]string{url}) },
		},
		OnBeforeClose: onBeforeClose,
		// SingleInstanceLock: when the user double-clicks a .torrent or follows
		// a magnet: URL while Mosaic is already open, the OS launches a second
		// process; this callback forwards its args to the running instance and
		// raises the window. Without this every magnet click would spawn a new
		// (broken) instance.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "io.github.exec.mosaic",
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				if app.ctx != nil {
					wailsruntime.WindowUnminimise(app.ctx)
					wailsruntime.WindowShow(app.ctx)
				}
				go app.HandleLaunchArgs(d.Args)
			},
		},
		OnStartup: app.startup,
		Bind: []any{
			app,
		},
	}
	// On Windows we hide the OS titlebar entirely and render our own
	// minimize/maximize/close controls in the top-right of the SPA. macOS
	// keeps its hidden-inset titlebar (traffic lights stay).
	if goruntime.GOOS == "windows" {
		opts.Frameless = true
	}
	err = wails.Run(opts)
	if err != nil {
		log.Fatal().Err(err).Msg("wails run")
	}
}
