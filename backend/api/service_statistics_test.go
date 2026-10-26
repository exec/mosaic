package api

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

func TestTransferStatisticsCheckpointResetAndRestart(t *testing.T) {
	s, fb, db := newTestServiceWithDB(t)
	ctx := sysCtx()
	id, err := s.AddMagnet(ctx, "magnet:?xt=urn:btih:statistics", "")
	require.NoError(t, err)
	require.NoError(t, s.torrents.AddTransferTotals(ctx, string(id), 200, 400))
	fb.SetSessionStats(engine.TorrentID(id), 100, 50, false)
	got, err := s.GetTransferStatistics(ctx)
	require.NoError(t, err)
	require.Equal(t, TransferTotalsDTO{Uploaded: 50, Downloaded: 100}, got.Session)
	require.Equal(t, TransferTotalsDTO{Uploaded: 250, Downloaded: 500}, got.Lifetime)
	require.Equal(t, got.Lifetime, got.Today)
	again, err := s.GetTransferStatistics(ctx)
	require.NoError(t, err)
	require.Equal(t, got, again)
	fb.SetSessionStats(engine.TorrentID(id), 20, 10, false)
	got, err = s.GetTransferStatistics(ctx)
	require.NoError(t, err)
	require.Equal(t, TransferTotalsDTO{Uploaded: 60, Downloaded: 120}, got.Session)
	require.Equal(t, int64(260), got.Lifetime.Uploaded)
	rows, err := s.ListTorrents(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(260), rows[0].TotalUp)
	detail, err := s.GetTorrentDetail(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, rows[0].TotalUp, detail.TotalUp)
	// A new service process starts a fresh session while preserving library totals.
	fresh := NewService(s.engine, s.torrents, s.categories, s.tags, s.settings, s.scheduleRules, s.feeds, s.filters, s.users, s.access, s.trackers, nil, t.TempDir())
	fb.SetSessionStats(engine.TorrentID(id), 0, 0, false)
	got, err = fresh.GetTransferStatistics(ctx)
	require.NoError(t, err)
	require.Equal(t, TransferTotalsDTO{}, got.Session)
	require.Equal(t, int64(260), got.Lifetime.Uploaded)
	// Failed daily writes roll back cumulative totals and can be retried once.
	_, err = db.SQL().Exec(`CREATE TRIGGER fail_daily BEFORE INSERT ON transfer_daily BEGIN SELECT RAISE(ABORT,'disk full'); END`)
	require.NoError(t, err)
	fb.SetSessionStats(engine.TorrentID(id), 30, 15, false)
	_, err = fresh.GetTransferStatistics(ctx)
	require.Error(t, err)
	rec, err := s.torrents.Get(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, int64(260), rec.TotalUploaded)
	_, err = db.SQL().Exec(`DROP TRIGGER fail_daily`)
	require.NoError(t, err)
	got, err = fresh.GetTransferStatistics(ctx)
	require.NoError(t, err)
	require.Equal(t, TransferTotalsDTO{Uploaded: 15, Downloaded: 30}, got.Session)
	require.Equal(t, int64(275), got.Lifetime.Uploaded)
}

func TestTransferStatisticsRespectAccess(t *testing.T) {
	s, fb := newTestService(t)
	ctx := sysCtx()
	a, err := s.AddMagnet(ctx, "magnet:?xt=urn:btih:visible", "")
	require.NoError(t, err)
	b, err := s.AddMagnet(ctx, "magnet:?xt=urn:btih:hidden", "")
	require.NoError(t, err)
	fb.SetSessionStats(engine.TorrentID(a), 100, 50, false)
	fb.SetSessionStats(engine.TorrentID(b), 900, 450, false)
	_, err = s.GetTransferStatistics(ctx)
	require.NoError(t, err)
	user := mkUser(t, s, UserInput{Username: "statistics", Password: "password123", Role: persistence.RoleUser})
	require.NoError(t, s.access.Grant(ctx, string(a), user.ID, persistence.AccessViewer, nil))
	got, err := s.GetTransferStatistics(asUser(t, s, user.ID))
	require.NoError(t, err)
	require.Equal(t, TransferTotalsDTO{Uploaded: 50, Downloaded: 100}, got.Lifetime)
	require.Equal(t, got.Lifetime, got.Today)
	require.Len(t, got.Days, 1)
	require.Equal(t, int64(50), got.Days[0].Uploaded)
	_, err = s.GetTransferStatistics(context.Background())
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestSeedPolicyBeforeCompletionAndEffectiveLimits(t *testing.T) {
	s, _ := newTestService(t)
	ctx := sysCtx()
	id, err := s.AddMagnet(ctx, "magnet:?xt=urn:btih:policy", "")
	require.NoError(t, err)
	ratio, minutes := 2.0, 120
	require.NoError(t, s.SetSeedingDefaults(ctx, SeedingDefaultsDTO{RatioLimit: &ratio, TimeMinLimit: &minutes}))
	got, err := s.GetTorrentSeedPolicy(ctx, string(id))
	require.NoError(t, err)
	require.True(t, got.UseGlobal)
	require.Equal(t, &ratio, got.EffectiveRatioLimit)
	require.Equal(t, &minutes, got.EffectiveTimeMinLimit)
	custom := 3.0
	require.NoError(t, s.SetTorrentSeedPolicy(ctx, string(id), SeedPolicyDTO{RatioLimit: &custom}))
	got, err = s.GetTorrentSeedPolicy(ctx, string(id))
	require.NoError(t, err)
	require.False(t, got.UseGlobal)
	require.Equal(t, &custom, got.EffectiveRatioLimit)
	require.Nil(t, got.EffectiveTimeMinLimit)
	require.NoError(t, s.SetTorrentSeedPolicy(ctx, string(id), SeedPolicyDTO{}))
	got, err = s.GetTorrentSeedPolicy(ctx, string(id))
	require.NoError(t, err)
	require.False(t, got.UseGlobal)
	require.Nil(t, got.EffectiveRatioLimit)
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1)} {
		require.Error(t, s.SetTorrentSeedPolicy(ctx, string(id), SeedPolicyDTO{RatioLimit: &bad}))
		require.Error(t, s.SetSeedingDefaults(ctx, SeedingDefaultsDTO{RatioLimit: &bad}))
	}
	zero := 0
	require.Error(t, s.SetTorrentSeedPolicy(ctx, string(id), SeedPolicyDTO{TimeMinLimit: &zero}))
}

func TestTransferTickRetainsCountersWhenCheckpointRunsBeforeFanout(t *testing.T) {
	s, fb := newTestService(t)
	ctx := sysCtx()
	id, err := s.AddMagnet(ctx, "magnet:?xt=urn:btih:tick-counters", "")
	require.NoError(t, err)
	require.NoError(t, s.torrents.AddTransferTotals(ctx, string(id), 200, 400))
	fb.SetSessionStats(id, 100, 50, false)
	tick, err := s.BuildTorrentTickSnapshot(ctx)
	require.NoError(t, err)
	_, err = s.GetTransferStatistics(ctx)
	require.NoError(t, err)
	rows, err := s.ListTorrentsFromSnapshot(ctx, tick)
	require.NoError(t, err)
	require.Equal(t, int64(250), rows[0].TotalUp)
	require.Equal(t, int64(500), rows[0].TotalDown)
}
