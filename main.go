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
	// On Windows + Linux, when the file manager launches us with a
	// .torrent path while another Mosaic is already running, we MUST
	// forward args + exit before touching anacrolix's listen port
	// (port-bind would fail and log.Fatal would kill us before reaching
	// the second-instance dispatch). Windows uses Wails's mutex+WM_COPYDATA
	// wire; Linux uses our own Unix socket because Wails's D-Bus single-
	// instance silently fails on common setups (Wayland, sandboxed
	// launchers, missing XDG_RUNTIME_DIR). See
	// backend/platform/single_instance_*.go. No-op on macOS.
	if platform.EarlyForwardLaunchArgs("io.github.exec.mosaic") {
		os.Exit(0)
	}

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

	verifySnaps := persistence.NewVerifySnapshots(db)
	backend, err := engine.NewAnacrolixBackend(engine.AnacrolixConfig{
		DataDir:            filepath.Join(paths.DataDir, "engine"),
		ListenPort:         listenPort,
		EnableDHT:          enableDHT,
		EnableEncryption:   enableEnc,
		MaxPeersPerTorrent: maxPeersPerTorrent,
		SnapshotStore:      &verifySnapshotAdapter{store: verifySnaps},
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
	svc.AttachRSSPoller(rssPoller)

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
	// The remote interface is optional in the GUI — the user has the
	// native window even if the web surface fails to bind. Log loudly
	// (so the user can see it in support output) but don't crash; the
	// SPA's Settings → Web Interface pane will show "enabled" with no
	// listener, which is the existing path for "you tried to enable
	// it but the port was taken."
	svc.OnWebConfigChange(func(c api.WebConfigDTO) {
		if err := remoteSrv.Apply(c); err != nil {
			log.Warn().Err(err).Int("port", c.Port).Msg("remote: web config change failed (port likely in use)")
		}
	})
	if err := remoteSrv.Apply(svc.GetWebConfig(ctx)); err != nil {
		log.Warn().Err(err).Msg("remote: bootstrap failed (port likely in use) — desktop UI is unaffected")
	}

	app := NewApp(svc, hub)

	// Linux second-instance listener. Bound here (after the early-forward
	// check has returned false, so we know we're the first instance) so any
	// file-handler launches that fire while we're booting are queued and
	// dispatched as soon as app.HandleLaunchArgs is safe to call. No-op on
	// Windows + macOS — both rely on Wails's own second-instance plumbing.
	if err := platform.StartSecondInstanceListener("io.github.exec.mosaic", app.HandleLaunchArgs); err != nil {
		log.Warn().Err(err).Msg("single-instance listener failed; .torrent forwards from a running instance won't work")
	}

	// Desktop integration: notifications subscriber + system tray.
	//
	// The subscriber consumes engine events and fires beeep notifications on
	// torrent completion / error transitions; the tray exposes show / pause-all
	// / alt-speed / settings / quit affordances on all three platforms — Linux
	// and Windows via energye/systray; macOS via a native NSStatusItem Cgo
	// bridge in backend/tray/tray_darwin.{m,h,go} (energye fights Wails for
	// the macOS main runloop, so we own the menubar item directly there).
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

	// On Linux (and only Linux), check whether anyone is actually watching
	// for tray icons before starting the tray goroutine. Vanilla Gnome
	// rejects the StatusNotifierItem protocol; users have to install the
	// AppIndicator extension before our tray icon would render anywhere.
	// If the watcher isn't there, starting the tray is a silent no-op
	// from the user's POV — they'd see no icon AND, worse, close-to-tray
	// would orphan the window into a hidden state with no way to bring
	// it back. Disable both for the session and log the fix.
	trayAvailable := tray.Available()
	if goruntime.GOOS == "linux" && !trayAvailable {
		log.Warn().Msg(
			"system tray not available on this desktop session " +
				"(no StatusNotifierWatcher owner). Tray icon hidden, " +
				"close-to-tray disabled. On Gnome, install " +
				"gnome-shell-extension-appindicator (and enable it via " +
				"the Extensions app) to get a tray icon.",
		)
	}
	if desktopCfg.TrayEnabled && trayAvailable {
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
	installSource := updater.DetectInstallSource()
	svc.AttachUpdater(upd, version, installSource)
	// Apt-managed installs defer upgrades to apt — running our own
	// updater on top would either need root to rewrite the dpkg
	// database (gross), or get clobbered on the next `apt upgrade`
	// (also gross). Skip the periodic check; the SPA's Updates pane
	// surfaces the same reasoning so the toggle isn't misleading.
	if installSource == updater.InstallSourceAPT {
		log.Info().Msg("auto-updater disabled: managed by apt — use `sudo apt upgrade mosaic` for updates")
	} else if svc.UpdaterEnabled(ctx) {
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
		// Linux without an active StatusNotifier watcher: the user can't
		// see the tray icon, so hiding the window would strand it. Quit
		// instead.
		if !trayAvailable {
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
		StartHidden: desktopCfg.StartMinimized && desktopCfg.TrayEnabled && trayAvailable && goruntime.GOOS != "darwin",
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
	// On Windows + Linux we hide the OS titlebar entirely and render our
	// own minimize/maximize/close controls in the top-right of the SPA.
	// macOS keeps its hidden-inset titlebar (traffic lights stay).
	if goruntime.GOOS == "windows" || goruntime.GOOS == "linux" {
		opts.Frameless = true
	}
	err = wails.Run(opts)
	if err != nil {
		log.Fatal().Err(err).Msg("wails run")
	}
}
