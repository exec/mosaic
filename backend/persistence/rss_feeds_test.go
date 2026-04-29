package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFeeds_CreateGet(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	id, err := f.Create(ctx, Feed{
		URL:         "https://example.com/rss.xml",
		Name:        "Example",
		IntervalMin: 15,
		ETag:        "abc",
		Enabled:     true,
	})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := f.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/rss.xml", got.URL)
	require.Equal(t, "Example", got.Name)
	require.Equal(t, 15, got.IntervalMin)
	require.Equal(t, "abc", got.ETag)
	require.True(t, got.Enabled)
}

func TestFeeds_CreateDefaultsInterval(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	id, err := f.Create(ctx, Feed{URL: "u", Name: "n", Enabled: true})
	require.NoError(t, err)
	got, err := f.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 30, got.IntervalMin)
}

func TestFeeds_List(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	_, _ = f.Create(ctx, Feed{URL: "u1", Name: "Bravo", IntervalMin: 30, Enabled: true})
	_, _ = f.Create(ctx, Feed{URL: "u2", Name: "Alpha", IntervalMin: 30, Enabled: true})

	rows, err := f.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "Alpha", rows[0].Name)
	require.Equal(t, "Bravo", rows[1].Name)
}

func TestFeeds_Update(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	id, _ := f.Create(ctx, Feed{URL: "u", Name: "Old", IntervalMin: 30, Enabled: true})
	require.NoError(t, f.Update(ctx, Feed{ID: id, URL: "u2", Name: "New", IntervalMin: 60, Enabled: false}))

	got, _ := f.Get(ctx, id)
	require.Equal(t, "u2", got.URL)
	require.Equal(t, "New", got.Name)
	require.Equal(t, 60, got.IntervalMin)
	require.False(t, got.Enabled)
}

func TestFeeds_Delete(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	id, _ := f.Create(ctx, Feed{URL: "u", Name: "Tmp", IntervalMin: 30, Enabled: true})
	require.NoError(t, f.Delete(ctx, id))
	_, err := f.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFeeds_UpdatePollResult(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	id, _ := f.Create(ctx, Feed{URL: "u", Name: "n", IntervalMin: 30, Enabled: true})
	when := time.Unix(1700000000, 0)
	require.NoError(t, f.UpdatePollResult(ctx, id, when, "etag-xyz"))

	got, err := f.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, when.Unix(), got.LastPolled.Unix())
	require.Equal(t, "etag-xyz", got.ETag)
}
