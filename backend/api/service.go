package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
	"mosaic/backend/remote/cred"
	"mosaic/backend/updater"
)

// Service is the only place business logic lives. Wails handlers and (later)
// HTTP handlers are thin adapters that translate transport shapes into Service
// calls.
type Service struct {
	engine          *engine.Engine
	torrents        *persistence.Torrents
	categories      *persistence.Categories
	tags            *persistence.Tags
	settings        *persistence.Settings
	scheduleRules   *persistence.ScheduleRules
	feeds           *persistence.Feeds
	filters         *persistence.Filters
	scheduler       *engine.Scheduler
	defaultSavePath string

	focusMu    sync.RWMutex
	focusID    engine.TorrentID
	focusScope engine.DetailScope

	blocklistMu sync.RWMutex
	blocklist   blocklistState

	webHookMu       sync.RWMutex
	onWebCfgChanged func(WebConfigDTO)

	desktopHookMu    sync.RWMutex
	onDesktopChanged func(DesktopIntegrationDTO)

	updater       *updater.Updater // may be nil if not yet attached
	appVersion    string
	installSource updater.InstallSource // "apt" | "appimage" | "manual"
	rssPoller     *RSSPoller            // may be nil during startup; set by AttachRSSPoller

	// updateInstalledNotifier, if set, is invoked after a successful
	// InstallUpdate so the OS-level desktop notification fires. Wired via
	// AttachUpdateInstalledNotifier; nil-safe (InstallUpdate just skips).
	updateInstalledNotifier UpdateInstalledNotifier

	// sessions, if attached, is the remote-server SessionStore. It's wired in
	// post-construction (via AttachSessionRevoker) to avoid an import cycle
	// with backend/remote. SetWebPassword and a username-changing SetWebConfig
	// call RevokeAll() to force every existing browser session to re-auth.
	sessions SessionRevoker
}

// SessionRevoker is the subset of *remote.SessionStore that api.Service needs
// to call after credentials change. Defining it as a small interface here
// keeps the api package free of a remote-package import.
type SessionRevoker interface {
	RevokeAll()
}

// AttachSessionRevoker wires the remote SessionStore into the Service so that
// SetWebPassword / SetWebConfig (when the username changes) can invalidate
// every active session. Pass nil (or never call) if there's no remote layer.
func (s *Service) AttachSessionRevoker(r SessionRevoker) {
	s.sessions = r
}

// blocklistState is the in-memory snapshot of the most recent successful (or
// failed) blocklist load. The list contents themselves live inside the engine's
// IP block proxy.
type blocklistState struct {
	loadedAt time.Time
	entries  int
	lastErr  string
}

func NewService(
	eng *engine.Engine,
	torrents *persistence.Torrents,
	categories *persistence.Categories,
	tags *persistence.Tags,
	settings *persistence.Settings,
	scheduleRules *persistence.ScheduleRules,
	feeds *persistence.Feeds,
	filters *persistence.Filters,
	scheduler *engine.Scheduler,
	defaultSavePath string,
) *Service {
	return &Service{
		engine:          eng,
		torrents:        torrents,
		categories:      categories,
		tags:            tags,
		settings:        settings,
		scheduleRules:   scheduleRules,
		feeds:           feeds,
		filters:         filters,
		scheduler:       scheduler,
		defaultSavePath: defaultSavePath,
	}
}

const (
	settingDefaultSavePath  = "default_save_path"
	settingMaxActiveDL      = "max_active_downloads"
	settingMaxActiveSeeds   = "max_active_seeds"
	settingDownKbps         = "down_kbps"
	settingUpKbps           = "up_kbps"
	settingAltDownKbps      = "alt_down_kbps"
	settingAltUpKbps        = "alt_up_kbps"
	settingAltActive        = "alt_active"
	settingBlocklistURL     = "blocklist_url"
	settingBlocklistEnabled = "blocklist_enabled"

	settingPeerListenPort      = "peer_listen_port"
	settingMaxPeersPerTorrent  = "peers_max_per_torrent"
	settingDHTEnabled          = "dht_enabled"
	settingEncryptionEnabled   = "encryption_enabled"

	settingWebEnabled  = "web_enabled"
	settingWebPort     = "web_port"
	settingWebBindAll  = "web_bind_all"
	settingWebUsername    = "web_username"
	settingWebPassHash    = "web_password_hash"
	settingWebAPIKey      = "web_api_key"
	settingWebPassUserSet = "web_password_user_set"

	settingUpdaterEnabled         = "updater_enabled"
	settingUpdaterChannel         = "updater_channel"
	settingUpdaterLastChecked     = "updater_last_checked_at"
	settingUpdaterLastSeenVersion = "updater_last_seen_version"

	// Desktop integration (system tray + notifications + close-to-tray).
	// Stored as bool strings via setBoolSetting; reads are presence-aware so
	// "" (never set) maps to the spec defaults below — see GetDesktopIntegration.
	settingDesktopTrayEnabled      = "desktop.tray_enabled"
	settingDesktopCloseToTray      = "desktop.close_to_tray"
	settingDesktopStartMinimized   = "desktop.start_minimized"
	settingDesktopNotifyOnComplete = "desktop.notify_on_complete"
	settingDesktopNotifyOnError    = "desktop.notify_on_error"
	settingDesktopNotifyOnUpdate   = "desktop.notify_on_update"
	// Sticky "user clicked Dismiss on the Gnome AppIndicator prompt"
	// flag. Only consulted on Linux/Gnome sessions when the tray
	// watcher is absent; persists across launches so we don't nag.
	settingGnomeAppIndicatorDismissed = "desktop.gnome_appindicator_dismissed"
)

// WebConfigDTO is the transport shape for the optional HTTP+WS interface.
// APIKey is only populated by RotateAPIKey (shown once); GetWebConfig returns
// the stored key so the UI can display it after navigation.
type WebConfigDTO struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"`
	BindAll  bool   `json:"bind_all"`
	Username string `json:"username"`
	APIKey   string `json:"api_key"`
}

func (s *Service) GetWebConfig(ctx context.Context) WebConfigDTO {
	port := s.intSetting(ctx, settingWebPort)
	if port == 0 {
		port = 8080
	}
	user, _ := s.settings.Get(ctx, settingWebUsername)
	if user == "" {
		user = "admin"
	}
	key, _ := s.settings.Get(ctx, settingWebAPIKey)
	return WebConfigDTO{
		Enabled:  s.boolSetting(ctx, settingWebEnabled),
		Port:     port,
		BindAll:  s.boolSetting(ctx, settingWebBindAll),
		Username: user,
		APIKey:   key,
	}
}

func (s *Service) SetWebConfig(ctx context.Context, c WebConfigDTO) error {
	prevUser, _ := s.settings.Get(ctx, settingWebUsername)
	if err := s.setBoolSetting(ctx, settingWebEnabled, c.Enabled); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingWebPort, c.Port); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingWebBindAll, c.BindAll); err != nil {
		return err
	}
	if err := s.settings.Set(ctx, settingWebUsername, c.Username); err != nil {
		return err
	}
	// Force re-login if the username actually changed — otherwise an
	// already-authenticated tab would keep working under the old identity.
	if prevUser != c.Username && s.sessions != nil {
		s.sessions.RevokeAll()
	}
	s.fireWebConfigChanged(s.GetWebConfig(ctx))
	return nil
}

// OnWebConfigChange registers a callback invoked (synchronously) after a
// SetWebConfig call commits. The callback receives the freshly-read DTO so
// the remote.Server can restart with the new bind/port/enabled state.
// Pass nil to unregister.
func (s *Service) OnWebConfigChange(cb func(WebConfigDTO)) {
	s.webHookMu.Lock()
	s.onWebCfgChanged = cb
	s.webHookMu.Unlock()
}

func (s *Service) fireWebConfigChanged(c WebConfigDTO) {
	s.webHookMu.RLock()
	cb := s.onWebCfgChanged
	s.webHookMu.RUnlock()
	if cb != nil {
		cb(c)
	}
}

func (s *Service) SetWebPassword(ctx context.Context, plain string) error {
	// Write the "user set" flag BEFORE the hash so a partial failure cannot
	// silently downgrade the operator back to ephemeral-password mode on the
	// next mosaicd restart. If the flag write fails, we return early — the
	// operator's old password keeps working AND the flag is still false,
	// which is a safe state. If the hash write fails *after* we set the
	// flag, the operator's old password also keeps working (we never wrote
	// the new hash); the flag claims "user set" but the existing hash is
	// either the previous user-set hash or the most recent ephemeral hash
	// the operator already authenticated against — so the daemon won't
	// rotate it on next boot, which matches the operator's intent.
	if err := s.settings.Set(ctx, settingWebPassUserSet, "true"); err != nil {
		return fmt.Errorf("persist web_password_user_set flag: %w", err)
	}
	if err := s.setWebPasswordHash(ctx, plain); err != nil {
		return err
	}
	// Invalidate every active session — the old password is no longer valid,
	// any browser still holding a pre-change cookie must re-authenticate.
	if s.sessions != nil {
		s.sessions.RevokeAll()
	}
	return nil
}

// SetWebPasswordEphemeral persists a password hash without flipping the
// "user set" flag and without touching active sessions. Intended only for
// the mosaicd daemon's first-launch / per-restart auto-generated password
// flow (qBittorrent-nox style). Calling this leaves the daemon in a state
// where the next restart will replace this password again — until the
// operator logs in and calls SetWebPassword from the UI.
func (s *Service) SetWebPasswordEphemeral(ctx context.Context, plain string) error {
	return s.setWebPasswordHash(ctx, plain)
}

// IsWebPasswordUserSet reports whether the operator has explicitly set a
// password via SetWebPassword (UI / REST). Used by mosaicd to decide
// whether to mint a fresh ephemeral password on each boot.
func (s *Service) IsWebPasswordUserSet(ctx context.Context) bool {
	v, _ := s.settings.Get(ctx, settingWebPassUserSet)
	return v == "true"
}

func (s *Service) setWebPasswordHash(ctx context.Context, plain string) error {
	hash, err := cred.HashPassword(plain)
	if err != nil {
		return err
	}
	return s.settings.Set(ctx, settingWebPassHash, hash)
}

func (s *Service) RotateAPIKey(ctx context.Context) (string, error) {
	key, err := cred.RandomToken()
	if err != nil {
		return "", err
	}
	if err := s.settings.Set(ctx, settingWebAPIKey, key); err != nil {
		return "", err
	}
	return key, nil
}

// VerifyWebCredentials returns true if username + password match the stored
// hash; used by the auth middleware on login.
func (s *Service) VerifyWebCredentials(ctx context.Context, username, plain string) bool {
	user, _ := s.settings.Get(ctx, settingWebUsername)
	hash, _ := s.settings.Get(ctx, settingWebPassHash)
	// Match GetWebConfig's default. Fresh installs (especially mosaicd's
	// minted-password path) never populate settingWebUsername explicitly,
	// so the stored value is "" — but the banner / Settings → Web Interface
	// shows "admin" because that's what GetWebConfig reports. Without this
	// fallback, logging in with the displayed username silently fails
	// here ("" != "admin"), surfacing as an unhelpful "invalid credentials".
	if user == "" {
		user = "admin"
	}
	if hash == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 {
		return false
	}
	return cred.VerifyPassword(plain, hash)
}

func (s *Service) VerifyAPIKey(ctx context.Context, key string) bool {
	stored, _ := s.settings.Get(ctx, settingWebAPIKey)
	if stored == "" || key == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(stored)) == 1
}

// UpdaterConfigDTO is the persisted updater preferences shape (channel +
// enabled toggle + cached last-check metadata for UI display).
type UpdaterConfigDTO struct {
	Enabled         bool   `json:"enabled"`
	Channel         string `json:"channel"`           // "stable" | "beta"
	LastCheckedAt   int64  `json:"last_checked_at"`   // unix seconds
	LastSeenVersion string `json:"last_seen_version"`
	// InstallSource classifies how this binary was installed: "apt"
	// (managed by apt — auto-updater is dormant), "appimage", or
	// "manual". The SPA renders Updates pane copy off this field.
	InstallSource string `json:"install_source"`
}

// UpdateInfoDTO mirrors updater.Info plus CurrentVersion so the UI can render
// "X → Y" without needing a separate AppVersion call.
type UpdateInfoDTO struct {
	Available      bool   `json:"available"`
	LatestVersion  string `json:"latest_version"`
	AssetURL       string `json:"asset_url"`
	AssetFilename  string `json:"asset_filename"`
	CheckedAt      int64  `json:"checked_at"` // unix seconds
	CurrentVersion string `json:"current_version"`
}

// AttachUpdater wires the live *updater.Updater + build-time version into the
// Service after construction. main.go calls this once at startup; tests can
// leave it unset to exercise the disabled-updater path.
//
// installSource is the freshly-detected classification of how the running
// binary was installed (apt / appimage / manual). The SPA reads it via
// UpdaterConfigDTO so the Updates pane can render an "apt-managed — use
// `apt upgrade` to update" banner instead of a misleading toggle.
func (s *Service) AttachUpdater(u *updater.Updater, version string, installSource updater.InstallSource) {
	s.updater = u
	s.appVersion = version
	s.installSource = installSource
}

// AttachRSSPoller wires the live *RSSPoller into the Service so the SPA's
// "refresh" button on a feed row can trigger an immediate poll via
// PollFeedNow. The poller and Service live in the same package so this is
// purely a back-reference, not a layering escape hatch. main.go calls
// this once at startup.
func (s *Service) AttachRSSPoller(p *RSSPoller) {
	s.rssPoller = p
}

// PollFeedNow polls a single RSS feed immediately, bypassing its
// scheduled interval. Used by the SPA's per-row refresh icon. Returns
// an error if the poller hasn't been attached or the feed lookup /
// HTTP fetch fails.
func (s *Service) PollFeedNow(ctx context.Context, feedID int) error {
	if s.rssPoller == nil {
		return fmt.Errorf("rss poller not attached")
	}
	return s.rssPoller.PollNow(ctx, feedID)
}

// AppVersion returns the build-time version string the Service was attached
// with. Empty string if AttachUpdater was never called.
func (s *Service) AppVersion() string {
	return s.appVersion
}

// UpdaterEnabled reports whether the auto-update goroutine should run. The
// setting defaults to true (if absent in the settings table). Stored as
// "true" / "false" strings via setBoolSetting.
func (s *Service) UpdaterEnabled(ctx context.Context) bool {
	v, err := s.settings.Get(ctx, settingUpdaterEnabled)
	if err != nil || v == "" {
		return true
	}
	return v == "true"
}

// UpdaterChannel returns the configured update channel ("stable" by default).
func (s *Service) UpdaterChannel(ctx context.Context) string {
	ch, _ := s.settings.Get(ctx, settingUpdaterChannel)
	if ch == "" {
		return "stable"
	}
	return ch
}

func (s *Service) GetUpdaterConfig(ctx context.Context) UpdaterConfigDTO {
	seen, _ := s.settings.Get(ctx, settingUpdaterLastSeenVersion)
	src := string(s.installSource)
	if src == "" {
		src = string(updater.InstallSourceManual)
	}
	return UpdaterConfigDTO{
		Enabled:         s.UpdaterEnabled(ctx),
		Channel:         s.UpdaterChannel(ctx),
		LastCheckedAt:   int64(s.intSetting(ctx, settingUpdaterLastChecked)),
		LastSeenVersion: seen,
		InstallSource:   src,
	}
}

func (s *Service) SetUpdaterConfig(ctx context.Context, c UpdaterConfigDTO) error {
	if c.Channel != "stable" && c.Channel != "beta" {
		return fmt.Errorf("channel must be stable or beta")
	}
	if err := s.setBoolSetting(ctx, settingUpdaterEnabled, c.Enabled); err != nil {
		return err
	}
	return s.settings.Set(ctx, settingUpdaterChannel, c.Channel)
}

func (s *Service) CheckForUpdate(ctx context.Context) (UpdateInfoDTO, error) {
	if s.updater == nil {
		return UpdateInfoDTO{CurrentVersion: s.appVersion}, fmt.Errorf("updater disabled")
	}
	info, err := s.updater.Check(ctx)
	if err != nil {
		return UpdateInfoDTO{CurrentVersion: s.appVersion}, err
	}
	_ = s.setIntSetting(ctx, settingUpdaterLastChecked, int(info.CheckedAt.Unix()))
	if info.Available {
		_ = s.settings.Set(ctx, settingUpdaterLastSeenVersion, info.LatestVersion)
	}
	return UpdateInfoDTO{
		Available:      info.Available,
		LatestVersion:  info.LatestVersion,
		AssetURL:       info.AssetURL,
		AssetFilename:  info.AssetFilename,
		CheckedAt:      info.CheckedAt.Unix(),
		CurrentVersion: s.appVersion,
	}, nil
}

func (s *Service) InstallUpdate(ctx context.Context) error {
	if s.updater == nil {
		return fmt.Errorf("updater disabled")
	}
	if s.installSource == updater.InstallSourceAPT {
		return fmt.Errorf("this Mosaic is managed by apt — run `sudo apt update && sudo apt upgrade mosaic` to update")
	}
	last := s.updater.Last()
	if err := s.updater.Install(ctx, last); err != nil {
		return err
	}
	// Fire the desktop notification (if a Notifier was attached and the user
	// hasn't disabled the notify_on_update toggle — the Notifier itself does
	// the toggle gating). Done here rather than from a hook in the updater
	// package because updater.Updater doesn't expose an OnInstalled callback.
	if s.updateInstalledNotifier != nil {
		s.updateInstalledNotifier.NotifyUpdateInstalled(last.LatestVersion)
	}
	return nil
}

// MakeUpdateInfoDTO converts a raw updater.Info to the API DTO. Used by main.go
// to wrap OnAvailable callback payloads for the WS/Wails event emission.
func (s *Service) MakeUpdateInfoDTO(info updater.Info) UpdateInfoDTO {
	return UpdateInfoDTO{
		Available:      info.Available,
		LatestVersion:  info.LatestVersion,
		AssetURL:       info.AssetURL,
		AssetFilename:  info.AssetFilename,
		CheckedAt:      info.CheckedAt.Unix(),
		CurrentVersion: s.appVersion,
	}
}

// DesktopIntegrationDTO is the transport shape for system-tray + notifications
// + close-to-tray preferences. The frontend keys off these JSON names; do not
// rename without coordinating with the frontend agent owning the settings UI.
type DesktopIntegrationDTO struct {
	TrayEnabled      bool `json:"tray_enabled"`       // default true
	CloseToTray      bool `json:"close_to_tray"`      // default true on Linux/Windows when tray is enabled; ignored on macOS
	StartMinimized   bool `json:"start_minimized"`    // default false — start hidden in tray, no window
	NotifyOnComplete bool `json:"notify_on_complete"` // default true
	NotifyOnError    bool `json:"notify_on_error"`    // default true
	NotifyOnUpdate   bool `json:"notify_on_update"`   // default true
}

// boolSettingDefault reads a bool setting with a presence-aware default —
// "" (never written) returns def, otherwise "true"/"false" parses normally.
// This is the shape we want for DesktopIntegrationDTO defaults: a fresh DB
// must yield TrayEnabled=true even though boolSetting alone would return false.
func (s *Service) boolSettingDefault(ctx context.Context, key string, def bool) bool {
	v, err := s.settings.Get(ctx, key)
	if err != nil || v == "" {
		return def
	}
	return v == "true"
}

func (s *Service) GetDesktopIntegration(ctx context.Context) DesktopIntegrationDTO {
	return DesktopIntegrationDTO{
		TrayEnabled:      s.boolSettingDefault(ctx, settingDesktopTrayEnabled, true),
		// Default true (when tray is enabled): closing the window into a
		// running tray icon matches every mainstream client (qBittorrent,
		// Discord, Slack, Steam). Default-off was surprising — users
		// closed the window expecting standard "minimize to tray" UX and
		// instead lost their session. Users who want close = quit can
		// flip this off in Settings → Desktop Integration.
		CloseToTray:      s.boolSettingDefault(ctx, settingDesktopCloseToTray, true),
		StartMinimized:   s.boolSettingDefault(ctx, settingDesktopStartMinimized, false),
		NotifyOnComplete: s.boolSettingDefault(ctx, settingDesktopNotifyOnComplete, true),
		NotifyOnError:    s.boolSettingDefault(ctx, settingDesktopNotifyOnError, true),
		NotifyOnUpdate:   s.boolSettingDefault(ctx, settingDesktopNotifyOnUpdate, true),
	}
}

// SetDesktopIntegration persists the user's desktop-integration preferences
// and fires the change hook so the running tray/notifications goroutines can
// reconfigure themselves. No validation: the user is allowed to disable
// everything (a perfectly reasonable choice).
func (s *Service) SetDesktopIntegration(ctx context.Context, c DesktopIntegrationDTO) error {
	if err := s.setBoolSetting(ctx, settingDesktopTrayEnabled, c.TrayEnabled); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingDesktopCloseToTray, c.CloseToTray); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingDesktopStartMinimized, c.StartMinimized); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingDesktopNotifyOnComplete, c.NotifyOnComplete); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingDesktopNotifyOnError, c.NotifyOnError); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingDesktopNotifyOnUpdate, c.NotifyOnUpdate); err != nil {
		return err
	}
	s.fireDesktopIntegrationChanged(c)
	return nil
}

// IsGnomeAppIndicatorPromptDismissed reports whether the user has
// previously clicked Dismiss on the in-app Gnome AppIndicator prompt.
// Once true, the SPA stops showing the prompt even if the tray watcher
// is still absent — manual re-trigger via Settings would be the path
// back to nagging if we ever surface that.
func (s *Service) IsGnomeAppIndicatorPromptDismissed(ctx context.Context) bool {
	v, _ := s.settings.Get(ctx, settingGnomeAppIndicatorDismissed)
	return v == "true"
}

// DismissGnomeAppIndicatorPrompt persists the "user said no" flag.
func (s *Service) DismissGnomeAppIndicatorPrompt(ctx context.Context) error {
	return s.settings.Set(ctx, settingGnomeAppIndicatorDismissed, "true")
}

// OnDesktopIntegrationChange registers a synchronous callback invoked after a
// SetDesktopIntegration commit. main.go uses this to push the updated
// notification toggles into the live notifications.Subscriber. Pass nil to
// unregister. Only one callback is supported.
func (s *Service) OnDesktopIntegrationChange(cb func(DesktopIntegrationDTO)) {
	s.desktopHookMu.Lock()
	s.onDesktopChanged = cb
	s.desktopHookMu.Unlock()
}

func (s *Service) fireDesktopIntegrationChanged(c DesktopIntegrationDTO) {
	s.desktopHookMu.RLock()
	cb := s.onDesktopChanged
	s.desktopHookMu.RUnlock()
	if cb != nil {
		cb(c)
	}
}

func (s *Service) GetDefaultSavePath(ctx context.Context) (string, error) {
	v, err := s.settings.Get(ctx, settingDefaultSavePath)
	if errors.Is(err, persistence.ErrNotFound) {
		return s.defaultSavePath, nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (s *Service) SetDefaultSavePath(ctx context.Context, path string) error {
	return s.settings.Set(ctx, settingDefaultSavePath, path)
}

func (s *Service) defaultPath(ctx context.Context) string {
	if v, err := s.GetDefaultSavePath(ctx); err == nil {
		return v
	}
	return s.defaultSavePath
}

// TorrentDTO is the shape returned to UI/transport callers.
type TorrentDTO struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Magnet        string   `json:"magnet"`
	SavePath      string   `json:"save_path"`
	TotalBytes    int64    `json:"total_bytes"`
	BytesDone     int64    `json:"bytes_done"`
	Progress      float64  `json:"progress"` // 0..1
	DownloadRate  int64    `json:"download_rate"`
	UploadRate    int64    `json:"upload_rate"`
	Peers         int      `json:"peers"`
	Seeds         int      `json:"seeds"`
	Paused        bool     `json:"paused"`
	Completed     bool     `json:"completed"`
	AddedAt       int64    `json:"added_at"` // unix seconds
	CategoryID    *int     `json:"category_id"`
	Tags          []TagDTO `json:"tags"`
	QueuePosition int      `json:"queue_position"`
	ForceStart    bool     `json:"force_start"`
	Queued        bool     `json:"queued"`
	Verifying     bool     `json:"verifying"`
	FilesMissing  bool     `json:"files_missing"`
}

func toDTO(s engine.Snapshot, addedAt time.Time) TorrentDTO {
	prog := 0.0
	if s.TotalBytes > 0 {
		prog = float64(s.BytesDone) / float64(s.TotalBytes)
	}
	return TorrentDTO{
		ID:            string(s.ID),
		Name:          s.Name,
		Magnet:        s.Magnet,
		SavePath:      s.SavePath,
		TotalBytes:    s.TotalBytes,
		BytesDone:     s.BytesDone,
		Progress:      prog,
		DownloadRate:  s.RateDown,
		UploadRate:    s.RateUp,
		Peers:         s.Peers,
		Seeds:         s.Seeds,
		Paused:        s.Paused,
		Completed:     s.Completed,
		AddedAt:       addedAt.Unix(),
		QueuePosition: s.QueuePosition,
		ForceStart:    s.ForceStart,
		Queued:        s.Queued,
		Verifying:     s.Verifying,
		FilesMissing:  s.FilesMissing,
	}
}

func (s *Service) AddMagnet(ctx context.Context, magnet, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultPath(ctx)
	}
	id, err := s.engine.AddMagnet(ctx, magnet, savePath)
	if err != nil {
		return "", fmt.Errorf("add magnet: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		Magnet:   magnet,
		SavePath: savePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return id, nil
}

func (s *Service) AddTorrentFile(ctx context.Context, filePath, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultPath(ctx)
	}
	blob, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read torrent file: %w", err)
	}
	id, err := s.engine.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", fmt.Errorf("add torrent: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
		Metainfo: blob,
	}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return id, nil
}

func (s *Service) AddTorrentBytes(ctx context.Context, blob []byte, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultPath(ctx)
	}
	id, err := s.engine.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", fmt.Errorf("add torrent bytes: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
		Metainfo: blob,
	}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return id, nil
}

func (s *Service) Pause(id engine.TorrentID) error   { return s.engine.Pause(id) }
func (s *Service) Resume(id engine.TorrentID) error  { return s.engine.Resume(id) }
func (s *Service) Recheck(id engine.TorrentID) error { return s.engine.Recheck(id) }

// PauseAll pauses every torrent currently known to the engine. Errors on
// individual torrents are logged but don't abort the loop — best-effort
// semantics so a single missing/dead torrent can't strand the rest.
// Used by the system-tray "Pause all" item.
func (s *Service) PauseAll(_ context.Context) {
	for _, snap := range s.engine.List() {
		if snap.Paused {
			continue
		}
		if err := s.engine.Pause(snap.ID); err != nil {
			log.Warn().Err(err).Str("id", string(snap.ID)).Msg("PauseAll: pause failed")
		}
	}
}

// ResumeAll is the mirror of PauseAll. Used by the system-tray "Resume all"
// item when the engine is in the globally-paused state.
func (s *Service) ResumeAll(_ context.Context) {
	for _, snap := range s.engine.List() {
		if !snap.Paused {
			continue
		}
		if err := s.engine.Resume(snap.ID); err != nil {
			log.Warn().Err(err).Str("id", string(snap.ID)).Msg("ResumeAll: resume failed")
		}
	}
}

// UpdateInstalledNotifier is the post-install hook the desktop-integration
// notifications package implements. Defining the contract as an interface
// here keeps the api package free of a notifications-package import.
type UpdateInstalledNotifier interface {
	NotifyUpdateInstalled(version string)
}

// AttachUpdateInstalledNotifier wires the notifications subscriber into the
// Service so InstallUpdate can fire the OS-level notification on success.
// Pass nil (or never call) to disable the post-install notification.
func (s *Service) AttachUpdateInstalledNotifier(n UpdateInstalledNotifier) {
	s.updateInstalledNotifier = n
}

func (s *Service) Remove(ctx context.Context, id engine.TorrentID, deleteFiles bool) error {
	if err := s.engine.Remove(id, deleteFiles); err != nil {
		return err
	}
	return s.torrents.Remove(ctx, string(id))
}

func (s *Service) ListTorrents(ctx context.Context) ([]TorrentDTO, error) {
	records, err := s.torrents.List(ctx)
	if err != nil {
		return nil, err
	}
	byHash := make(map[string]persistence.TorrentRecord, len(records))
	for _, r := range records {
		byHash[r.InfoHash] = r
	}
	// Pre-v0.4.3 we called s.tags.ForTorrent(ctx, infohash) inside the
	// loop below — N+1 SELECTs every tick (~500ms cadence). One bulk
	// fetch instead, hashed by infohash so the loop is an O(1) lookup.
	tagsByHash, err := s.tags.ForAllTorrents(ctx)
	if err != nil {
		return nil, err
	}
	snaps := s.engine.List()
	out := make([]TorrentDTO, 0, len(snaps))
	for _, snap := range snaps {
		rec, ok := byHash[string(snap.ID)]
		addedAt := time.Now()
		if ok {
			snap.SavePath = rec.SavePath
			if snap.Magnet == "" {
				snap.Magnet = rec.Magnet
			}
			addedAt = rec.AddedAt
		}
		dto := toDTO(snap, addedAt)
		if ok {
			dto.CategoryID = rec.CategoryID
		}
		tags := tagsByHash[string(snap.ID)]
		dto.Tags = make([]TagDTO, 0, len(tags))
		for _, tg := range tags {
			dto.Tags = append(dto.Tags, TagDTO{ID: tg.ID, Name: tg.Name, Color: tg.Color})
		}
		out = append(out, dto)
	}
	// Stable order — engine.List() iterates anacrolix's internal map (random
	// per call), which would swap rows on every tick. Sort by added_at desc
	// (newest first), tie-broken by id so identical-second adds don't flip.
	sort.Slice(out, func(i, j int) bool {
		if out[i].AddedAt != out[j].AddedAt {
			return out[i].AddedAt > out[j].AddedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// GlobalStats is the snapshot displayed in the status bar.
type GlobalStats struct {
	TotalTorrents      int   `json:"total_torrents"`
	ActiveTorrents     int   `json:"active_torrents"`
	SeedingTorrents    int   `json:"seeding_torrents"`
	TotalDownloadRate  int64 `json:"total_download_rate"`
	TotalUploadRate    int64 `json:"total_upload_rate"`
	TotalPeers         int   `json:"total_peers"`
}

func (s *Service) GlobalStats(ctx context.Context) (GlobalStats, error) {
	snaps := s.engine.List()
	var st GlobalStats
	st.TotalTorrents = len(snaps)
	for _, snap := range snaps {
		if !snap.Paused && !snap.Completed {
			st.ActiveTorrents++
		}
		if snap.Completed {
			st.SeedingTorrents++
		}
		st.TotalDownloadRate += snap.RateDown
		st.TotalUploadRate += snap.RateUp
		st.TotalPeers += snap.Peers
	}
	return st, nil
}

// DetailDTO is the inspector tick payload, returned from DetailForFocus or
// emitted via the inspector:tick event.
type DetailDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Overview-tab fields — always present
	Magnet      string  `json:"magnet"`
	SavePath    string  `json:"save_path"`
	TotalBytes  int64   `json:"total_bytes"`
	BytesDone   int64   `json:"bytes_done"`
	Progress    float64 `json:"progress"`
	Ratio       float64 `json:"ratio"`
	TotalDown   int64   `json:"total_down"`
	TotalUp     int64   `json:"total_up"`
	Peers       int     `json:"peers"`
	Seeds       int     `json:"seeds"`
	AddedAt     int64   `json:"added_at"`
	CompletedAt int64   `json:"completed_at,omitempty"`

	Files     []FileDTO    `json:"files,omitempty"`
	PeersList []PeerDTO    `json:"peers_list,omitempty"`
	Trackers  []TrackerDTO `json:"trackers,omitempty"`
}

type FileDTO struct {
	Index     int     `json:"index"`
	Path      string  `json:"path"`
	Size      int64   `json:"size"`
	BytesDone int64   `json:"bytes_done"`
	Progress  float64 `json:"progress"`
	Priority  string  `json:"priority"` // "skip" | "normal" | "high" | "max"
}

type PeerDTO struct {
	IP           string  `json:"ip"`
	Port         int     `json:"port"`
	Client       string  `json:"client"`
	Flags        string  `json:"flags"`
	Progress     float64 `json:"progress"`
	DownloadRate int64   `json:"download_rate"`
	UploadRate   int64   `json:"upload_rate"`
	Country      string  `json:"country"`
}

type TrackerDTO struct {
	URL          string `json:"url"`
	Status       string `json:"status"`
	Seeds        int    `json:"seeds"`
	Peers        int    `json:"peers"`
	Downloaded   int    `json:"downloaded"`
	LastAnnounce int64  `json:"last_announce"`
	NextAnnounce int64  `json:"next_announce"`
}

// SetInspectorFocus tells the service which torrent + tabs the UI is looking
// at. Subsequent DetailForFocus calls (and the inspector:tick event in app.go)
// will return the appropriately-scoped Detail. tabs is a subset of:
// "overview", "files", "peers", "trackers", "speed".
func (s *Service) SetInspectorFocus(id string, tabs []string) error {
	if id == "" {
		s.ClearInspectorFocus()
		return nil
	}
	scope := scopeForTabs(tabs)
	s.focusMu.Lock()
	s.focusID = engine.TorrentID(id)
	s.focusScope = scope
	s.focusMu.Unlock()
	return nil
}

func (s *Service) ClearInspectorFocus() {
	s.focusMu.Lock()
	s.focusID = ""
	s.focusScope = engine.DetailScope{}
	s.focusMu.Unlock()
}

// DetailForFocus returns the current focused torrent's detail, or nil if no
// inspector focus is set.
func (s *Service) DetailForFocus(ctx context.Context) (*DetailDTO, error) {
	s.focusMu.RLock()
	id := s.focusID
	scope := s.focusScope
	s.focusMu.RUnlock()
	if id == "" {
		return nil, nil
	}
	d, err := s.engine.DetailedSnapshot(id, scope)
	if err != nil {
		return nil, err
	}
	dto := detailToDTO(d, s.lookupAddedAt(ctx, id))
	return &dto, nil
}

func scopeForTabs(tabs []string) engine.DetailScope {
	scope := engine.DetailScope{}
	for _, t := range tabs {
		switch t {
		case "files":
			scope.Files = true
		case "peers":
			scope.Peers = true
		case "trackers":
			scope.Trackers = true
		}
	}
	return scope
}

func (s *Service) lookupAddedAt(ctx context.Context, id engine.TorrentID) time.Time {
	rec, err := s.torrents.Get(ctx, string(id))
	if err != nil {
		return time.Time{}
	}
	return rec.AddedAt
}

func detailToDTO(d engine.Detail, addedAt time.Time) DetailDTO {
	snap := d.Snapshot
	prog := 0.0
	if snap.TotalBytes > 0 {
		prog = float64(snap.BytesDone) / float64(snap.TotalBytes)
	}
	dto := DetailDTO{
		ID:         string(snap.ID),
		Name:       snap.Name,
		Magnet:     snap.Magnet,
		SavePath:   snap.SavePath,
		TotalBytes: snap.TotalBytes,
		BytesDone:  snap.BytesDone,
		Progress:   prog,
		Ratio:      ratioOf(snap.BytesDown, snap.BytesUp),
		TotalDown:  snap.BytesDown,
		TotalUp:    snap.BytesUp,
		Peers:      snap.Peers,
		Seeds:      snap.Seeds,
		AddedAt:    addedAt.Unix(),
	}
	for _, f := range d.Files {
		fp := 0.0
		if f.Size > 0 {
			fp = float64(f.BytesDone) / float64(f.Size)
		}
		dto.Files = append(dto.Files, FileDTO{
			Index: f.Index, Path: f.Path, Size: f.Size, BytesDone: f.BytesDone, Progress: fp,
			Priority: priorityToString(f.Priority),
		})
	}
	for _, p := range d.Peers {
		dto.PeersList = append(dto.PeersList, PeerDTO{
			IP: p.IP, Port: p.Port, Client: p.ClientName, Flags: p.Flags,
			Progress: p.Progress, DownloadRate: p.DownloadRate, UploadRate: p.UploadRate, Country: p.CountryCode,
		})
	}
	for _, t := range d.Trackers {
		dto.Trackers = append(dto.Trackers, TrackerDTO{
			URL: t.URL, Status: t.Status, Seeds: t.Seeds, Peers: t.Peers, Downloaded: t.Downloaded,
			LastAnnounce: t.LastAnnounce.Unix(), NextAnnounce: t.NextAnnounce.Unix(),
		})
	}
	return dto
}

func ratioOf(down, up int64) float64 {
	if down == 0 {
		return 0
	}
	return float64(up) / float64(down)
}

func priorityToString(p engine.Priority) string {
	switch p {
	case engine.PrioritySkip:
		return "skip"
	case engine.PriorityHigh:
		return "high"
	case engine.PriorityMax:
		return "max"
	}
	return "normal"
}

type CategoryDTO struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	DefaultSavePath string `json:"default_save_path"`
	Color           string `json:"color"`
}

type TagDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Service) CreateCategory(ctx context.Context, name, defaultPath, color string) (int, error) {
	return s.categories.Create(ctx, persistence.Category{Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) ListCategories(ctx context.Context) ([]CategoryDTO, error) {
	cats, err := s.categories.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryDTO, 0, len(cats))
	for _, c := range cats {
		out = append(out, CategoryDTO{ID: c.ID, Name: c.Name, DefaultSavePath: c.DefaultSavePath, Color: c.Color})
	}
	return out, nil
}

func (s *Service) UpdateCategory(ctx context.Context, id int, name, defaultPath, color string) error {
	return s.categories.Update(ctx, persistence.Category{ID: id, Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) DeleteCategory(ctx context.Context, id int) error {
	return s.categories.Delete(ctx, id)
}

func (s *Service) CreateTag(ctx context.Context, name, color string) (int, error) {
	return s.tags.Create(ctx, persistence.Tag{Name: name, Color: color})
}

func (s *Service) ListTags(ctx context.Context) ([]TagDTO, error) {
	tags, err := s.tags.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out, nil
}

func (s *Service) DeleteTag(ctx context.Context, id int) error {
	return s.tags.Delete(ctx, id)
}

func (s *Service) AssignTag(ctx context.Context, infohash string, tagID int) error {
	return s.tags.Assign(ctx, infohash, tagID)
}

func (s *Service) UnassignTag(ctx context.Context, infohash string, tagID int) error {
	return s.tags.Unassign(ctx, infohash, tagID)
}

func (s *Service) ListTagsFor(ctx context.Context, infohash string) ([]TagDTO, error) {
	tags, err := s.tags.ForTorrent(ctx, infohash)
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out, nil
}

func (s *Service) SetTorrentCategory(ctx context.Context, infohash string, categoryID *int) error {
	return s.torrents.SetCategory(ctx, infohash, categoryID)
}

func (s *Service) SetFilePriorities(ctx context.Context, infohash string, prios map[int]string) error {
	mapped := make(map[int]engine.Priority, len(prios))
	for idx, p := range prios {
		switch p {
		case "skip":
			mapped[idx] = engine.PrioritySkip
		case "high":
			mapped[idx] = engine.PriorityHigh
		case "max":
			mapped[idx] = engine.PriorityMax
		default:
			mapped[idx] = engine.PriorityNormal
		}
	}
	return s.engine.SetFilePriorities(engine.TorrentID(infohash), mapped)
}

// LimitsDTO is the bandwidth-limits transport shape (kbps units).
type LimitsDTO struct {
	DownKbps    int  `json:"down_kbps"`
	UpKbps      int  `json:"up_kbps"`
	AltDownKbps int  `json:"alt_down_kbps"`
	AltUpKbps   int  `json:"alt_up_kbps"`
	AltActive   bool `json:"alt_active"`
}

// QueueLimitsDTO is the queue-slot transport shape.
type QueueLimitsDTO struct {
	MaxActiveDownloads int `json:"max_active_downloads"`
	MaxActiveSeeds     int `json:"max_active_seeds"`
}

func (s *Service) GetLimits(ctx context.Context) (LimitsDTO, error) {
	return LimitsDTO{
		DownKbps:    s.intSetting(ctx, settingDownKbps),
		UpKbps:      s.intSetting(ctx, settingUpKbps),
		AltDownKbps: s.intSetting(ctx, settingAltDownKbps),
		AltUpKbps:   s.intSetting(ctx, settingAltUpKbps),
		AltActive:   s.boolSetting(ctx, settingAltActive),
	}, nil
}

func (s *Service) SetLimits(ctx context.Context, l LimitsDTO) error {
	if err := s.setIntSetting(ctx, settingDownKbps, l.DownKbps); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingUpKbps, l.UpKbps); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingAltDownKbps, l.AltDownKbps); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingAltUpKbps, l.AltUpKbps); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingAltActive, l.AltActive); err != nil {
		return err
	}
	return s.applyLimits(ctx)
}

// ToggleAltSpeed flips the alt-speed flag and reapplies engine limits. Returns
// the new alt_active state.
func (s *Service) ToggleAltSpeed(ctx context.Context) (bool, error) {
	cur := s.boolSetting(ctx, settingAltActive)
	next := !cur
	if err := s.setBoolSetting(ctx, settingAltActive, next); err != nil {
		return cur, err
	}
	return next, s.applyLimits(ctx)
}

func (s *Service) applyLimits(ctx context.Context) error {
	l, _ := s.GetLimits(ctx)
	down, up := l.DownKbps*1024, l.UpKbps*1024
	if l.AltActive {
		down, up = l.AltDownKbps*1024, l.AltUpKbps*1024
	}
	return s.engine.SetGlobalRateLimits(down, up)
}

// PeerLimitsDTO carries the connection-level settings users can adjust from
// Settings → Connection. Most fields require a restart to take effect because
// anacrolix exposes them only at Client construction; MaxPeersPerTorrent is
// runtime-mutable and gets pushed to every running torrent on Set.
type PeerLimitsDTO struct {
	ListenPort         int  `json:"listen_port"`         // 0 = let OS pick at startup
	MaxPeersPerTorrent int  `json:"max_peers_per_torrent"` // 0 = anacrolix default (80)
	DHTEnabled         bool `json:"dht_enabled"`
	EncryptionEnabled  bool `json:"encryption_enabled"`
}

func (s *Service) GetPeerLimits(ctx context.Context) PeerLimitsDTO {
	// DHT + encryption default to true (matches anacrolix's defaults + good
	// privacy hygiene). The bool helpers in this Service treat unset as
	// false, so use a presence-aware reader.
	dhtRaw, _ := s.settings.Get(ctx, settingDHTEnabled)
	dhtEnabled := dhtRaw == "" || dhtRaw == "true" // default-on
	encRaw, _ := s.settings.Get(ctx, settingEncryptionEnabled)
	encEnabled := encRaw == "" || encRaw == "true" // default-on
	return PeerLimitsDTO{
		ListenPort:         s.intSetting(ctx, settingPeerListenPort),
		MaxPeersPerTorrent: s.intSetting(ctx, settingMaxPeersPerTorrent),
		DHTEnabled:         dhtEnabled,
		EncryptionEnabled:  encEnabled,
	}
}

func (s *Service) SetPeerLimits(ctx context.Context, p PeerLimitsDTO) error {
	if p.ListenPort < 0 || p.ListenPort > 65535 {
		return fmt.Errorf("listen port must be 0..65535")
	}
	if p.MaxPeersPerTorrent < 0 {
		return fmt.Errorf("max peers per torrent must be >= 0")
	}
	if err := s.setIntSetting(ctx, settingPeerListenPort, p.ListenPort); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingMaxPeersPerTorrent, p.MaxPeersPerTorrent); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingDHTEnabled, p.DHTEnabled); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingEncryptionEnabled, p.EncryptionEnabled); err != nil {
		return err
	}
	// Per-torrent cap is the only one anacrolix lets us mutate at runtime.
	// ListenPort / DHT / Encryption changes take effect at next launch.
	if err := s.engine.ApplyPerTorrentMaxPeers(p.MaxPeersPerTorrent); err != nil {
		return err
	}
	return nil
}

func (s *Service) GetQueueLimits(ctx context.Context) QueueLimitsDTO {
	return QueueLimitsDTO{
		MaxActiveDownloads: s.intSetting(ctx, settingMaxActiveDL),
		MaxActiveSeeds:     s.intSetting(ctx, settingMaxActiveSeeds),
	}
}

func (s *Service) SetQueueLimits(ctx context.Context, q QueueLimitsDTO) error {
	if err := s.setIntSetting(ctx, settingMaxActiveDL, q.MaxActiveDownloads); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingMaxActiveSeeds, q.MaxActiveSeeds); err != nil {
		return err
	}
	if s.scheduler != nil {
		s.scheduler.SetLimits(q.MaxActiveDownloads, q.MaxActiveSeeds)
	}
	return nil
}

func (s *Service) SetQueuePosition(ctx context.Context, infohash string, pos int) error {
	if err := s.torrents.SetQueuePosition(ctx, infohash, pos); err != nil {
		return err
	}
	s.engine.SetQueuePosition(engine.TorrentID(infohash), pos)
	return nil
}

func (s *Service) SetForceStart(ctx context.Context, infohash string, force bool) error {
	if err := s.torrents.SetForceStart(ctx, infohash, force); err != nil {
		return err
	}
	s.engine.SetForceStart(engine.TorrentID(infohash), force)
	return nil
}

// ScheduleRuleDTO is the transport shape for a time-of-day bandwidth rule.
type ScheduleRuleDTO struct {
	ID       int  `json:"id"`
	DaysMask int  `json:"days_mask"`
	StartMin int  `json:"start_min"`
	EndMin   int  `json:"end_min"`
	DownKbps int  `json:"down_kbps"`
	UpKbps   int  `json:"up_kbps"`
	AltOnly  bool `json:"alt_only"`
	Enabled  bool `json:"enabled"`
}

func toScheduleRuleDTO(r persistence.ScheduleRule) ScheduleRuleDTO {
	return ScheduleRuleDTO{
		ID: r.ID, DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	}
}

func (s *Service) ListScheduleRules(ctx context.Context) ([]ScheduleRuleDTO, error) {
	if s.scheduleRules == nil {
		return nil, nil
	}
	rules, err := s.scheduleRules.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduleRuleDTO, 0, len(rules))
	for _, r := range rules {
		out = append(out, toScheduleRuleDTO(r))
	}
	return out, nil
}

func (s *Service) CreateScheduleRule(ctx context.Context, r ScheduleRuleDTO) (int, error) {
	return s.scheduleRules.Create(ctx, persistence.ScheduleRule{
		DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	})
}

func (s *Service) UpdateScheduleRule(ctx context.Context, r ScheduleRuleDTO) error {
	return s.scheduleRules.Update(ctx, persistence.ScheduleRule{
		ID: r.ID, DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	})
}

func (s *Service) DeleteScheduleRule(ctx context.Context, id int) error {
	return s.scheduleRules.Delete(ctx, id)
}

// BlocklistDTO is the transport shape for the IP blocklist config + status.
type BlocklistDTO struct {
	URL          string `json:"url"`
	Enabled      bool   `json:"enabled"`
	LastLoadedAt int64  `json:"last_loaded_at"`
	Entries      int    `json:"entries"`
	Error        string `json:"error,omitempty"`
}

func (s *Service) GetBlocklist(ctx context.Context) BlocklistDTO {
	url, _ := s.settings.Get(ctx, settingBlocklistURL)
	en := s.boolSetting(ctx, settingBlocklistEnabled)
	s.blocklistMu.RLock()
	defer s.blocklistMu.RUnlock()
	dto := BlocklistDTO{URL: url, Enabled: en, Entries: s.blocklist.entries, Error: s.blocklist.lastErr}
	if !s.blocklist.loadedAt.IsZero() {
		dto.LastLoadedAt = s.blocklist.loadedAt.Unix()
	}
	return dto
}

func (s *Service) SetBlocklistURL(ctx context.Context, url string, enabled bool) error {
	// Reject obviously dangerous URLs at write time. The dialer in
	// safeHTTPClient is the second layer that catches DNS-rebind tricks.
	if enabled && url != "" {
		if _, err := validateFetchURL(url); err != nil {
			return fmt.Errorf("blocklist URL must be http or https and not point at a private/loopback address: %w", err)
		}
	}
	if err := s.settings.Set(ctx, settingBlocklistURL, url); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingBlocklistEnabled, enabled); err != nil {
		return err
	}
	if !enabled || url == "" {
		_ = s.engine.SetIPBlocklist(nil)
		s.blocklistMu.Lock()
		s.blocklist = blocklistState{}
		s.blocklistMu.Unlock()
		return nil
	}
	return s.RefreshBlocklist(ctx)
}

func (s *Service) RefreshBlocklist(ctx context.Context) error {
	url, _ := s.settings.Get(ctx, settingBlocklistURL)
	if url == "" {
		return errors.New("no blocklist URL configured")
	}
	if _, err := validateFetchURL(url); err != nil {
		return fmt.Errorf("refusing to fetch blocklist: %w", err)
	}

	httpCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(httpCtx, "GET", url, nil)
	if err != nil {
		return err
	}
	client := safeHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		s.blocklistMu.Lock()
		s.blocklist.lastErr = err.Error()
		s.blocklistMu.Unlock()
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 50MB safety cap
	if err != nil {
		s.blocklistMu.Lock()
		s.blocklist.lastErr = err.Error()
		s.blocklistMu.Unlock()
		return err
	}

	if err := s.engine.SetIPBlocklist(bytes.NewReader(body)); err != nil {
		s.blocklistMu.Lock()
		s.blocklist.lastErr = err.Error()
		s.blocklistMu.Unlock()
		return err
	}

	s.blocklistMu.Lock()
	s.blocklist = blocklistState{loadedAt: time.Now(), entries: countLines(body), lastErr: ""}
	s.blocklistMu.Unlock()
	return nil
}

func countLines(b []byte) int {
	n := 0
	for _, x := range b {
		if x == '\n' {
			n++
		}
	}
	return n
}

// RestoreOnStartup hydrates engine + scheduler limits from persisted settings
// AND re-adds every persisted torrent to the engine so prior-session downloads
// resume on next launch. Call this once after constructing the Service.
func (s *Service) RestoreOnStartup(ctx context.Context) error {
	q := s.GetQueueLimits(ctx)
	if s.scheduler != nil {
		s.scheduler.SetLimits(q.MaxActiveDownloads, q.MaxActiveSeeds)
	}
	if err := s.applyLimits(ctx); err != nil {
		return err
	}

	records, err := s.torrents.List(ctx)
	if err != nil {
		return fmt.Errorf("list persisted torrents: %w", err)
	}
	failed := 0
	orphaned := 0
	for _, r := range records {
		var id engine.TorrentID
		var addErr error
		switch {
		case len(r.Metainfo) > 0:
			id, addErr = s.engine.AddFile(ctx, r.Metainfo, r.SavePath)
		case r.Magnet != "":
			id, addErr = s.engine.AddMagnet(ctx, r.Magnet, r.SavePath)
		default:
			log.Warn().Str("infohash", r.InfoHash).Str("name", r.Name).Msg("restore: skipping orphan record (no magnet, no metainfo)")
			orphaned++
			continue
		}
		if addErr != nil {
			log.Warn().Err(addErr).Str("infohash", r.InfoHash).Str("name", r.Name).Msg("restore: re-add failed")
			failed++
			continue
		}
		// If the persistence layer recorded this torrent as previously complete,
		// tell the engine — its post-VerifyData hook flags FilesMissing and
		// pauses the torrent if the on-disk pieces no longer match (the user
		// deleted files between sessions). Without this hint VerifyData silently
		// turns a deleted file into a redownload.
		if r.CompletedAt != nil {
			s.engine.MarkExpectedComplete(id)
		}
	}
	if failed > 0 || orphaned > 0 {
		log.Warn().Int("failed", failed).Int("orphaned", orphaned).Int("total", len(records)).Msg("restore: not all torrents could be re-added")
	}
	return nil
}

func (s *Service) intSetting(ctx context.Context, key string) int {
	v, err := s.settings.Get(ctx, key)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

func (s *Service) setIntSetting(ctx context.Context, key string, n int) error {
	return s.settings.Set(ctx, key, strconv.Itoa(n))
}

func (s *Service) boolSetting(ctx context.Context, key string) bool {
	v, _ := s.settings.Get(ctx, key)
	return v == "true"
}

func (s *Service) setBoolSetting(ctx context.Context, key string, b bool) error {
	v := "false"
	if b {
		v = "true"
	}
	return s.settings.Set(ctx, key, v)
}

// FeedDTO is the transport shape for an RSS/Atom feed subscription.
type FeedDTO struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	IntervalMin int    `json:"interval_min"`
	LastPolled  int64  `json:"last_polled"`
	ETag        string `json:"etag"`
	Enabled     bool   `json:"enabled"`
}

// FilterDTO is the transport shape for a per-feed regex filter rule.
type FilterDTO struct {
	ID         int    `json:"id"`
	FeedID     int    `json:"feed_id"`
	Regex      string `json:"regex"`
	CategoryID *int   `json:"category_id"`
	SavePath   string `json:"save_path"`
	Enabled    bool   `json:"enabled"`
}

func toFeedDTO(f persistence.Feed) FeedDTO {
	dto := FeedDTO{
		ID: f.ID, URL: f.URL, Name: f.Name, IntervalMin: f.IntervalMin,
		ETag: f.ETag, Enabled: f.Enabled,
	}
	if !f.LastPolled.IsZero() {
		dto.LastPolled = f.LastPolled.Unix()
	}
	return dto
}

func toFilterDTO(f persistence.Filter) FilterDTO {
	return FilterDTO{
		ID: f.ID, FeedID: f.FeedID, Regex: f.Regex, CategoryID: f.CategoryID,
		SavePath: f.SavePath, Enabled: f.Enabled,
	}
}

func (s *Service) ListFeeds(ctx context.Context) ([]FeedDTO, error) {
	if s.feeds == nil {
		return nil, nil
	}
	rows, err := s.feeds.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FeedDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, toFeedDTO(r))
	}
	return out, nil
}

func (s *Service) CreateFeed(ctx context.Context, dto FeedDTO) (int, error) {
	if _, err := validateFetchURL(dto.URL); err != nil {
		return 0, fmt.Errorf("feed URL must be http or https and not point at a private/loopback address: %w", err)
	}
	return s.feeds.Create(ctx, persistence.Feed{
		URL: dto.URL, Name: dto.Name, IntervalMin: dto.IntervalMin,
		ETag: dto.ETag, Enabled: dto.Enabled,
	})
}

func (s *Service) UpdateFeed(ctx context.Context, dto FeedDTO) error {
	if _, err := validateFetchURL(dto.URL); err != nil {
		return fmt.Errorf("feed URL must be http or https and not point at a private/loopback address: %w", err)
	}
	return s.feeds.Update(ctx, persistence.Feed{
		ID: dto.ID, URL: dto.URL, Name: dto.Name, IntervalMin: dto.IntervalMin,
		Enabled: dto.Enabled,
	})
}

func (s *Service) DeleteFeed(ctx context.Context, id int) error {
	return s.feeds.Delete(ctx, id)
}

func (s *Service) ListFiltersByFeed(ctx context.Context, feedID int) ([]FilterDTO, error) {
	if s.filters == nil {
		return nil, nil
	}
	rows, err := s.filters.ListByFeed(ctx, feedID)
	if err != nil {
		return nil, err
	}
	out := make([]FilterDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, toFilterDTO(r))
	}
	return out, nil
}

func (s *Service) CreateFilter(ctx context.Context, dto FilterDTO) (int, error) {
	return s.filters.Create(ctx, persistence.Filter{
		FeedID: dto.FeedID, Regex: dto.Regex, CategoryID: dto.CategoryID,
		SavePath: dto.SavePath, Enabled: dto.Enabled,
	})
}

func (s *Service) UpdateFilter(ctx context.Context, dto FilterDTO) error {
	return s.filters.Update(ctx, persistence.Filter{
		ID: dto.ID, FeedID: dto.FeedID, Regex: dto.Regex, CategoryID: dto.CategoryID,
		SavePath: dto.SavePath, Enabled: dto.Enabled,
	})
}

func (s *Service) DeleteFilter(ctx context.Context, id int) error {
	return s.filters.Delete(ctx, id)
}
