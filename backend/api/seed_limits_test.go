package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// floatPtr is a tiny literal helper for SeedingDefaultsDTO.RatioLimit.
func floatPtr(f float64) *float64 { return &f }

// TestCheckSeedLimits_RatioPausesWhenExceeded covers the basic in-session
// path: a completed torrent whose cumulative upload crosses the ratio limit
// gets paused; one below it does not.
func TestCheckSeedLimits_RatioPausesWhenExceeded(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()

	require.NoError(t, svc.SetSeedingDefaults(ctx, SeedingDefaultsDTO{RatioLimit: floatPtr(1.0)}))

	id, err := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:seedratio", "")
	require.NoError(t, err)

	// Completed, uploaded 2x the payload (TotalBytes is 1 GiB in the fake).
	fb.SetSessionStats(id, 0, 2<<30, true)
	svc.CheckSeedLimits(ctx)

	snap, err := fb.Snapshot(id)
	require.NoError(t, err)
	require.True(t, snap.Paused, "ratio 2.0 >= limit 1.0 must pause the torrent")

	// Totals were checkpointed to the DB.
	rec, err := svc.torrents.Get(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, int64(2<<30), rec.TotalUploaded)
}

// TestCheckSeedLimits_RatioBelowLimitDoesNotPause is the negative control.
func TestCheckSeedLimits_RatioBelowLimitDoesNotPause(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()

	require.NoError(t, svc.SetSeedingDefaults(ctx, SeedingDefaultsDTO{RatioLimit: floatPtr(1.0)}))

	id, err := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:seedlow", "")
	require.NoError(t, err)

	fb.SetSessionStats(id, 0, 1<<29, true) // ratio 0.5
	svc.CheckSeedLimits(ctx)

	snap, err := fb.Snapshot(id)
	require.NoError(t, err)
	require.False(t, snap.Paused)
}

// TestCheckSeedLimits_RatioSurvivesCounterReset covers the cross-session bug:
// anacrolix's BytesUp counter resets every process start, so a ratio computed
// from it restarted at 0 each launch. The persisted totals must accumulate
// across a counter reset (observed as the session counter dropping), so the
// combined upload trips the limit even though no single session does.
func TestCheckSeedLimits_RatioSurvivesCounterReset(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()

	require.NoError(t, svc.SetSeedingDefaults(ctx, SeedingDefaultsDTO{RatioLimit: floatPtr(1.0)}))

	id, err := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:seedreset", "")
	require.NoError(t, err)

	// Session 1: uploaded 0.75x — below the limit.
	fb.SetSessionStats(id, 0, 3<<28, true)
	svc.CheckSeedLimits(ctx)
	snap, err := fb.Snapshot(id)
	require.NoError(t, err)
	require.False(t, snap.Paused, "0.75 ratio must not pause")

	// "Restart": the session counter drops to a lower value (0.5x this
	// session). Session-scoped math would see ratio 0.5 and never pause;
	// the persisted totals make it 0.75 + 0.5 = 1.25 >= 1.0.
	fb.SetSessionStats(id, 0, 1<<29, true)
	svc.CheckSeedLimits(ctx)

	rec, err := svc.torrents.Get(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, int64(3<<28+1<<29), rec.TotalUploaded)

	snap, err = fb.Snapshot(id)
	require.NoError(t, err)
	require.True(t, snap.Paused, "cumulative ratio 1.25 >= limit 1.0 must pause")
}

// TestCheckSeedLimits_CheckpointsIncompleteTorrents ensures the transfer
// checkpoint runs for every persisted torrent, not just completed ones —
// upload during the download phase must count toward the eventual ratio.
func TestCheckSeedLimits_CheckpointsIncompleteTorrents(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()

	id, err := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:seedpartial", "")
	require.NoError(t, err)

	fb.SetSessionStats(id, 1<<20, 1<<19, false)
	svc.CheckSeedLimits(ctx)
	fb.SetSessionStats(id, 3<<20, 1<<20, false)
	svc.CheckSeedLimits(ctx)

	rec, err := svc.torrents.Get(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, int64(1<<20), rec.TotalUploaded)
	require.Equal(t, int64(3<<20), rec.TotalDownloaded)

	snap, err := fb.Snapshot(id)
	require.NoError(t, err)
	require.False(t, snap.Paused, "incomplete torrents are never paused by seed limits")
}
