// mosaicd is the headless Linux daemon variant of Mosaic. It runs the same
// engine + persistence + service stack as the desktop GUI, but exposes its
// state exclusively through the HTTPS+WebSocket interface and the embedded
// SolidJS SPA — no Wails shell, no system tray, no desktop notifications.
//
// Conceptually equivalent to qbittorrent-nox: a long-running BitTorrent
// service that you reach from a browser. Designed for servers, NAS boxes, and
// anywhere a GUI process would be a poor fit.
//
// Auto-update is intentionally NOT wired here — system services should be
// upgraded through the system package manager (apt / dnf). The .deb / .rpm
// packages drop a systemd unit that takes care of restarts on upgrade.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"mosaic/backend/api"
	"mosaic/backend/config"
	"mosaic/backend/engine"
	"mosaic/backend/logging"
	"mosaic/backend/persistence"
	"mosaic/backend/platform"
	"mosaic/backend/remote"
)

// version is overridden at build time with `-ldflags "-X main.version=v0.7.0"`.
var version = "dev"

func main() {
	// CLI flags. We keep the surface intentionally small; everything else is
	// a persisted setting, configurable from the web UI's Settings panel.
	var (
		flagConfig    = flag.String("config", "", "path to mosaic.yaml (default: <ConfigDir>/mosaic.yaml)")
		flagDataDir   = flag.String("data-dir", "", "override data + config + log root (default: $STATE_DIRECTORY if set by systemd, else XDG_DATA_HOME/Mosaic)")
		flagAssetsDir = flag.String("assets-dir", "/usr/share/mosaicd/dist", "directory containing the SPA bundle (index.html + assets/)")
		flagPort      = flag.Int("port", 0, "override the persisted web interface port for this run only (0 = use stored config)")
		flagBindAll   = flag.Bool("bind-all", false, "override bind-all for this run only (forces HTTPS on 0.0.0.0)")
		flagVersion   = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Println(version)
		return
	}

	// Validate --port early so a typo (negative, zero-pretending-to-mean-disable,
	// >65535) fails loudly instead of getting silently re-clamped later.
	// 0 is the documented "use stored value, don't override" sentinel.
	if *flagPort < 0 || *flagPort > 65535 {
		fmt.Fprintf(os.Stderr, "mosaicd: --port must be between 0 and 65535 (got %d)\n", *flagPort)
		os.Exit(2)
	}

	paths, err := platform.Paths("Mosaic")
	if err != nil {
		panic(err)
	}

	// systemd's StateDirectory= directive sets STATE_DIRECTORY to an
	// absolute path inside ReadWritePaths and is the documented place for
	// persistent service state. Honor it as the *default* root for data,
	// config, and logs — but only if --data-dir wasn't passed explicitly.
	// This makes a unit file with `StateDirectory=mosaic` Just Work without
	// us having to know about XDG variables.
	dataRoot := *flagDataDir
	if dataRoot == "" {
		if sd := os.Getenv("STATE_DIRECTORY"); sd != "" {
			dataRoot = sd
		}
	}
	if dataRoot != "" {
		// Redirect *everything* under the operator-chosen root. Crucially this
		// includes LogDir, because under systemd's ProtectSystem=strict +
		// ReadWritePaths=<state-dir> the default xdg.StateHome path
		// (`/var/lib/mosaic/.local/state/Mosaic/logs`) is technically inside
		// ReadWritePaths but is non-obvious; collapsing logs into <data>/logs
		// keeps everything we write under one root and matches the systemd
		// unit's ReadWritePaths declaration without surprises.
		paths.DataDir = dataRoot
		paths.ConfigDir = dataRoot
		paths.LogDir = filepath.Join(dataRoot, "logs")
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

	configPath := *flagConfig
	if configPath == "" {
		configPath = filepath.Join(paths.ConfigDir, "mosaic.yaml")
	}
	cfg, err := config.Load(configPath)
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

	// First-run bootstrap. mosaicd is useless without a running web interface,
	// so if the user (or a fresh DB) has it disabled we flip it on for THIS
	// process only — without persisting. The operator's stored preference is
	// preserved verbatim; on next launch (or with persistence flipped off via
	// the UI) we'll force-enable again at runtime, so the daemon never sits
	// with no API surface, and the operator's "off" choice in the DB is never
	// silently overwritten.
	//
	// --port and --bind-all also bypass persistence — same rationale, plus
	// it matches how other server daemons treat their CLI flags (per-launch).
	web := svc.GetWebConfig(ctx)
	if !web.Enabled {
		web.Enabled = true
		log.Warn().Msg("mosaicd: web interface was disabled in stored config — forcing on for this run only (not persisted)")
	}
	if *flagPort > 0 {
		web.Port = *flagPort
	}
	if *flagBindAll {
		web.BindAll = true
	}

	// Ephemeral password (qBittorrent-nox pattern). If the operator has
	// never set a password via the web UI, mint a fresh one on every boot
	// and dump it to stdout so journald captures it. Once the operator
	// logs in and changes the password from Settings → Web Interface, the
	// service flips a "user set" flag and we stop rotating.
	if err := mintEphemeralPasswordIfNeeded(ctx, svc, web); err != nil {
		log.Fatal().Err(err).Msg("mint ephemeral password")
	}

	// Optional HTTPS+WS remote interface. Reads its enabled/port/bind state
	// from settings; restarts whenever SetWebConfig fires the change hook.
	//
	// We serve the SPA from disk (os.DirFS) rather than embedding it in the
	// binary. The .deb / .rpm package ships dist/ at /usr/share/mosaicd/dist
	// which is what --assets-dir defaults to — this is more idiomatic for
	// Linux packaging and lets sysadmins patch the assets without rebuilding.
	staticFS, err := openAssetsDir(*flagAssetsDir)
	if err != nil {
		log.Fatal().Err(err).Str("dir", *flagAssetsDir).Msg("open assets dir")
	}

	hub := remote.NewHub()
	defer hub.Close()
	sessions := remote.NewSessionStore()
	svc.AttachSessionRevoker(sessions)
	remoteSrv := remote.NewServer(svc, hub, sessions, staticFS, paths.DataDir)
	defer remoteSrv.Stop()
	// Runtime web-config changes (operator flips port via Settings) are
	// logged but non-fatal — the daemon stays alive on the previous
	// listener. Bootstrap is different: see below.
	svc.OnWebConfigChange(func(c api.WebConfigDTO) {
		if err := remoteSrv.Apply(c); err != nil {
			log.Error().Err(err).Int("port", c.Port).Msg("mosaicd: web config change failed")
		}
	})
	// Apply our locally-mutated web config (which may have force-enabled or
	// applied --port / --bind-all overrides on top of the persisted state)
	// rather than re-reading from the DB — otherwise our in-memory overrides
	// would be lost the moment the server starts.
	if err := remoteSrv.Apply(web); err != nil {
		// mosaicd is useless without its web interface, so bind failure
		// at bootstrap is fatal. The most common cause in practice is a
		// port collision (qBittorrent-nox / Transmission / another
		// mosaicd already on 8080); spell that out so the operator
		// doesn't have to grep journalctl for clues.
		log.Fatal().
			Err(err).
			Int("port", web.Port).
			Bool("bind_all", web.BindAll).
			Msgf("mosaicd: failed to bind web interface on port %d — likely already in use by another process. "+
				"Stop the conflicting service or relaunch mosaicd with --port <other> "+
				"(or set a new port persistently in /var/lib/mosaic/mosaic.db).", web.Port)
	}

	scheme := "http"
	if web.BindAll {
		scheme = "https"
	}
	host := "127.0.0.1"
	if web.BindAll {
		host = "0.0.0.0"
	}
	log.Info().Str("url", fmt.Sprintf("%s://%s:%d", scheme, host, web.Port)).Str("version", version).Msg("mosaicd: ready")

	// Block until SIGINT / SIGTERM. systemd sends SIGTERM on `systemctl stop`;
	// the deferred Close()s above unwind the stack in reverse declaration order
	// (rss → schedule → remote → hub → sched → eng → backend → db → log) which
	// matches the dependency graph.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	sig := <-stop
	log.Info().Str("signal", sig.String()).Msg("mosaicd: shutting down")
}

// openAssetsDir resolves the configured assets directory and returns an fs.FS
// rooted at it. Returns an error if the directory doesn't exist or doesn't
// contain an index.html, since that almost certainly indicates a packaging or
// CLI mistake and is better caught at startup than after a 404 in the browser.
func openAssetsDir(dir string) (fs.FS, error) {
	if dir == "" {
		return nil, errors.New("assets dir is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
		return nil, fmt.Errorf("missing index.html under %s: %w", abs, err)
	}
	return os.DirFS(abs), nil
}

// mintEphemeralPasswordIfNeeded generates a fresh random password on every
// boot until the operator logs in and changes it from the web UI. Mirrors
// qBittorrent-nox's behavior: a temporary password is regenerated each
// launch and printed to stdout (so journalctl captures it). Once the
// operator calls SetWebPassword from the UI / REST, the service flips
// IsWebPasswordUserSet to true and this function becomes a no-op.
//
// Rationale: avoids stale credential files on disk and the trust-on-first-
// use ambiguity of "is this hash the auto-generated one or the operator's?"
// — we ALWAYS regenerate until proven otherwise by an explicit user action.
func mintEphemeralPasswordIfNeeded(ctx context.Context, svc *api.Service, web api.WebConfigDTO) error {
	if svc.IsWebPasswordUserSet(ctx) {
		return nil
	}

	pwd, err := randomPassword()
	if err != nil {
		return fmt.Errorf("generate password: %w", err)
	}
	if err := svc.SetWebPasswordEphemeral(ctx, pwd); err != nil {
		return fmt.Errorf("persist password: %w", err)
	}

	scheme := "http"
	if web.BindAll {
		scheme = "https"
	}
	host := "<host>"
	if !web.BindAll {
		host = "127.0.0.1"
	}

	// The cleartext password lives ONLY on the stdout banner below — that
	// path goes to journald (when launched by systemd) or to the operator's
	// terminal (when run in the foreground). The structured log entry is
	// written to a rotating file on disk (lumberjack), so we deliberately
	// omit the password here to avoid retaining old credentials across log
	// rotations after the operator changes the password from the UI.
	log.Warn().
		Str("username", web.Username).
		Int("port", web.Port).
		Msg("mosaicd: minted temporary web-interface password — see stdout banner / journalctl for the cleartext (regenerated every restart until you change it from Settings → Web Interface)")

	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "================ mosaicd: temporary web-interface password ================")
	fmt.Fprintf(os.Stdout, "  URL:      %s://%s:%d/\n", scheme, host, web.Port)
	fmt.Fprintf(os.Stdout, "  Username: %s\n", web.Username)
	fmt.Fprintf(os.Stdout, "  Password: %s\n", pwd)
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "  This password is REGENERATED on every restart.")
	fmt.Fprintln(os.Stdout, "  Log in and change it via Settings → Web Interface to make it persist.")
	fmt.Fprintln(os.Stdout, "===========================================================================")
	fmt.Fprintln(os.Stdout, "")
	return nil
}

// randomPassword returns a 32-byte URL-safe random password (~256 bits of
// entropy). We use crypto/rand directly rather than cred.RandomToken so the
// daemon doesn't pull a transitive dependency on test-time random hooks.
func randomPassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
