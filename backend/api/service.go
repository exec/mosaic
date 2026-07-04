package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
	"mosaic/backend/updater"
)

// jsonMarshal / jsonUnmarshal are thin wrappers so we can add logging later
// without touching every callsite.
var jsonMarshal   = json.Marshal
var jsonUnmarshal = json.Unmarshal

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
	trackers        *persistence.TorrentTrackers
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

	updaterHookMu    sync.RWMutex
	onUpdaterChanged func(UpdaterConfigDTO)

	updater       *updater.Updater // may be nil if not yet attached
	appVersion    string
	installSource updater.InstallSource // "apt" | "appimage" | "manual"
	rssPoller     *RSSPoller            // may be nil during startup; set by AttachRSSPoller
	watchFolder   *WatchFolder          // may be nil during startup; set by AttachWatchFolder

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

	// lastSeedCounters is the last-observed session transfer counters per
	// infohash. CheckSeedLimits adds the delta since the previous observation
	// to the persisted cumulative totals each pass — the engine's counters
	// reset every process start, so ratio limits computed straight from them
	// restarted at 0 on every launch. Guarded by seedCheckMu.
	seedCheckMu      sync.Mutex
	lastSeedCounters map[string]sessionCounters
}

// sessionCounters is one observation of the engine's session-scoped transfer
// counters for a torrent. See Service.lastSeedCounters.
type sessionCounters struct {
	up   int64
	down int64
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
	trackers *persistence.TorrentTrackers,
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
		trackers:        trackers,
		scheduler:       scheduler,
		defaultSavePath: defaultSavePath,
		focus:           make(map[int]focusState),
		callers:         newCallerCache(),
		lastSeedCounters: make(map[string]sessionCounters),
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

	settingWatchFolderPath          = "watch_folder.path"
	settingWatchFolderDeleteAfterAdd = "watch_folder.delete_after_add"
	settingWatchFolderEnabled       = "watch_folder.enabled"

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
