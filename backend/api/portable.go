package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// Deliberately excludes credentials, server exposure, automation, and machine-specific paths.
type SettingsBackupDTO struct {
	Version int                 `json:"version"`
	Limits  *LimitsDTO          `json:"limits"`
	Queue   *QueueLimitsDTO     `json:"queue"`
	Peers   *PeerLimitsDTO      `json:"peers"`
	Seeding *SeedingDefaultsDTO `json:"seeding"`
}

func (s *Service) ExportSettings(ctx context.Context) (SettingsBackupDTO, error) {
	if !CallerFrom(ctx).CanChangeSettings() {
		return SettingsBackupDTO{}, ErrForbidden
	}
	l, err := s.GetLimits(ctx)
	if err != nil {
		return SettingsBackupDTO{}, err
	}
	q, p, d := s.GetQueueLimits(ctx), s.GetPeerLimits(ctx), s.GetSeedingDefaults(ctx)
	return SettingsBackupDTO{Version: 1, Limits: &l, Queue: &q, Peers: &p, Seeding: &d}, nil
}

// Import persists one atomic snapshot before applying live limits.
func (s *Service) ImportSettings(ctx context.Context, b SettingsBackupDTO) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
	if b.Limits == nil || b.Queue == nil || b.Peers == nil || b.Seeding == nil {
		return fmt.Errorf("settings backup: incomplete backup")
	}
	if b.Version != 1 {
		return fmt.Errorf("settings backup: unsupported version")
	}
	for _, n := range []int{b.Limits.DownKbps, b.Limits.UpKbps, b.Limits.AltDownKbps, b.Limits.AltUpKbps, b.Queue.MaxActiveDownloads, b.Queue.MaxActiveSeeds, b.Peers.MaxPeersPerTorrent} {
		if n < 0 || n > 1000000000 {
			return fmt.Errorf("settings backup: limits must be between 0 and 1000000000")
		}
	}
	if b.Peers.ListenPort < 0 || b.Peers.ListenPort > 65535 {
		return fmt.Errorf("settings backup: listen port must be 0..65535")
	}
	if err := validateSeedLimits(b.Seeding.RatioLimit, b.Seeding.TimeMinLimit); err != nil {
		return err
	}
	values := map[string]string{}
	ints := map[string]int{settingDownKbps: b.Limits.DownKbps, settingUpKbps: b.Limits.UpKbps, settingAltDownKbps: b.Limits.AltDownKbps, settingAltUpKbps: b.Limits.AltUpKbps, settingMaxActiveDL: b.Queue.MaxActiveDownloads, settingMaxActiveSeeds: b.Queue.MaxActiveSeeds, settingPeerListenPort: b.Peers.ListenPort, settingMaxPeersPerTorrent: b.Peers.MaxPeersPerTorrent}
	for k, v := range ints {
		values[k] = strconv.Itoa(v)
	}
	for k, v := range map[string]bool{settingAltActive: b.Limits.AltActive, settingDHTEnabled: b.Peers.DHTEnabled, settingEncryptionEnabled: b.Peers.EncryptionEnabled, settingUPnPEnabled: b.Peers.UPnPEnabled} {
		values[k] = strconv.FormatBool(v)
	}
	values[settingSeedingRatioLimit] = ""
	values[settingSeedingTimeMiniLimit] = ""
	if b.Seeding.RatioLimit != nil {
		values[settingSeedingRatioLimit] = strconv.FormatFloat(*b.Seeding.RatioLimit, 'f', -1, 64)
	}
	if b.Seeding.TimeMinLimit != nil {
		values[settingSeedingTimeMiniLimit] = strconv.Itoa(*b.Seeding.TimeMinLimit)
	}
	if err := s.settings.SetMany(ctx, values); err != nil {
		return err
	}
	if s.scheduler != nil {
		s.scheduler.SetLimits(b.Queue.MaxActiveDownloads, b.Queue.MaxActiveSeeds)
	}
	if err := s.applyLimits(ctx); err != nil {
		return fmt.Errorf("settings backup: saved, but live bandwidth limits failed; restart Mosaic: %w", err)
	}
	if err := s.engine.ApplyPerTorrentMaxPeers(b.Peers.MaxPeersPerTorrent); err != nil {
		return fmt.Errorf("settings backup: saved, but live peer limits failed; restart Mosaic: %w", err)
	}
	return nil
}
func (s *Service) ExportTorrent(ctx context.Context, id string) (string, error) {
	if err := s.requireTorrentAccess(ctx, id, persistence.AccessViewer); err != nil {
		return "", err
	}
	data, err := s.engine.ExportTorrent(engine.TorrentID(id))
	if err != nil {
		rec, e := s.torrents.Get(ctx, id)
		if e != nil {
			return "", e
		}
		if len(rec.Metainfo) == 0 {
			return "", err
		}
		data = rec.Metainfo
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
func (s *Service) GetConnectionDiagnostics(ctx context.Context) (engine.ConnectionDiagnostics, error) {
	if !CallerFrom(ctx).CanChangeSettings() {
		return engine.ConnectionDiagnostics{}, ErrForbidden
	}
	return s.engine.ConnectionDiagnostics(), nil
}
