package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCategories_CreateGet(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, err := c.Create(ctx, Category{Name: "Movies", DefaultSavePath: "/Volumes/media", Color: "#ef4444"})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := c.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "Movies", got.Name)
	require.Equal(t, "/Volumes/media", got.DefaultSavePath)
	require.Equal(t, "#ef4444", got.Color)
}

func TestCategories_List(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	_, _ = c.Create(ctx, Category{Name: "Movies"})
	_, _ = c.Create(ctx, Category{Name: "Software"})

	rows, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestCategories_Update(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, _ := c.Create(ctx, Category{Name: "Movies"})
	require.NoError(t, c.Update(ctx, Category{ID: id, Name: "Cinema", Color: "#000000"}))

	got, _ := c.Get(ctx, id)
	require.Equal(t, "Cinema", got.Name)
	require.Equal(t, "#000000", got.Color)
}

func TestCategories_Delete(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, _ := c.Create(ctx, Category{Name: "Tmp"})
	require.NoError(t, c.Delete(ctx, id))
	_, err := c.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}

// TestCategories_DeleteInUse covers deleting a category still assigned to a
// torrent: torrents.category_id has no ON DELETE action and foreign_keys is
// on, so Delete must detach the torrents (category_id → NULL) in the same
// transaction instead of failing with an FK violation.
func TestCategories_DeleteInUse(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	tor := NewTorrents(db)
	ctx := context.Background()

	id, err := c.Create(ctx, Category{Name: "Movies"})
	require.NoError(t, err)
	require.NoError(t, tor.Save(ctx, TorrentRecord{
		InfoHash: "aaa", Name: "film", SavePath: "/tmp", CategoryID: &id, AddedAt: time.Now(),
	}))

	require.NoError(t, c.Delete(ctx, id))
	_, err = c.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)

	got, err := tor.Get(ctx, "aaa")
	require.NoError(t, err)
	require.Nil(t, got.CategoryID, "torrent must be detached from the deleted category")
}

func TestCategories_NameUnique(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()
	_, err := c.Create(ctx, Category{Name: "Movies"})
	require.NoError(t, err)
	_, err = c.Create(ctx, Category{Name: "Movies"})
	require.Error(t, err)
}
