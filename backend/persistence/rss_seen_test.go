package persistence

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRSSSeen_AddAndAll(t *testing.T) {
	db := newTestDB(t)
	feeds := NewFeeds(db)
	seen := NewRSSSeen(db)
	ctx := context.Background()

	feedID, err := feeds.Create(ctx, Feed{URL: "https://example.test/f", Name: "f", Enabled: true})
	require.NoError(t, err)

	require.NoError(t, seen.Add(ctx, feedID, "guid-1", 1000))
	require.NoError(t, seen.Add(ctx, feedID, "guid-2", 1000))
	// Re-marking is a no-op, not an error.
	require.NoError(t, seen.Add(ctx, feedID, "guid-1", 1000))

	all, err := seen.All(ctx)
	require.NoError(t, err)
	require.Len(t, all[feedID], 2)
	require.Contains(t, all[feedID], "guid-1")
	require.Contains(t, all[feedID], "guid-2")
}

func TestRSSSeen_AddPrunesOldestBeyondKeep(t *testing.T) {
	db := newTestDB(t)
	feeds := NewFeeds(db)
	seen := NewRSSSeen(db)
	ctx := context.Background()

	feedID, err := feeds.Create(ctx, Feed{URL: "https://example.test/f", Name: "f", Enabled: true})
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		require.NoError(t, seen.Add(ctx, feedID, fmt.Sprintf("guid-%d", i), 3))
	}

	all, err := seen.All(ctx)
	require.NoError(t, err)
	require.Len(t, all[feedID], 3, "oldest rows beyond keep should be pruned")
	// Rows share a seen_at second; the id DESC tie-break keeps the newest.
	require.Contains(t, all[feedID], "guid-4")
	require.Contains(t, all[feedID], "guid-3")
	require.Contains(t, all[feedID], "guid-2")
}

func TestRSSSeen_CascadesWithFeed(t *testing.T) {
	db := newTestDB(t)
	feeds := NewFeeds(db)
	seen := NewRSSSeen(db)
	ctx := context.Background()

	feedID, err := feeds.Create(ctx, Feed{URL: "https://example.test/f", Name: "f", Enabled: true})
	require.NoError(t, err)
	require.NoError(t, seen.Add(ctx, feedID, "guid-1", 1000))
	require.NoError(t, feeds.Delete(ctx, feedID))

	all, err := seen.All(ctx)
	require.NoError(t, err)
	require.Empty(t, all[feedID])
}
