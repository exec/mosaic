package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTags_CreateListUpdateDelete(t *testing.T) {
	db := newTestDB(t)
	tg := NewTags(db)
	ctx := context.Background()

	id, err := tg.Create(ctx, Tag{Name: "#archive", Color: "#3b82f6"})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := tg.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "#archive", got.Name)

	require.NoError(t, tg.Update(ctx, Tag{ID: id, Name: "#archived", Color: "#000000"}))
	got, _ = tg.Get(ctx, id)
	require.Equal(t, "#archived", got.Name)

	require.NoError(t, tg.Delete(ctx, id))
	_, err = tg.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestTags_AssocWithTorrent(t *testing.T) {
	db := newTestDB(t)
	tor := NewTorrents(db)
	tg := NewTags(db)
	ctx := context.Background()

	require.NoError(t, tor.Save(ctx, TorrentRecord{
		InfoHash: "abc123", Name: "x", SavePath: "/tmp", AddedAt: time.Now(),
	}))
	tagID, _ := tg.Create(ctx, Tag{Name: "#priority"})

	require.NoError(t, tg.Assign(ctx, "abc123", tagID))

	tags, err := tg.ForTorrent(ctx, "abc123")
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, "#priority", tags[0].Name)

	require.NoError(t, tg.Unassign(ctx, "abc123", tagID))
	tags, _ = tg.ForTorrent(ctx, "abc123")
	require.Empty(t, tags)
}
