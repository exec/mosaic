package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newFeedForTest(t *testing.T, db *DB) int {
	t.Helper()
	id, err := NewFeeds(db).Create(context.Background(), Feed{
		URL: "u", Name: "f", IntervalMin: 30, Enabled: true,
	})
	require.NoError(t, err)
	return id
}

func TestFilters_CreateGet(t *testing.T) {
	db := newTestDB(t)
	feedID := newFeedForTest(t, db)
	catID, err := NewCategories(db).Create(context.Background(), Category{Name: "Linux ISOs"})
	require.NoError(t, err)
	fl := NewFilters(db)
	ctx := context.Background()

	id, err := fl.Create(ctx, Filter{
		FeedID:     feedID,
		Regex:      `^Ubuntu.*amd64\.iso$`,
		CategoryID: &catID,
		SavePath:   "/tmp/iso",
		Enabled:    true,
	})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := fl.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, feedID, got.FeedID)
	require.Equal(t, `^Ubuntu.*amd64\.iso$`, got.Regex)
	require.NotNil(t, got.CategoryID)
	require.Equal(t, catID, *got.CategoryID)
	require.Equal(t, "/tmp/iso", got.SavePath)
	require.True(t, got.Enabled)
}

func TestFilters_NullableFields(t *testing.T) {
	db := newTestDB(t)
	feedID := newFeedForTest(t, db)
	fl := NewFilters(db)
	ctx := context.Background()

	id, err := fl.Create(ctx, Filter{FeedID: feedID, Regex: ".*", Enabled: true})
	require.NoError(t, err)

	got, err := fl.Get(ctx, id)
	require.NoError(t, err)
	require.Nil(t, got.CategoryID)
	require.Equal(t, "", got.SavePath)
}

func TestFilters_ListByFeed(t *testing.T) {
	db := newTestDB(t)
	feedA := newFeedForTest(t, db)
	feedB, err := NewFeeds(db).Create(context.Background(), Feed{URL: "u2", Name: "g", IntervalMin: 30, Enabled: true})
	require.NoError(t, err)
	fl := NewFilters(db)
	ctx := context.Background()

	_, err = fl.Create(ctx, Filter{FeedID: feedA, Regex: "a1", Enabled: true})
	require.NoError(t, err)
	_, err = fl.Create(ctx, Filter{FeedID: feedA, Regex: "a2", Enabled: true})
	require.NoError(t, err)
	_, err = fl.Create(ctx, Filter{FeedID: feedB, Regex: "b1", Enabled: true})
	require.NoError(t, err)

	a, err := fl.ListByFeed(ctx, feedA)
	require.NoError(t, err)
	require.Len(t, a, 2)
	for _, f := range a {
		require.Equal(t, feedA, f.FeedID)
	}

	b, err := fl.ListByFeed(ctx, feedB)
	require.NoError(t, err)
	require.Len(t, b, 1)
	require.Equal(t, "b1", b[0].Regex)
}

func TestFilters_Update(t *testing.T) {
	db := newTestDB(t)
	feedID := newFeedForTest(t, db)
	fl := NewFilters(db)
	ctx := context.Background()

	id, _ := fl.Create(ctx, Filter{FeedID: feedID, Regex: "old", Enabled: true})
	require.NoError(t, fl.Update(ctx, Filter{ID: id, FeedID: feedID, Regex: "new", SavePath: "/x", Enabled: false}))

	got, _ := fl.Get(ctx, id)
	require.Equal(t, "new", got.Regex)
	require.Equal(t, "/x", got.SavePath)
	require.False(t, got.Enabled)
}

func TestFilters_Delete(t *testing.T) {
	db := newTestDB(t)
	feedID := newFeedForTest(t, db)
	fl := NewFilters(db)
	ctx := context.Background()

	id, _ := fl.Create(ctx, Filter{FeedID: feedID, Regex: "x", Enabled: true})
	require.NoError(t, fl.Delete(ctx, id))
	_, err := fl.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFilters_CascadeOnFeedDelete(t *testing.T) {
	db := newTestDB(t)
	feeds := NewFeeds(db)
	fl := NewFilters(db)
	ctx := context.Background()

	feedID := newFeedForTest(t, db)
	_, err := fl.Create(ctx, Filter{FeedID: feedID, Regex: "a", Enabled: true})
	require.NoError(t, err)
	_, err = fl.Create(ctx, Filter{FeedID: feedID, Regex: "b", Enabled: true})
	require.NoError(t, err)

	pre, err := fl.ListByFeed(ctx, feedID)
	require.NoError(t, err)
	require.Len(t, pre, 2)

	require.NoError(t, feeds.Delete(ctx, feedID))

	post, err := fl.ListByFeed(ctx, feedID)
	require.NoError(t, err)
	require.Len(t, post, 0)
}
