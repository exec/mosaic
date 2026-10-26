package persistence

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestTransferDailyRetentionAndRemoval(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()
	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "daily", Name: "daily", SavePath: "/tmp", AddedAt: time.Now()}))
	require.NoError(t, tr.AddTransferTotals(ctx, "daily", 12, 34))
	require.NoError(t, tr.AddTransferTotals(ctx, "daily", 3, 4))
	require.Error(t, tr.AddTransferTotals(ctx, "daily", -1, 0))
	old := time.Now().UTC().AddDate(0, 0, -90).Format("2006-01-02")
	_, err := db.SQL().Exec(`INSERT INTO transfer_daily(infohash,day,uploaded,downloaded) VALUES('daily',?,99,99)`, old)
	require.NoError(t, err)
	days, err := tr.TransferDays(ctx, 0, true)
	require.NoError(t, err)
	require.Len(t, days, 1)
	require.Equal(t, int64(15), days[0].Uploaded)
	require.Equal(t, int64(38), days[0].Downloaded)
	require.NoError(t, tr.PruneTransferDays(ctx))
	var count int
	require.NoError(t, db.SQL().QueryRow(`SELECT COUNT(*) FROM transfer_daily`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, tr.Remove(ctx, "daily"))
	require.NoError(t, db.SQL().QueryRow(`SELECT COUNT(*) FROM transfer_daily`).Scan(&count))
	require.Zero(t, count)
}
