package api

import (
	"context"
	"fmt"

	"mosaic/backend/updater"
)

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

// AppVersion returns the build-time version string the Service was set with.
// Empty string if neither SetAppVersion nor AttachUpdater has been called.
func (s *Service) AppVersion() string {
	return s.appVersion
}

// SetAppVersion records the build-time version on the Service so AppVersion()
// (and the /api/version endpoint behind it) can return it even when the
// updater isn't wired — notably in the daemon, where auto-update is
// intentionally not attached. AttachUpdater also writes this field, so calling
// SetAppVersion before AttachUpdater is safe and the values will match.
func (s *Service) SetAppVersion(version string) {
	s.appVersion = version
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
	// Admin-only, not merely CanChangeSettings: the updater downloads and
	// swaps the running server binary, so steering its channel/enablement is a
	// host-integrity operation, not an ordinary preference. PermChangeSettings
	// is a delegable, sub-admin flag and must not reach this far.
	if !CallerFrom(ctx).IsAdmin() {
		return ErrForbidden
	}
	if c.Channel != "stable" && c.Channel != "beta" {
		return fmt.Errorf("channel must be stable or beta")
	}
	if err := s.setBoolSetting(ctx, settingUpdaterEnabled, c.Enabled); err != nil {
		return err
	}
	if err := s.settings.Set(ctx, settingUpdaterChannel, c.Channel); err != nil {
		return err
	}
	s.fireUpdaterConfigChanged(s.GetUpdaterConfig(ctx))
	return nil
}

// OnUpdaterConfigChange registers a synchronous callback invoked after a
// SetUpdaterConfig commit, mirroring OnWebConfigChange /
// OnDesktopIntegrationChange. main.go uses it to push channel changes into
// the live GitHubSource and to start/stop the periodic check goroutine —
// without it, both silently required an app restart. Pass nil to unregister.
// Only one callback is supported. The daemon never registers one (auto-update
// is intentionally not wired there); firing is nil-safe.
func (s *Service) OnUpdaterConfigChange(cb func(UpdaterConfigDTO)) {
	s.updaterHookMu.Lock()
	s.onUpdaterChanged = cb
	s.updaterHookMu.Unlock()
}

func (s *Service) fireUpdaterConfigChanged(c UpdaterConfigDTO) {
	s.updaterHookMu.RLock()
	cb := s.onUpdaterChanged
	s.updaterHookMu.RUnlock()
	if cb != nil {
		cb(c)
	}
}

func (s *Service) CheckForUpdate(ctx context.Context) (UpdateInfoDTO, error) {
	// Same admin-only gate as InstallUpdate / SetUpdaterConfig: the check
	// performs an outbound HTTP request and writes two settings rows, and it is
	// the precursor to an install, so it belongs to the same host-integrity
	// boundary rather than the delegable PermChangeSettings flag.
	if !CallerFrom(ctx).IsAdmin() {
		return UpdateInfoDTO{}, ErrForbidden
	}
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
	// Admin-only: this replaces the running server binary. A non-admin holding
	// the delegable PermChangeSettings flag must not be able to swap the host's
	// executable.
	if !CallerFrom(ctx).IsAdmin() {
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