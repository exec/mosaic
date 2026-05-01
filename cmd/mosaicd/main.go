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
		flagDataDir   = flag.String("data-dir", "", "override data directory (default: XDG_DATA_HOME/Mosaic)")
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

	paths, err := platform.Paths("Mosaic")
	if err != nil {
		panic(err)
	}
	if *flagDataDir != "" {
		paths.DataDir = *flagDataDir
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
	// so if the user (or a fresh DB) has it disabled we flip it on. We also
	// honor --port and --bind-all overrides on every boot — they win over the
	// stored config for this run, but are NOT persisted, so they remain
	// per-launch knobs the way other server daemons treat their CLI flags.
	web := svc.GetWebConfig(ctx)
	mutated := false
	if !web.Enabled {
		web.Enabled = true
		mutated = true
		log.Warn().Msg("mosaicd: web interface was disabled — forcing on (without it the daemon has no API)")
	}
	if *flagPort > 0 && web.Port != *flagPort {
		web.Port = *flagPort
		mutated = true
	}
	if *flagBindAll && !web.BindAll {
		web.BindAll = true
		mutated = true
	}
	if mutated {
		if err := svc.SetWebConfig(ctx, web); err != nil {
			log.Fatal().Err(err).Msg("persist forced web config")
		}
		web = svc.GetWebConfig(ctx)
	}

	// First-run password. If no password has ever been set, mint a strong
	// random one, persist its argon2id hash, and write the cleartext to a
	// 0600 credentials file under the data dir AND log it to stdout so the
	// operator can recover it from journalctl. Subsequent boots are no-ops.
	if err := ensureInitialPassword(ctx, svc, paths.DataDir, web); err != nil {
		log.Fatal().Err(err).Msg("ensure initial password")
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
	svc.OnWebConfigChange(remoteSrv.Apply)
	remoteSrv.Apply(svc.GetWebConfig(ctx))

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

// ensureInitialPassword mints a random first-run password if none has been set.
// Idempotent: if VerifyWebCredentials already accepts a stored hash, this is a
// no-op. The cleartext is written to <dataDir>/mosaicd-credentials with mode
// 0600 and printed once to stdout (so journalctl captures it).
func ensureInitialPassword(ctx context.Context, svc *api.Service, dataDir string, web api.WebConfigDTO) error {
	// VerifyWebCredentials reads the hash; if we can never satisfy it with any
	// password (because the hash is empty), we know a password has never been
	// set. We don't check directly — the Service intentionally hides hash
	// reads — so we use the public surface: a cheap probe with a sentinel.
	if svc.VerifyWebCredentials(ctx, web.Username, "\x00probe-not-a-real-password") {
		// Should never match, but if somehow it does, we definitely have a
		// password set — bail out without rotating it.
		return nil
	}
	// Probe a second way: try a different sentinel. If both fail, we either
	// have no password set OR the password just happens not to match either
	// sentinel. To distinguish, check whether SetWebPassword was ever invoked
	// by reading the raw setting through a public side channel: we can't.
	// So fall back to a marker file written next to the credentials file. If
	// the marker exists, we've already initialized. If not, we initialize
	// (and create the marker). This is robust across restarts and avoids
	// rotating a real password the operator has set.
	markerPath := filepath.Join(dataDir, ".mosaicd-credentials-initialized")
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	pwd, err := randomPassword()
	if err != nil {
		return fmt.Errorf("generate password: %w", err)
	}
	if err := svc.SetWebPassword(ctx, pwd); err != nil {
		return fmt.Errorf("persist password: %w", err)
	}

	credsPath := filepath.Join(dataDir, "mosaicd-credentials")
	body := fmt.Sprintf(`Mosaic daemon initial credentials
Generated: %s
Username:  %s
Password:  %s

Visit https://<host>:%d to log in. Change the password from Settings -> Web Interface.
This file is written once on first launch. It is safe to delete after you've recorded the credentials.
`, time.Now().UTC().Format(time.RFC3339), web.Username, pwd, web.Port)
	if err := os.WriteFile(credsPath, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write credentials file %s: %w", credsPath, err)
	}
	if err := os.WriteFile(markerPath, []byte("ok\n"), 0o600); err != nil {
		// Non-fatal: if the marker fails to write, the next boot may rotate
		// the password again. Log and continue.
		log.Warn().Err(err).Str("path", markerPath).Msg("write init marker")
	}

	log.Warn().
		Str("username", web.Username).
		Str("password", pwd).
		Str("credentials_file", credsPath).
		Msg("mosaicd: generated initial web-interface password — record it now and rotate from Settings")

	// Also write to plain stdout so an operator running mosaicd in the
	// foreground (or tailing journalctl with --output=cat) sees it without
	// digging through structured log fields.
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "================ mosaicd: initial credentials ================")
	fmt.Fprintf(os.Stdout, "  Username: %s\n", web.Username)
	fmt.Fprintf(os.Stdout, "  Password: %s\n", pwd)
	fmt.Fprintf(os.Stdout, "  Saved to: %s (0600)\n", credsPath)
	fmt.Fprintln(os.Stdout, "  Change the password from Settings -> Web Interface after first login.")
	fmt.Fprintln(os.Stdout, "==============================================================")
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
