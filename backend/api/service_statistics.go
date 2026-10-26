package api

import (
	"context"
	"time"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

type TransferTotalsDTO struct {
	Uploaded   int64 `json:"uploaded"`
	Downloaded int64 `json:"downloaded"`
}
type TransferStatisticsDTO struct {
	Session          TransferTotalsDTO         `json:"session"`
	Today            TransferTotalsDTO         `json:"today"`
	Lifetime         TransferTotalsDTO         `json:"lifetime"`
	SessionStartedAt int64                     `json:"session_started_at"`
	SampledAt        int64                     `json:"sampled_at"`
	Days             []persistence.TransferDay `json:"days"`
}

// shortcut: statistics follow the current library; retain tombstones if removed-torrent totals are needed.
func (s *Service) GetTransferStatistics(ctx context.Context) (TransferStatisticsDTO, error) {
	if err := requireAuth(CallerFrom(ctx)); err != nil {
		return TransferStatisticsDTO{}, err
	}
	s.torrentMu.Lock()
	defer s.torrentMu.Unlock()
	caller := CallerFrom(ctx)
	records, err := s.torrents.List(ctx)
	if err != nil {
		return TransferStatisticsDTO{}, err
	}
	var access map[string]string
	if !caller.SeesAllTorrents() {
		access, err = s.access.InfohashesForUser(ctx, caller.UserID)
		if err != nil {
			return TransferStatisticsDTO{}, err
		}
	}
	snaps := map[string]engine.Snapshot{}
	for _, snap := range s.engine.List() {
		snaps[string(snap.ID)] = snap
	}
	out := TransferStatisticsDTO{SessionStartedAt: s.statisticsStarted.Unix(), SampledAt: time.Now().Unix()}
	for _, rec := range records {
		if !caller.SeesAllTorrents() && access[rec.InfoHash] == "" {
			continue
		}
		if snap, ok := snaps[rec.InfoHash]; ok {
			if _, err := s.checkpointTransferTotals(ctx, &rec, snap); err != nil {
				return TransferStatisticsDTO{}, err
			}
		}
		out.Lifetime.Uploaded += rec.TotalUploaded
		out.Lifetime.Downloaded += rec.TotalDownloaded
		s.seedCheckMu.Lock()
		base, seen := s.sessionBaseline[rec.InfoHash]
		s.seedCheckMu.Unlock()
		if seen {
			out.Session.Uploaded += max(0, rec.TotalUploaded-base.up)
			out.Session.Downloaded += max(0, rec.TotalDownloaded-base.down)
		}
	}
	out.Days, err = s.torrents.TransferDays(ctx, caller.UserID, caller.SeesAllTorrents())
	if err != nil {
		return TransferStatisticsDTO{}, err
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, day := range out.Days {
		if day.Day == today {
			out.Today = TransferTotalsDTO{Uploaded: day.Uploaded, Downloaded: day.Downloaded}
		}
	}
	return out, nil
}

func (s *Service) transferTotals(rec persistence.TorrentRecord, snap engine.Snapshot) (down, up int64) {
	s.seedCheckMu.Lock()
	last := s.lastSeedCounters[rec.InfoHash]
	s.seedCheckMu.Unlock()
	d, u := snap.BytesDown-last.down, snap.BytesUp-last.up
	if d < 0 {
		d = snap.BytesDown
	}
	if u < 0 {
		u = snap.BytesUp
	}
	return rec.TotalDownloaded + d, rec.TotalUploaded + u
}
