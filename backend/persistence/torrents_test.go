package persistence

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTorrents_SaveAndGet(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()

	rec := TorrentRecord{
		InfoHash: "abc123",
		Name:     "ubuntu-24.04.iso",
		Magnet:   "magnet:?xt=urn:btih:abc123",
		SavePath: "/tmp/dl",
		AddedAt:  time.Unix(1700000000, 0),
	}
	require.NoError(t, tr.Save(ctx, rec))

	got, err := tr.Get(ctx, "abc123")
	require.NoError(t, err)
	require.Equal(t, rec.Name, got.Name)
	require.Equal(t, rec.SavePath, got.SavePath)
	require.Equal(t, rec.AddedAt.Unix(), got.AddedAt.Unix())
}

func TestTorrents_List_ReturnsAll(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()

	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "h1", Name: "a", SavePath: "/p", AddedAt: time.Now()}))
	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "h2", Name: "b", SavePath: "/p", AddedAt: time.Now()}))

	all, err := tr.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestTorrents_Remove(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()

	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "h1", Name: "a", SavePath: "/p", AddedAt: time.Now()}))
	require.NoError(t, tr.Remove(ctx, "h1"))

	all, err := tr.List(ctx)
	require.NoError(t, err)
	require.Empty(t, all)
}

func TestTorrents_QueueAndForceStart(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()
	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "q1", Name: "n", SavePath: "/p", AddedAt: time.Now()}))

	// Default values
	got, _ := tr.Get(ctx, "q1")
	require.Equal(t, 0, got.QueuePosition)
	require.False(t, got.ForceStart)

	require.NoError(t, tr.SetQueuePosition(ctx, "q1", 5))
	require.NoError(t, tr.SetForceStart(ctx, "q1", true))

	got, _ = tr.Get(ctx, "q1")
	require.Equal(t, 5, got.QueuePosition)
	require.True(t, got.ForceStart)
}

func TestTorrents_SetPausedRoundtrip(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()
	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "p1", Name: "n", SavePath: "/p", AddedAt: time.Now()}))

	got, _ := tr.Get(ctx, "p1")
	require.False(t, got.Paused)

	require.NoError(t, tr.SetPaused(ctx, "p1", true))
	got, _ = tr.Get(ctx, "p1")
	require.True(t, got.Paused)

	require.NoError(t, tr.SetPaused(ctx, "p1", false))
	got, _ = tr.Get(ctx, "p1")
	require.False(t, got.Paused)
}

func TestTorrents_SetCompletedAt(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()
	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "c1", Name: "n", SavePath: "/p", AddedAt: time.Now()}))

	got, _ := tr.Get(ctx, "c1")
	require.Nil(t, got.CompletedAt)

	at := time.Unix(1700000123, 0)
	require.NoError(t, tr.SetCompletedAt(ctx, "c1", at))
	got, _ = tr.Get(ctx, "c1")
	require.NotNil(t, got.CompletedAt)
	require.Equal(t, at.Unix(), got.CompletedAt.Unix())
}

// TestTorrents_SaveDuplicateKeepsUserState pins the non-destructive upsert:
// re-adding an already-known torrent (RSS re-match, watch-folder re-scan)
// must refresh metadata only, never reset user state set via the dedicated
// Set* methods.
func TestTorrents_SaveDuplicateKeepsUserState(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	cats := NewCategories(db)
	ctx := context.Background()

	addedAt := time.Unix(1700000000, 0)
	require.NoError(t, tr.Save(ctx, TorrentRecord{
		InfoHash: "d1", Name: "n", Magnet: "magnet:?xt=urn:btih:d1", SavePath: "/p", AddedAt: addedAt,
	}))

	catID, _ := cats.Create(ctx, Category{Name: "Movies"})
	completedAt := time.Unix(1700001000, 0)
	require.NoError(t, tr.SetPaused(ctx, "d1", true))
	require.NoError(t, tr.SetQueuePosition(ctx, "d1", 3))
	require.NoError(t, tr.SetForceStart(ctx, "d1", true))
	require.NoError(t, tr.SetSequential(ctx, "d1", true))
	require.NoError(t, tr.SetCategory(ctx, "d1", &catID))
	require.NoError(t, tr.SetRateLimits(ctx, "d1", 1000, 2000))
	require.NoError(t, tr.SetCompletedAt(ctx, "d1", completedAt))

	// Duplicate add: fresh AddedAt, zeroed user state, empty magnet (file add).
	require.NoError(t, tr.Save(ctx, TorrentRecord{
		InfoHash: "d1", Name: "renamed", SavePath: "/other", AddedAt: time.Now(),
	}))

	got, err := tr.Get(ctx, "d1")
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name, "name is metadata and should refresh")
	require.Equal(t, "magnet:?xt=urn:btih:d1", got.Magnet, "empty magnet must not wipe the stored one")
	require.Equal(t, "/p", got.SavePath, "save_path must not change on re-add")
	require.Equal(t, addedAt.Unix(), got.AddedAt.Unix())
	require.True(t, got.Paused)
	require.Equal(t, 3, got.QueuePosition)
	require.True(t, got.ForceStart)
	require.True(t, got.Sequential)
	require.NotNil(t, got.CategoryID)
	require.Equal(t, catID, *got.CategoryID)
	require.Equal(t, int64(1000), got.DownRateLimit)
	require.Equal(t, int64(2000), got.UpRateLimit)
	require.NotNil(t, got.CompletedAt)
	require.Equal(t, completedAt.Unix(), got.CompletedAt.Unix())
}

func TestTorrents_CategoryAssignment(t *testing.T) {
	db := newTestDB(t)
	tor := NewTorrents(db)
	cats := NewCategories(db)
	ctx := context.Background()

	catID, _ := cats.Create(ctx, Category{Name: "Movies"})

	require.NoError(t, tor.Save(ctx, TorrentRecord{
		InfoHash: "h1", Name: "n", SavePath: "/p", AddedAt: time.Now(),
	}))
	require.NoError(t, tor.SetCategory(ctx, "h1", &catID))

	got, _ := tor.Get(ctx, "h1")
	require.NotNil(t, got.CategoryID)
	require.Equal(t, catID, *got.CategoryID)

	require.NoError(t, tor.SetCategory(ctx, "h1", nil))
	got, _ = tor.Get(ctx, "h1")
	require.Nil(t, got.CategoryID)
}
