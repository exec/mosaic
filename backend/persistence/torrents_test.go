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
