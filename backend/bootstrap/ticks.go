package bootstrap

import (
	"crypto/sha256"
	"time"

	"context"

	"github.com/rs/zerolog/log"

	"mosaic/backend/api"
	"mosaic/backend/remote"
)

// StreamTicks polls the service at regular intervals and pushes state
// snapshots to connected WebSocket clients. It is the canonical tick loop
// shared by both the desktop and daemon entry points.
//
// Ticks are published per connected user so one user never receives another's
// torrents, stats, or inspector data. Admin users share a single encoded frame
// (computed once per tick) to avoid redundant JSON marshaling; non-admin users
// receive an access-filtered reduction of the same engine snapshot.
//
// A user's torrents frame is skipped when its SHA-256 fingerprint is identical
// to the last frame sent to that user — a paused, fully-seeding library
// produces zero traffic. Fingerprints for users who disconnect are pruned each
// tick so a reconnecting client always gets a fresh first frame.
//
// Desktop entry point: call this in a goroutine alongside streamWailsEvents
// (which handles the embedded-SPA Wails event emission separately).
// Daemon entry point: call this as the sole tick goroutine.
func StreamTicks(ctx context.Context, svc *api.Service, hub *remote.Hub) {
	torrents := time.NewTicker(1 * time.Second)
	stats := time.NewTicker(1 * time.Second)
	inspector := time.NewTicker(1 * time.Second)
	defer torrents.Stop()
	defer stats.Stop()
	defer inspector.Stop()

	lastTorrentsFrame := make(map[int][32]byte)

	for {
		select {
		case <-ctx.Done():
			return

		case <-torrents.C:
			uids := hub.ConnectedUserIDs()
			if len(uids) == 0 {
				if len(lastTorrentsFrame) > 0 {
					lastTorrentsFrame = make(map[int][32]byte)
				}
				continue
			}
			tick, err := svc.BuildTorrentTickSnapshot(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("StreamTicks: torrents snapshot failed; clients will see stale state this tick")
				continue
			}
			var adminFrame []byte
			adminComputed := false
			seen := make(map[int]struct{}, len(uids))
			for _, uid := range uids {
				seen[uid] = struct{}{}
				caller, err := svc.CallerForUserID(ctx, uid)
				if err != nil {
					log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: caller lookup failed; skipping torrents frame")
					continue
				}
				var frame []byte
				if caller.SeesAllTorrents() {
					if !adminComputed {
						rows, err := svc.ListTorrentsFromSnapshot(api.WithCaller(ctx, caller), tick)
						if err != nil {
							log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: admin torrents-list failed")
							continue
						}
						adminFrame = remote.EncodeTorrentsFrame(rows)
						adminComputed = true
					}
					frame = adminFrame
				} else {
					rows, err := svc.ListTorrentsFromSnapshot(api.WithCaller(ctx, caller), tick)
					if err != nil {
						log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: per-user torrents-list failed")
						continue
					}
					frame = remote.EncodeTorrentsFrame(rows)
				}
				if frame == nil {
					continue
				}
				fp := sha256.Sum256(frame)
				if prev, ok := lastTorrentsFrame[uid]; ok && prev == fp {
					continue
				}
				lastTorrentsFrame[uid] = fp
				hub.PublishTorrentsRawTo(uid, frame)
			}
			for uid := range lastTorrentsFrame {
				if _, ok := seen[uid]; !ok {
					delete(lastTorrentsFrame, uid)
				}
			}

		case <-stats.C:
			uids := hub.ConnectedUserIDs()
			if len(uids) == 0 {
				continue
			}
			snaps := svc.EngineSnapshots()
			var adminStats api.GlobalStats
			adminComputed := false
			for _, uid := range uids {
				caller, err := svc.CallerForUserID(ctx, uid)
				if err != nil {
					log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: caller lookup failed; skipping stats frame")
					continue
				}
				if caller.SeesAllTorrents() {
					if !adminComputed {
						adminStats, err = svc.GlobalStatsFromSnapshot(api.WithCaller(ctx, caller), snaps)
						if err != nil {
							log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: admin stats failed")
							continue
						}
						adminComputed = true
					}
					hub.PublishStatsTo(uid, adminStats)
					continue
				}
				st, err := svc.GlobalStatsFromSnapshot(api.WithCaller(ctx, caller), snaps)
				if err != nil {
					log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: per-user stats failed")
					continue
				}
				hub.PublishStatsTo(uid, st)
			}

		case <-inspector.C:
			for _, uid := range hub.ConnectedUserIDs() {
				uctx, err := callerCtx(ctx, svc, uid)
				if err != nil {
					log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: caller lookup failed; skipping inspector frame")
					continue
				}
				detail, err := svc.DetailForFocus(uctx)
				if err != nil {
					log.Warn().Err(err).Int("user_id", uid).Msg("StreamTicks: inspector detail failed")
					continue
				}
				if detail != nil {
					hub.PublishInspectorTo(uid, *detail)
				}
			}
		}
	}
}

func callerCtx(ctx context.Context, svc *api.Service, userID int) (context.Context, error) {
	caller, err := svc.CallerForUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return api.WithCaller(ctx, caller), nil
}
