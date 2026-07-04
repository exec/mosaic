package api

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// ─── Seed Policy ─────────────────────────────────────────────────────────────

const (
	settingSeedingRatioLimit  = "seeding.ratio_limit"   // float64 as string; "" = no global limit
	settingSeedingTimeMiniLimit = "seeding.time_min_limit" // int (minutes) as string; "" = no global limit
)

// SeedingDefaultsDTO carries the global seeding-stop defaults.
// nil pointer fields mean "no limit".
type SeedingDefaultsDTO struct {
	RatioLimit   *float64 `json:"ratio_limit"`    // bytes_up / bytes_done; nil = no limit
	TimeMinLimit *int     `json:"time_min_limit"` // minutes; nil = no limit
}

// SeedPolicyDTO is the per-torrent override.
// nil = "use global default"; non-nil object with nil fields = "no limit for this torrent".
type SeedPolicyDTO struct {
	// UseGlobal, when true, means the per-torrent override is cleared —
	// the torrent reverts to global defaults.
	UseGlobal    bool     `json:"use_global"`
	RatioLimit   *float64 `json:"ratio_limit"`
	TimeMinLimit *int     `json:"time_min_limit"`
}

func (s *Service) GetSeedingDefaults(ctx context.Context) SeedingDefaultsDTO {
	var dto SeedingDefaultsDTO
	if v, err := s.settings.Get(ctx, settingSeedingRatioLimit); err == nil && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			dto.RatioLimit = &f
		}
	}
	if v, err := s.settings.Get(ctx, settingSeedingTimeMiniLimit); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dto.TimeMinLimit = &n
		}
	}
	return dto
}

func (s *Service) SetSeedingDefaults(ctx context.Context, d SeedingDefaultsDTO) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
	ratioStr := ""
	if d.RatioLimit != nil {
		ratioStr = strconv.FormatFloat(*d.RatioLimit, 'f', -1, 64)
	}
	if err := s.settings.Set(ctx, settingSeedingRatioLimit, ratioStr); err != nil {
		return err
	}
	timeStr := ""
	if d.TimeMinLimit != nil {
		timeStr = strconv.Itoa(*d.TimeMinLimit)
	}
	return s.settings.Set(ctx, settingSeedingTimeMiniLimit, timeStr)
}

func (s *Service) GetTorrentSeedPolicy(ctx context.Context, infohash string) (SeedPolicyDTO, error) {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessViewer); err != nil {
		return SeedPolicyDTO{}, err
	}
	rec, err := s.torrents.Get(ctx, infohash)
	if err != nil {
		return SeedPolicyDTO{}, err
	}
	if rec.SeedPolicy == nil {
		return SeedPolicyDTO{UseGlobal: true}, nil
	}
	var p seedPolicyJSON
	if err := jsonUnmarshal([]byte(*rec.SeedPolicy), &p); err != nil {
		return SeedPolicyDTO{UseGlobal: true}, nil
	}
	return SeedPolicyDTO{
		UseGlobal:    false,
		RatioLimit:   p.RatioLimit,
		TimeMinLimit: p.TimeMinLimit,
	}, nil
}

func (s *Service) SetTorrentSeedPolicy(ctx context.Context, infohash string, p SeedPolicyDTO) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if p.UseGlobal {
		return s.torrents.SetSeedPolicy(ctx, infohash, nil)
	}
	raw, err := jsonMarshal(seedPolicyJSON{
		RatioLimit:   p.RatioLimit,
		TimeMinLimit: p.TimeMinLimit,
	})
	if err != nil {
		return fmt.Errorf("marshal seed policy: %w", err)
	}
	str := string(raw)
	return s.torrents.SetSeedPolicy(ctx, infohash, &str)
}

// seedPolicyJSON is the wire/storage shape for a per-torrent seed policy.
type seedPolicyJSON struct {
	RatioLimit   *float64 `json:"ratio_limit"`
	TimeMinLimit *int     `json:"time_min_limit"`
}

// CheckSeedLimits inspects every completed, non-paused torrent and pauses
// any that have exceeded their effective seeding limit (ratio or time).
// It is designed to be called on a periodic ticker (e.g. every 30 seconds).
//
// Each pass also checkpoints the engine's session-scoped transfer counters
// into the persisted total_uploaded/total_downloaded columns (for every
// persisted torrent, not just completed ones), and computes the ratio from
// the persisted total — anacrolix's BytesUp resets each process start, so a
// ratio computed from it restarted at 0 on every launch and ratio limits
// effectively never fired across sessions.
func (s *Service) CheckSeedLimits(ctx context.Context) {
	ctx = WithCaller(ctx, SystemCaller)
	defaults := s.GetSeedingDefaults(ctx)
	snaps := s.engine.List()
	// One bulk SELECT per pass instead of a per-torrent Get every 30s.
	records, err := s.torrents.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("seed policy: list torrents failed")
		return
	}
	byHash := make(map[string]persistence.TorrentRecord, len(records))
	for _, r := range records {
		byHash[r.InfoHash] = r
	}
	now := time.Now()
	live := make(map[string]struct{}, len(snaps))
	for _, snap := range snaps {
		live[string(snap.ID)] = struct{}{}
		rec, ok := byHash[string(snap.ID)]
		if !ok {
			continue
		}
		totalUp := s.checkpointTransferTotals(ctx, &rec, snap)
		if !snap.Completed || snap.Paused {
			continue
		}
		// Determine effective policy: per-torrent override wins over global default.
		var ratioLimit *float64
		var timeMinLimit *int
		if rec.SeedPolicy != nil {
			var p seedPolicyJSON
			if jsonUnmarshal([]byte(*rec.SeedPolicy), &p) == nil {
				ratioLimit = p.RatioLimit
				timeMinLimit = p.TimeMinLimit
			}
		} else {
			ratioLimit = defaults.RatioLimit
			timeMinLimit = defaults.TimeMinLimit
		}
		if ratioLimit == nil && timeMinLimit == nil {
			continue
		}
		// Record seeding start time on first encounter.
		if rec.SeedingStartedAt == nil {
			if err := s.torrents.SetSeedingStartedAt(ctx, string(snap.ID), &now); err != nil {
				log.Warn().Err(err).Str("id", string(snap.ID)).Msg("seed policy: failed to record seeding_started_at")
			}
			rec.SeedingStartedAt = &now
		}
		exceeded := false
		if ratioLimit != nil && snap.BytesDone > 0 {
			ratio := float64(totalUp) / float64(snap.BytesDone)
			if ratio >= *ratioLimit {
				exceeded = true
			}
		}
		if !exceeded && timeMinLimit != nil && rec.SeedingStartedAt != nil {
			elapsed := now.Sub(*rec.SeedingStartedAt)
			if elapsed >= time.Duration(*timeMinLimit)*time.Minute {
				exceeded = true
			}
		}
		if exceeded {
			if err := s.engine.Pause(snap.ID); err != nil {
				log.Warn().Err(err).Str("id", string(snap.ID)).Msg("seed policy: pause failed")
			} else {
				log.Info().Str("id", string(snap.ID)).Str("name", snap.Name).Msg("seed policy: paused torrent — limit reached")
			}
		}
	}
	// Prune observations for torrents no longer in the engine so a removed
	// and re-added infohash starts from fresh counters and the map doesn't
	// grow unbounded across remove cycles.
	s.seedCheckMu.Lock()
	for hash := range s.lastSeedCounters {
		if _, ok := live[hash]; !ok {
			delete(s.lastSeedCounters, hash)
		}
	}
	s.seedCheckMu.Unlock()
}

// checkpointTransferTotals persists the delta between the engine's
// session-scoped transfer counters and the last value this process observed
// for the torrent, returning the updated cumulative uploaded total. A counter
// lower than the last observation means the engine restarted (counters reset
// to zero) — the full current value is the delta. On a write failure the
// observation is rolled back so the delta is retried next pass instead of
// being lost.
func (s *Service) checkpointTransferTotals(ctx context.Context, rec *persistence.TorrentRecord, snap engine.Snapshot) int64 {
	hash := string(snap.ID)
	s.seedCheckMu.Lock()
	last, seen := s.lastSeedCounters[hash]
	upDelta, downDelta := snap.BytesUp-last.up, snap.BytesDown-last.down
	if upDelta < 0 {
		upDelta = snap.BytesUp
	}
	if downDelta < 0 {
		downDelta = snap.BytesDown
	}
	s.lastSeedCounters[hash] = sessionCounters{up: snap.BytesUp, down: snap.BytesDown}
	s.seedCheckMu.Unlock()
	if upDelta == 0 && downDelta == 0 {
		return rec.TotalUploaded
	}
	if err := s.torrents.AddTransferTotals(ctx, hash, upDelta, downDelta); err != nil {
		log.Warn().Err(err).Str("id", hash).Msg("seed policy: persist transfer totals failed")
		s.seedCheckMu.Lock()
		if seen {
			s.lastSeedCounters[hash] = last
		} else {
			delete(s.lastSeedCounters, hash)
		}
		s.seedCheckMu.Unlock()
		return rec.TotalUploaded
	}
	rec.TotalUploaded += upDelta
	rec.TotalDownloaded += downDelta
	return rec.TotalUploaded
}