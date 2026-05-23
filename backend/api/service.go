package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	users           *persistence.Users
	access          *persistence.TorrentAccess
	scheduler       *engine.Scheduler
	defaultSavePath string

	// focus tracks which torrent each user's inspector is open on. Keyed by
	// Caller.UserID so multi-user mosaicd sessions don't clobber each other.
	focusMu sync.RWMutex
	focus   map[int]focusState

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

	// wsRevoker, if attached, is the remote-server Hub. Wired post-construction
	// (AttachWSRevoker) for the same import-cycle reason as sessions. When a
	// user is deleted/disabled/password-changed/renamed, invalidateCaller calls
	// RevokeUser here so any already-upgraded WebSocket is closed instead of
	// continuing to stream until its next re-auth check.
	wsRevoker WSRevoker

	// callers memoizes the Caller resolved for each user id so authenticated
	// HTTP requests and per-user WS ticks don't re-SELECT the users table on
	// every call. Invalidated alongside session revocation — see
	// invalidateCaller.
	callers *callerCache
}

// SessionRevoker is the subset of *remote.SessionStore that api.Service needs
// to call after credentials change. Defining it as a small interface here
// keeps the api package free of a remote-package import.
type SessionRevoker interface {
	RevokeAll()
	RevokeUser(userID int)
}

// AttachSessionRevoker wires the remote SessionStore into the Service so that
// SetWebPassword / SetWebConfig (when the username changes) can invalidate
// every active session. Pass nil (or never call) if there's no remote layer.
func (s *Service) AttachSessionRevoker(r SessionRevoker) {
	s.sessions = r
}

// WSRevoker is the subset of *remote.Hub that api.Service needs to terminate
// live WebSocket connections owned by a user whose credentials/authority just
// changed. Defined here as an interface to avoid an import cycle with
// backend/remote.
type WSRevoker interface {
	RevokeUser(userID int)
}

// AttachWSRevoker wires the remote Hub into the Service so invalidateCaller
// can hang up live WebSockets in addition to revoking sessions. Pass nil (or
// never call) when there's no remote layer.
func (s *Service) AttachWSRevoker(r WSRevoker) {
	s.wsRevoker = r
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
	users *persistence.Users,
	access *persistence.TorrentAccess,
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
		users:           users,
		access:          access,
		scheduler:       scheduler,
		defaultSavePath: defaultSavePath,
		focus:           make(map[int]focusState),
		callers:         newCallerCache(),
	}
}

// invalidateCaller revokes a user's live sessions and drops their cached
// Caller. Every credential/authority mutation must funnel through here so the
// session store and the caller cache can never disagree about a user — a stale
// cached Caller surviving a disable/demote would be a privilege-escalation bug.
func (s *Service) invalidateCaller(userID int) {
	if s.sessions != nil {
		s.sessions.RevokeUser(userID)
	}
	if s.wsRevoker != nil {
		s.wsRevoker.RevokeUser(userID)
	}
	if s.callers != nil {
		s.callers.evict(userID)
	}
}

// focusState is the inspector focus for a single user.
type focusState struct {
	id    engine.TorrentID
	scope engine.DetailScope
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
	settingUPnPEnabled         = "upnp_enabled"

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

// adminUserID is the id of the admin account seeded by migration 0009. The
// desktop "Web Interface" pane and mosaicd's ephemeral-password bootstrap both
// operate on this user — it is the single login for the desktop build and the
// initial administrator on a fresh mosaicd install.
const adminUserID = 1

func (s *Service) GetWebConfig(ctx context.Context) WebConfigDTO {
	if !CallerFrom(ctx).CanChangeSettings() {
		return WebConfigDTO{}
	}
	port := s.intSetting(ctx, settingWebPort)
	if port == 0 {
		port = 8080
	}
	dto := WebConfigDTO{
		Enabled:  s.boolSetting(ctx, settingWebEnabled),
		Port:     port,
		BindAll:  s.boolSettingDefault(ctx, settingWebBindAll, false),
		Username: "admin",
	}
	// Username + api-key hint live on the admin user row, not in settings.
	if u, err := s.users.Get(ctx, adminUserID); err == nil {
		dto.Username = u.Username
		dto.APIKey = u.APIKeyHint
	}
	return dto
}

func (s *Service) SetWebConfig(ctx context.Context, c WebConfigDTO) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
	if err := s.setBoolSetting(ctx, settingWebEnabled, c.Enabled); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingWebPort, c.Port); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingWebBindAll, c.BindAll); err != nil {
		return err
	}
	// The desktop "Web Interface" pane edits the admin user's username here.
	// A username change forces that user's sessions to re-authenticate.
	if c.Username != "" {
		u, err := s.users.Get(ctx, adminUserID)
		if err == nil && u.Username != c.Username {
			u.Username = c.Username
			if err := s.users.Update(ctx, u); err != nil {
				return err
			}
			s.invalidateCaller(adminUserID)
		}
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

// SetWebPassword sets the admin user's password (UI / REST "change password"
// path) and marks it operator-set so mosaicd stops minting ephemeral ones.
// Every active session for that user is revoked. The hash + password_set flag
// are written by a single atomic UPDATE, so there is no partial-failure window.
func (s *Service) SetWebPassword(ctx context.Context, plain string) error {
	if !CallerFrom(ctx).CanManageUsers() {
		return ErrForbidden
	}
	if err := s.setUserPasswordHash(ctx, adminUserID, plain, true); err != nil {
		return err
	}
	s.invalidateCaller(adminUserID)
	return nil
}

// SetWebPasswordEphemeral sets the admin user's password without flipping the
// "user set" flag and without touching active sessions. Used only by mosaicd's
// per-restart auto-generated password flow (qBittorrent-nox style).
func (s *Service) SetWebPasswordEphemeral(ctx context.Context, plain string) error {
	return s.setUserPasswordHash(ctx, adminUserID, plain, false)
}

// IsWebPasswordUserSet reports whether the admin user's password was set
// explicitly by the operator. mosaicd consults it to decide whether to mint a
// fresh ephemeral password on each boot. Fail-safe default is true: if the
// admin row is unreadable for any reason, we report "operator-set" so we don't
// silently regenerate a password over the operator's existing one.
func (s *Service) IsWebPasswordUserSet(ctx context.Context) bool {
	u, err := s.users.Get(ctx, adminUserID)
	if err != nil {
		return true
	}
	return u.PasswordSet
}

func (s *Service) setUserPasswordHash(ctx context.Context, userID int, plain string, userSet bool) error {
	hash, err := cred.HashPassword(plain)
	if err != nil {
		return err
	}
	return s.users.SetPasswordHash(ctx, userID, hash, userSet)
}

// RotateAPIKey mints a fresh API key for the admin user (desktop "Web
// Interface" pane). The cleartext is returned once — only its hash is stored.
func (s *Service) RotateAPIKey(ctx context.Context) (string, error) {
	if !CallerFrom(ctx).CanManageUsers() {
		return "", ErrForbidden
	}
	return s.RotateUserAPIKey(ctx, adminUserID)
}

// ReconcileLegacyAPIKey migrates a pre-0009 plaintext web_api_key setting into
// the admin user's hashed key column, then clears the setting. Idempotent and
// safe to call on every startup: it no-ops once the legacy key is gone or the
// admin already has a key.
func (s *Service) ReconcileLegacyAPIKey(ctx context.Context) error {
	legacy, _ := s.settings.Get(ctx, settingWebAPIKey)
	if legacy == "" {
		return nil
	}
	u, err := s.users.Get(ctx, adminUserID)
	if err != nil {
		return err
	}
	if u.APIKeyHash == "" {
		if err := s.users.SetAPIKey(ctx, adminUserID,
			cred.HashAPIKey(legacy), cred.APIKeyHint(legacy)); err != nil {
			return err
		}
	}
	return s.settings.Set(ctx, settingWebAPIKey, "")
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
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return UpdaterConfigDTO{}
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	// Access is the requesting caller's access level on this torrent
	// ("owner" | "editor" | "viewer"). Admins always see "owner". The SPA
	// uses it to gate per-row controls.
	Access string `json:"access"`
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
	caller := CallerFrom(ctx)
	if !caller.CanAddTorrents() {
		return "", ErrForbidden
	}
	savePath, err := s.resolveSavePath(ctx, caller, savePath)
	if err != nil {
		return "", err
	}
	id, err := s.engine.AddMagnet(ctx, magnet, savePath)
	if err != nil {
		return "", fmt.Errorf("add magnet: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		// Highly unlikely directly after AddMagnet, but if it happens we can't
		// build a record without the name — roll the engine add back rather
		// than leave a torrent we can't track.
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("AddMagnet: engine rollback after snapshot failure also failed")
		}
		return "", err
	}
	if err := s.persistAndGrantTorrent(ctx, id, caller, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		Magnet:   magnet,
		SavePath: savePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", err
	}
	return id, nil
}

// grantOwner records that the caller owns a torrent. Adding an infohash that
// another user already added makes the caller a co-owner (Grant upserts).
func (s *Service) grantOwner(ctx context.Context, infohash string, caller Caller) error {
	uid := caller.UserID
	return s.access.Grant(ctx, infohash, uid, persistence.AccessOwner, &uid)
}

// persistAndGrantTorrent runs the post-engine-add steps (persist record,
// record ownership) with rollback semantics. The caller has already added the
// torrent to the engine and provides the persistence record. Without this
// rollback, a failed Save left the torrent in the engine but absent from the
// DB — it would download for the rest of this session, then vanish on next
// restart (RestoreOnStartup walks the DB, not the engine). A failed grantOwner
// left an orphaned, ownerless record visible to admins only.
//
// engine.Remove is called with deleteFiles=false: the operation just failed,
// nothing material has been downloaded yet, and we don't want to clobber
// anything that might have. Rollback errors are logged but not returned —
// the original failure is what the caller needs to see.
func (s *Service) persistAndGrantTorrent(ctx context.Context, id engine.TorrentID, caller Caller, rec persistence.TorrentRecord) error {
	if err := s.torrents.Save(ctx, rec); err != nil {
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("add torrent: engine rollback after persist failure also failed; torrent will linger until restart")
		}
		return fmt.Errorf("persist: %w", err)
	}
	if err := s.grantOwner(ctx, string(id), caller); err != nil {
		if rmErr := s.torrents.Remove(ctx, string(id)); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("add torrent: persist rollback after grant failure also failed; record will survive as orphaned")
		}
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("add torrent: engine rollback after grant failure also failed; torrent will linger until restart")
		}
		return fmt.Errorf("grant owner: %w", err)
	}
	return nil
}

// resolveSavePath returns the cleaned absolute save path the engine should
// MkdirAll into, after applying the per-user containment policy.
//
// Admins (including the system caller used by the Wails desktop and internal
// workers) keep unrestricted behavior — the operator already has shell access
// to the host. Non-admin callers are pinned to <defaultSavePath>/<username>:
// an unconstrained save_path would let a low-privilege multi-user mosaicd
// account write torrent data into arbitrary directories the daemon process
// can reach (/etc/cron.d, another user's home, the daemon's data dir).
func (s *Service) resolveSavePath(ctx context.Context, caller Caller, savePath string) (string, error) {
	if caller.IsAdmin() {
		if savePath == "" {
			savePath = s.defaultPath(ctx)
		}
		return engine.ValidateSavePath("", savePath, false)
	}
	if caller.Username == "" || s.defaultSavePath == "" {
		return "", ErrForbidden
	}
	userRoot := filepath.Join(s.defaultSavePath, caller.Username)
	if savePath == "" {
		savePath = userRoot
	}
	return engine.ValidateSavePath(userRoot, savePath, true)
}

// requireTorrentAccess returns ErrForbidden unless the caller holds at least
// minLevel access on the torrent. Admins and the system caller always pass.
func (s *Service) requireTorrentAccess(ctx context.Context, infohash, minLevel string) error {
	caller := CallerFrom(ctx)
	if caller.SeesAllTorrents() {
		return nil
	}
	lvl, err := s.access.AccessFor(ctx, infohash, caller.UserID)
	if err != nil {
		return err
	}
	if persistence.AccessRank(lvl) < persistence.AccessRank(minLevel) {
		return ErrForbidden
	}
	return nil
}

func (s *Service) AddTorrentFile(ctx context.Context, filePath, savePath string) (engine.TorrentID, error) {
	caller := CallerFrom(ctx)
	if !caller.CanAddTorrents() {
		return "", ErrForbidden
	}
	savePath, err := s.resolveSavePath(ctx, caller, savePath)
	if err != nil {
		return "", err
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
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("AddTorrentFile: engine rollback after snapshot failure also failed")
		}
		return "", err
	}
	if err := s.persistAndGrantTorrent(ctx, id, caller, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
		Metainfo: blob,
	}); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) AddTorrentBytes(ctx context.Context, blob []byte, savePath string) (engine.TorrentID, error) {
	caller := CallerFrom(ctx)
	if !caller.CanAddTorrents() {
		return "", ErrForbidden
	}
	savePath, err := s.resolveSavePath(ctx, caller, savePath)
	if err != nil {
		return "", err
	}
	id, err := s.engine.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", fmt.Errorf("add torrent bytes: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("AddTorrentBytes: engine rollback after snapshot failure also failed")
		}
		return "", err
	}
	if err := s.persistAndGrantTorrent(ctx, id, caller, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
		Metainfo: blob,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// Pause/Resume/Recheck require editor-or-higher access on the torrent.
func (s *Service) Pause(ctx context.Context, id engine.TorrentID) error {
	if err := s.requireTorrentAccess(ctx, string(id), persistence.AccessEditor); err != nil {
		return err
	}
	return s.engine.Pause(id)
}

func (s *Service) Resume(ctx context.Context, id engine.TorrentID) error {
	if err := s.requireTorrentAccess(ctx, string(id), persistence.AccessEditor); err != nil {
		return err
	}
	return s.engine.Resume(id)
}

func (s *Service) Recheck(ctx context.Context, id engine.TorrentID) error {
	if err := s.requireTorrentAccess(ctx, string(id), persistence.AccessEditor); err != nil {
		return err
	}
	return s.engine.Recheck(id)
}

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

// Remove tears a torrent down or detaches it from the caller's view. An admin
// (or the last remaining owner) fully removes it from the engine + database;
// any other user with access just loses their own grant — the torrent keeps
// running for everyone else. A caller with no access gets ErrForbidden.
func (s *Service) Remove(ctx context.Context, id engine.TorrentID, deleteFiles bool) error {
	caller := CallerFrom(ctx)
	hash := string(id)
	if !caller.SeesAllTorrents() {
		lvl, err := s.access.AccessFor(ctx, hash, caller.UserID)
		if err != nil {
			return err
		}
		if lvl == "" {
			return ErrForbidden
		}
		if lvl != persistence.AccessOwner {
			return s.access.Revoke(ctx, hash, caller.UserID)
		}
		owners, err := s.access.OwnerCount(ctx, hash)
		if err != nil {
			return err
		}
		if owners > 1 {
			return s.access.Revoke(ctx, hash, caller.UserID)
		}
	}
	if err := s.engine.Remove(id, deleteFiles); err != nil {
		return err
	}
	// torrent_access rows cascade-delete with the torrents row.
	return s.torrents.Remove(ctx, hash)
}

// TorrentTickSnapshot is the per-tick state shared across every connected
// user: the engine's torrent snapshots, the persistence records keyed by
// infohash, and the tags map. None of it is caller-dependent, so mosaicd's
// streamTicks builds it ONCE per tick (see BuildTorrentTickSnapshot) and then
// fans it out — instead of re-running torrents.List + tags.ForAllTorrents +
// the full engine walk once per connected user.
type TorrentTickSnapshot struct {
	snaps      []engine.Snapshot
	byHash     map[string]persistence.TorrentRecord
	tagsByHash map[string][]persistence.Tag
}

// BuildTorrentTickSnapshot gathers the caller-independent torrent state for
// one tick. Call it once, then pass the result to ListTorrentsFromSnapshot /
// GlobalStatsFromSnapshot for each connected user.
func (s *Service) BuildTorrentTickSnapshot(ctx context.Context) (TorrentTickSnapshot, error) {
	records, err := s.torrents.List(ctx)
	if err != nil {
		return TorrentTickSnapshot{}, err
	}
	byHash := make(map[string]persistence.TorrentRecord, len(records))
	for _, r := range records {
		byHash[r.InfoHash] = r
	}
	// Pre-v0.4.3 we called s.tags.ForTorrent(ctx, infohash) inside the DTO
	// loop — N+1 SELECTs every tick (~500ms cadence). One bulk fetch instead,
	// hashed by infohash so assembly is an O(1) lookup.
	tagsByHash, err := s.tags.ForAllTorrents(ctx)
	if err != nil {
		return TorrentTickSnapshot{}, err
	}
	return TorrentTickSnapshot{
		snaps:      s.engine.List(),
		byHash:     byHash,
		tagsByHash: tagsByHash,
	}, nil
}

func (s *Service) ListTorrents(ctx context.Context) ([]TorrentDTO, error) {
	tick, err := s.BuildTorrentTickSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListTorrentsFromSnapshot(ctx, tick)
}

// ListTorrentsFromSnapshot assembles a caller's torrent DTO list from an
// already-built TorrentTickSnapshot. The shared snapshot is byte-identical
// across users; only the access filter, per-user Access level, sort, and DTO
// assembly happen here — so streamTicks pays the DB/engine cost once per tick
// rather than once per connected user. Output matches the old per-user
// ListTorrents exactly.
func (s *Service) ListTorrentsFromSnapshot(ctx context.Context, tick TorrentTickSnapshot) ([]TorrentDTO, error) {
	caller := CallerFrom(ctx)
	// Non-admin callers see only torrents they hold an access grant for.
	var accessByHash map[string]string
	if !caller.SeesAllTorrents() {
		var err error
		accessByHash, err = s.access.InfohashesForUser(ctx, caller.UserID)
		if err != nil {
			return nil, err
		}
	}
	out := make([]TorrentDTO, 0, len(tick.snaps))
	for _, snap := range tick.snaps {
		hash := string(snap.ID)
		access := persistence.AccessOwner
		if !caller.SeesAllTorrents() {
			lvl, visible := accessByHash[hash]
			if !visible {
				continue
			}
			access = lvl
		}
		rec, ok := tick.byHash[hash]
		addedAt := time.Now()
		if ok {
			snap.SavePath = rec.SavePath
			if snap.Magnet == "" {
				snap.Magnet = rec.Magnet
			}
			addedAt = rec.AddedAt
		}
		dto := toDTO(snap, addedAt)
		dto.Access = access
		if ok {
			dto.CategoryID = rec.CategoryID
		}
		tags := tick.tagsByHash[hash]
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

// EngineSnapshots returns the engine's current per-torrent snapshot slice.
// It is caller-independent: streamTicks walks the engine once per tick and
// passes the result to GlobalStatsFromSnapshot for each connected user.
func (s *Service) EngineSnapshots() []engine.Snapshot {
	return s.engine.List()
}

func (s *Service) GlobalStats(ctx context.Context) (GlobalStats, error) {
	return s.GlobalStatsFromSnapshot(ctx, s.engine.List())
}

// GlobalStatsFromSnapshot computes a caller's status-bar aggregate from an
// already-fetched engine snapshot slice. The snapshot is identical across
// users; per-user stats are just a filtered reduction of it, so streamTicks
// can walk the engine once per tick and reuse the slice for every connected
// user. Output matches the old per-user GlobalStats exactly.
func (s *Service) GlobalStatsFromSnapshot(ctx context.Context, snaps []engine.Snapshot) (GlobalStats, error) {
	caller := CallerFrom(ctx)
	// Non-admin callers get an aggregate scoped to their own torrents only —
	// the status bar must not leak the existence/bandwidth of other users'.
	var visible map[string]string
	if !caller.SeesAllTorrents() {
		var err error
		visible, err = s.access.InfohashesForUser(ctx, caller.UserID)
		if err != nil {
			return GlobalStats{}, err
		}
	}
	var st GlobalStats
	for _, snap := range snaps {
		if visible != nil {
			if _, ok := visible[string(snap.ID)]; !ok {
				continue
			}
		}
		st.TotalTorrents++
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
	// Status flags so the inspector can color the progress bar without
	// re-deriving from the torrents list.
	Paused       bool `json:"paused"`
	Completed    bool `json:"completed"`
	FilesMissing bool `json:"files_missing"`

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

// SetInspectorFocus tells the service which torrent + tabs the caller's UI is
// looking at. Focus is tracked per user, so concurrent mosaicd sessions don't
// clobber each other. Subsequent DetailForFocus calls (and the inspector:tick
// event) return the appropriately-scoped Detail. tabs is a subset of:
// "overview", "files", "peers", "trackers", "speed".
func (s *Service) SetInspectorFocus(ctx context.Context, id string, tabs []string) error {
	uid := CallerFrom(ctx).UserID
	if id == "" {
		s.ClearInspectorFocus(ctx)
		return nil
	}
	scope := scopeForTabs(tabs)
	s.focusMu.Lock()
	s.focus[uid] = focusState{id: engine.TorrentID(id), scope: scope}
	s.focusMu.Unlock()
	return nil
}

// ClearInspectorFocus drops the caller's inspector focus.
func (s *Service) ClearInspectorFocus(ctx context.Context) {
	uid := CallerFrom(ctx).UserID
	s.focusMu.Lock()
	delete(s.focus, uid)
	s.focusMu.Unlock()
}

// DetailForFocus returns the caller's focused torrent detail, or nil if no
// focus is set or the caller no longer has access to that torrent.
func (s *Service) DetailForFocus(ctx context.Context) (*DetailDTO, error) {
	caller := CallerFrom(ctx)
	s.focusMu.RLock()
	f, ok := s.focus[caller.UserID]
	s.focusMu.RUnlock()
	if !ok || f.id == "" {
		return nil, nil
	}
	if !caller.SeesAllTorrents() {
		lvl, err := s.access.AccessFor(ctx, string(f.id), caller.UserID)
		if err != nil {
			return nil, err
		}
		if lvl == "" {
			return nil, nil
		}
	}
	d, err := s.engine.DetailedSnapshot(f.id, f.scope)
	if err != nil {
		return nil, err
	}
	dto := detailToDTO(d, s.lookupAddedAt(ctx, f.id))
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
		Peers:        snap.Peers,
		Seeds:        snap.Seeds,
		AddedAt:      addedAt.Unix(),
		Paused:       snap.Paused,
		Completed:    snap.Completed,
		FilesMissing: snap.FilesMissing,
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
	if !CallerFrom(ctx).CanManageCatTags() {
		return 0, ErrForbidden
	}
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
	if !CallerFrom(ctx).CanManageCatTags() {
		return ErrForbidden
	}
	return s.categories.Update(ctx, persistence.Category{ID: id, Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) DeleteCategory(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanManageCatTags() {
		return ErrForbidden
	}
	return s.categories.Delete(ctx, id)
}

func (s *Service) CreateTag(ctx context.Context, name, color string) (int, error) {
	if !CallerFrom(ctx).CanManageCatTags() {
		return 0, ErrForbidden
	}
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
	if !CallerFrom(ctx).CanManageCatTags() {
		return ErrForbidden
	}
	return s.tags.Delete(ctx, id)
}

// AssignTag/UnassignTag attach an existing tag to a torrent — that is per-
// torrent organisation, so editor access on the torrent is sufficient (you
// don't need the global manage-cat-tags permission, which gates the tag set
// itself).
func (s *Service) AssignTag(ctx context.Context, infohash string, tagID int) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	return s.tags.Assign(ctx, infohash, tagID)
}

func (s *Service) UnassignTag(ctx context.Context, infohash string, tagID int) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
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
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	return s.torrents.SetCategory(ctx, infohash, categoryID)
}

func (s *Service) SetFilePriorities(ctx context.Context, infohash string, prios map[int]string) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return s.boolSetting(ctx, settingAltActive), ErrForbidden
	}
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
	ListenPort         int  `json:"listen_port"`           // 0 = let OS pick at startup
	MaxPeersPerTorrent int  `json:"max_peers_per_torrent"` // 0 = anacrolix default (80)
	DHTEnabled         bool `json:"dht_enabled"`
	EncryptionEnabled  bool `json:"encryption_enabled"`
	// UPnPEnabled enables automatic UPnP/NAT-PMP port forwarding via the
	// router. anacrolix runs the discovery on startup so a restart is required
	// after toggling. Defaults to true (anacrolix's own default).
	UPnPEnabled bool `json:"upnp_enabled"`
}

func (s *Service) GetPeerLimits(ctx context.Context) PeerLimitsDTO {
	if !CallerFrom(ctx).CanChangeSettings() {
		return PeerLimitsDTO{}
	}
	// DHT + encryption + UPnP default to true (matches anacrolix's defaults +
	// good privacy hygiene). The bool helpers in this Service treat unset as
	// false, so use a presence-aware reader.
	dhtRaw, _ := s.settings.Get(ctx, settingDHTEnabled)
	dhtEnabled := dhtRaw == "" || dhtRaw == "true" // default-on
	encRaw, _ := s.settings.Get(ctx, settingEncryptionEnabled)
	encEnabled := encRaw == "" || encRaw == "true" // default-on
	upnpRaw, _ := s.settings.Get(ctx, settingUPnPEnabled)
	upnpEnabled := upnpRaw == "" || upnpRaw == "true" // default-on (matches anacrolix)
	return PeerLimitsDTO{
		ListenPort:         s.intSetting(ctx, settingPeerListenPort),
		MaxPeersPerTorrent: s.intSetting(ctx, settingMaxPeersPerTorrent),
		DHTEnabled:         dhtEnabled,
		EncryptionEnabled:  encEnabled,
		UPnPEnabled:        upnpEnabled,
	}
}

func (s *Service) SetPeerLimits(ctx context.Context, p PeerLimitsDTO) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if err := s.setBoolSetting(ctx, settingUPnPEnabled, p.UPnPEnabled); err != nil {
		return err
	}
	// Per-torrent cap is the only one anacrolix lets us mutate at runtime.
	// ListenPort / DHT / Encryption / UPnP changes take effect at next launch.
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if err := s.torrents.SetQueuePosition(ctx, infohash, pos); err != nil {
		return err
	}
	s.engine.SetQueuePosition(engine.TorrentID(infohash), pos)
	return nil
}

func (s *Service) SetForceStart(ctx context.Context, infohash string, force bool) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return 0, ErrForbidden
	}
	return s.scheduleRules.Create(ctx, persistence.ScheduleRule{
		DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	})
}

func (s *Service) UpdateScheduleRule(ctx context.Context, r ScheduleRuleDTO) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
	return s.scheduleRules.Update(ctx, persistence.ScheduleRule{
		ID: r.ID, DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	})
}

func (s *Service) DeleteScheduleRule(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return BlocklistDTO{}
	}
	rawURL, _ := s.settings.Get(ctx, settingBlocklistURL)
	en := s.boolSetting(ctx, settingBlocklistEnabled)
	s.blocklistMu.RLock()
	defer s.blocklistMu.RUnlock()
	dto := BlocklistDTO{URL: redactURLCredentials(rawURL), Enabled: en, Entries: s.blocklist.entries, Error: s.blocklist.lastErr}
	if !s.blocklist.loadedAt.IsZero() {
		dto.LastLoadedAt = s.blocklist.loadedAt.Unix()
	}
	return dto
}

// redactURLCredentials strips userinfo from a URL so a stored "http://user:pw@host/..."
// blocklist URL doesn't leak the password back to the SPA in GetBlocklist. The
// raw URL with credentials remains in settings (so refresh still works); only
// the DTO is scrubbed.
func redactURLCredentials(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func (s *Service) SetBlocklistURL(ctx context.Context, url string, enabled bool) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
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
//
// Installs SystemCaller on the context so callers don't have to — this is the
// trusted startup hydration path, not a user-driven action.
func (s *Service) RestoreOnStartup(ctx context.Context) error {
	ctx = WithCaller(ctx, SystemCaller)
	if err := s.recoverInvalidAdminPasswordHash(ctx); err != nil {
		log.Warn().Err(err).Msg("restore: recover invalid admin password hash failed")
	}
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
		// Propagate persisted queue ordering so the scheduler's tie-breaks
		// are stable across launches. Without this every restored torrent
		// has QueuePosition=0 and ForceStart=false in the engine, and any
		// non-zero MaxActiveSeeds / MaxActiveDownloads picks arbitrary
		// victims via sort.Slice's unstable ordering — a completed torrent
		// that was seeding fine yesterday can come back paused-by-queue
		// today with no peers attached.
		s.engine.SetQueuePosition(id, r.QueuePosition)
		s.engine.SetForceStart(id, r.ForceStart)
		// Surface persisted user-pause too, otherwise a torrent the user
		// paused last session resumes silently.
		if r.Paused {
			if err := s.engine.Pause(id); err != nil {
				log.Warn().Err(err).Str("infohash", r.InfoHash).Msg("restore: re-pause failed")
			}
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
	if !CallerFrom(ctx).CanManageRSS() {
		return 0, ErrForbidden
	}
	if _, err := validateFetchURL(dto.URL); err != nil {
		return 0, fmt.Errorf("feed URL must be http or https and not point at a private/loopback address: %w", err)
	}
	return s.feeds.Create(ctx, persistence.Feed{
		URL: dto.URL, Name: dto.Name, IntervalMin: dto.IntervalMin,
		ETag: dto.ETag, Enabled: dto.Enabled,
	})
}

func (s *Service) UpdateFeed(ctx context.Context, dto FeedDTO) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	if _, err := validateFetchURL(dto.URL); err != nil {
		return fmt.Errorf("feed URL must be http or https and not point at a private/loopback address: %w", err)
	}
	return s.feeds.Update(ctx, persistence.Feed{
		ID: dto.ID, URL: dto.URL, Name: dto.Name, IntervalMin: dto.IntervalMin,
		Enabled: dto.Enabled,
	})
}

func (s *Service) DeleteFeed(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
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
	if !CallerFrom(ctx).CanManageRSS() {
		return 0, ErrForbidden
	}
	return s.filters.Create(ctx, persistence.Filter{
		FeedID: dto.FeedID, Regex: dto.Regex, CategoryID: dto.CategoryID,
		SavePath: dto.SavePath, Enabled: dto.Enabled,
	})
}

func (s *Service) UpdateFilter(ctx context.Context, dto FilterDTO) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	return s.filters.Update(ctx, persistence.Filter{
		ID: dto.ID, FeedID: dto.FeedID, Regex: dto.Regex, CategoryID: dto.CategoryID,
		SavePath: dto.SavePath, Enabled: dto.Enabled,
	})
}

func (s *Service) DeleteFilter(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	return s.filters.Delete(ctx, id)
}
