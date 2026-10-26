package persistence

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestUpgradeFrom181RetainsLibraryAndEnablesNewTables(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mosaic.db")
	db, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, goose.DownToContext(ctx, db.SQL(), "migrations", 16))
	torrents := NewTorrents(db)
	original := TorrentRecord{InfoHash: "existing", Name: "Existing library item", SavePath: "/downloads/existing", Magnet: "magnet:?xt=urn:btih:existing", AddedAt: time.Now().Truncate(time.Second), Paused: true, QueuePosition: 3, Sequential: true, TotalUploaded: 150, TotalDownloaded: 300}
	require.NoError(t, torrents.Save(ctx, original))
	_, err = db.SQL().ExecContext(ctx, `UPDATE torrents SET total_uploaded=?,total_downloaded=? WHERE infohash=?`, original.TotalUploaded, original.TotalDownloaded, original.InfoHash)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	db, err = Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	torrents = NewTorrents(db)
	got, err := torrents.Get(ctx, original.InfoHash)
	require.NoError(t, err)
	require.Equal(t, original, got)
	require.NoError(t, torrents.SetFilePriorities(ctx, original.InfoHash, map[int]int{0: 0, 1: 2}))
	priorities, err := torrents.FilePriorities(ctx, original.InfoHash)
	require.NoError(t, err)
	require.Equal(t, map[int]int{0: 0, 1: 2}, priorities)
	require.NoError(t, torrents.AddTransferTotals(ctx, original.InfoHash, 25, 50))
	got, err = torrents.Get(ctx, original.InfoHash)
	require.NoError(t, err)
	require.Equal(t, int64(175), got.TotalUploaded)
	require.Equal(t, int64(350), got.TotalDownloaded)
	days, err := torrents.TransferDays(ctx, 0, true)
	require.NoError(t, err)
	require.Len(t, days, 1)
	require.Equal(t, int64(25), days[0].Uploaded)
}
