package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedTorrent inserts a parent torrents row so the verify_snapshots FK is
// satisfied. Returns the infohash for chaining into the DAO call.
func seedTorrent(t *testing.T, db *DB, infohash string) string {
	t.Helper()
	tor := NewTorrents(db)
	require.NoError(t, tor.Save(context.Background(), TorrentRecord{
		InfoHash: infohash,
		Name:     "n",
		SavePath: "/p",
		AddedAt:  time.Unix(1700000000, 0),
	}))
	return infohash
}

func TestVerifySnapshots_GetMissing(t *testing.T) {
	db := newTestDB(t)
	vs := NewVerifySnapshots(db)
	ctx := context.Background()

	snap, complete, ok, err := vs.Get(ctx, "deadbeef")
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, complete)
	require.Nil(t, snap)
}

func TestVerifySnapshots_UpsertAndGet(t *testing.T) {
	db := newTestDB(t)
	vs := NewVerifySnapshots(db)
	ctx := context.Background()
	seedTorrent(t, db, "abc123")

	payload := []byte{0x01, 0x02, 0x03, 0x04}
	require.NoError(t, vs.Upsert(ctx, "abc123", payload, true))

	got, complete, ok, err := vs.Get(ctx, "abc123")
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, complete)
	require.Equal(t, payload, got)

	// Overwrite with new payload + wasComplete=false.
	payload2 := []byte{0x09, 0x08, 0x07}
	require.NoError(t, vs.Upsert(ctx, "abc123", payload2, false))

	got, complete, ok, err = vs.Get(ctx, "abc123")
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, complete)
	require.Equal(t, payload2, got)
}

func TestVerifySnapshots_Delete(t *testing.T) {
	db := newTestDB(t)
	vs := NewVerifySnapshots(db)
	ctx := context.Background()
	seedTorrent(t, db, "abc123")

	require.NoError(t, vs.Upsert(ctx, "abc123", []byte("x"), true))
	require.NoError(t, vs.Delete(ctx, "abc123"))

	_, _, ok, err := vs.Get(ctx, "abc123")
	require.NoError(t, err)
	require.False(t, ok)

	// Delete on missing row is a no-op.
	require.NoError(t, vs.Delete(ctx, "abc123"))
}

func TestVerifySnapshots_CascadesOnTorrentRemove(t *testing.T) {
	db := newTestDB(t)
	vs := NewVerifySnapshots(db)
	tor := NewTorrents(db)
	ctx := context.Background()
	seedTorrent(t, db, "abc123")

	require.NoError(t, vs.Upsert(ctx, "abc123", []byte("x"), true))

	// Removing the parent torrent should cascade and drop the snapshot.
	require.NoError(t, tor.Remove(ctx, "abc123"))

	_, _, ok, err := vs.Get(ctx, "abc123")
	require.NoError(t, err)
	require.False(t, ok, "snapshot row must be cascade-deleted with parent")
}
