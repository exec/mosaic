package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"mosaic/backend/persistence"
	"mosaic/backend/remote/cred"
)

// ─── Web config ─────────────────────────────────────────────────────────────

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

// ─── Web password ────────────────────────────────────────────────────────────

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

// ─── Save path ───────────────────────────────────────────────────────────────

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

// ─── Bandwidth limits ────────────────────────────────────────────────────────

// LimitsDTO is the bandwidth-limits transport shape (kbps units).
type LimitsDTO struct {
	DownKbps    int  `json:"down_kbps"`
	UpKbps      int  `json:"up_kbps"`
	AltDownKbps int  `json:"alt_down_kbps"`
	AltUpKbps   int  `json:"alt_up_kbps"`
	AltActive   bool `json:"alt_active"`
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

// ─── Queue limits ────────────────────────────────────────────────────────────

// QueueLimitsDTO is the queue-slot transport shape.
type QueueLimitsDTO struct {
	MaxActiveDownloads int `json:"max_active_downloads"`
	MaxActiveSeeds     int `json:"max_active_seeds"`
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

// ─── Peer limits ─────────────────────────────────────────────────────────────

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
	if err := s.engine.ApplyPerTorrentMaxPeers(p.MaxPeersPerTorrent); err != nil {
		return err
	}
	return nil
}

// ─── Schedule rules ──────────────────────────────────────────────────────────

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

// ─── IP Blocklist ────────────────────────────────────────────────────────────

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

// ─── Watch folder ────────────────────────────────────────────────────────────

// WatchFolderDTO is the transport shape for the watch-folder settings.
type WatchFolderDTO struct {
	Path           string `json:"path"`
	DeleteAfterAdd bool   `json:"delete_after_add"`
	Enabled        bool   `json:"enabled"`
}

// GetWatchFolder reads the current watch-folder configuration from settings.
func (s *Service) GetWatchFolder(ctx context.Context) WatchFolderDTO {
	if !CallerFrom(ctx).CanChangeSettings() {
		return WatchFolderDTO{}
	}
	path, _ := s.settings.Get(ctx, settingWatchFolderPath)
	return WatchFolderDTO{
		Path:           path,
		DeleteAfterAdd: s.boolSetting(ctx, settingWatchFolderDeleteAfterAdd),
		Enabled:        s.boolSetting(ctx, settingWatchFolderEnabled),
	}
}

// SetWatchFolder persists the watch-folder configuration and (re)starts or
// stops the watcher accordingly.
func (s *Service) SetWatchFolder(ctx context.Context, c WatchFolderDTO) error {
	if !CallerFrom(ctx).IsAdmin() {
		return ErrForbidden
	}
	if err := s.settings.Set(ctx, settingWatchFolderPath, c.Path); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingWatchFolderDeleteAfterAdd, c.DeleteAfterAdd); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingWatchFolderEnabled, c.Enabled); err != nil {
		return err
	}
	if s.watchFolder != nil {
		if c.Enabled && c.Path != "" {
			s.watchFolder.Start(c.Path, c.DeleteAfterAdd)
		} else {
			s.watchFolder.Stop()
		}
	}
	return nil
}

// AttachWatchFolder wires the live *WatchFolder into the Service so that
// SetWatchFolder can (re)start the polling loop when the user changes the
// configuration. main.go calls this once after construction.
func (s *Service) AttachWatchFolder(w *WatchFolder) {
	s.watchFolder = w
}

// ─── Desktop integration ─────────────────────────────────────────────────────

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

// ─── Setting helpers ─────────────────────────────────────────────────────────

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