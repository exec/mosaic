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

// TestFeeds_LastPolledZeroRoundTrips: a feed created without a LastPolled must
// store 0 (not the bogus -62135596800 from the zero time's Unix()) and read
// back as the zero time.Time{} so IsZero() is true.
func TestFeeds_LastPolledZeroRoundTrips(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	id, err := f.Create(ctx, Feed{URL: "u", Name: "n", IntervalMin: 30, Enabled: true})
	require.NoError(t, err)

	// The stored column must be exactly 0.
	var stored int64
	require.NoError(t, db.SQL().QueryRowContext(ctx,
		`SELECT last_polled FROM rss_feeds WHERE id = ?`, id).Scan(&stored))
	require.Equal(t, int64(0), stored)

	got, err := f.Get(ctx, id)
	require.NoError(t, err)
	require.True(t, got.LastPolled.IsZero(), "LastPolled should round-trip to the zero time")

	rows, err := f.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].LastPolled.IsZero(), "LastPolled from List should be the zero time")
}

// TestFeeds_UpdateFloorsInterval: Update must floor a non-positive interval to
// 30, matching Create's behavior.
func TestFeeds_UpdateFloorsInterval(t *testing.T) {
	db := newTestDB(t)
	f := NewFeeds(db)
	ctx := context.Background()

	id, err := f.Create(ctx, Feed{URL: "u", Name: "n", IntervalMin: 60, Enabled: true})
	require.NoError(t, err)

	require.NoError(t, f.Update(ctx, Feed{ID: id, URL: "u", Name: "n", IntervalMin: 0, Enabled: true}))

	got, err := f.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 30, got.IntervalMin, "Update should floor interval 0 to 30")
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
