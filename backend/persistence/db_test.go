package persistence

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpen_RunsMigrations(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.SQL().Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.Contains(t, names, "torrents")
	require.Contains(t, names, "settings")
	require.Contains(t, names, "categories")
	require.Contains(t, names, "tags")
	require.Contains(t, names, "torrent_tags")
	require.Contains(t, names, "schedule_rules")
	require.Contains(t, names, "rss_feeds")
	require.Contains(t, names, "rss_filters")

	require.Eventually(t, func() bool {
		rows, err := db.SQL().Query(`SELECT name FROM pragma_table_info('torrents')`)
		require.NoError(t, err)
		defer rows.Close()
		have := map[string]bool{}
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			have[n] = true
		}
		return have["queue_position"] && have["force_start"]
	}, time.Second, 50*time.Millisecond)
}

// TestOpen_BusyTimeoutSurvivesConcurrentWriters confirms the
// busy_timeout=5000 PRAGMA in Open is enough to ride out the kind of
// burst contention our DAO sees in production: the RSS poller batching
// adds while a user is renaming categories, the engine tick saving
// queue positions while goose runs an upgrade migration on next launch,
// etc. Eight goroutines × 50 inserts each (400 writes total) over the
// torrents table shouldn't surface "database is locked" with a 5-second
// blast-radius cap on contention. If it does, the timeout needs raising
// (or we need to serialize writes).
//
// The existing v0.4.2 audit flagged this as untested under load; this
// closes that out.
func TestOpen_BusyTimeoutSurvivesConcurrentWriters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping busy-timeout stress test in -short mode")
	}
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "stress.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	torrents := NewTorrents(db)

	const writers = 8
	const writesPerWriter = 50

	var wg sync.WaitGroup
	var failures atomic.Int32
	start := make(chan struct{})

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-start
			for i := 0; i < writesPerWriter; i++ {
				infohash := fmt.Sprintf("stress-%d-%d", workerID, i)
				rec := TorrentRecord{
					InfoHash: infohash,
					Name:     "stress",
					SavePath: "/tmp",
					AddedAt:  time.Now(),
				}
				if err := torrents.Save(context.Background(), rec); err != nil {
					failures.Add(1)
					t.Logf("worker %d insert %d: %v", workerID, i, err)
				}
			}
		}(w)
	}

	close(start) // unleash everyone simultaneously
	wg.Wait()

	require.Zero(t, failures.Load(), "concurrent inserts should not surface database-locked errors with busy_timeout=5000")

	// Confirm we actually wrote what we expected — busy_timeout surviving
	// without committing rows would mask a different bug.
	var count int
	require.NoError(t, db.SQL().QueryRow(`SELECT COUNT(*) FROM torrents WHERE infohash LIKE 'stress-%'`).Scan(&count))
	require.Equal(t, writers*writesPerWriter, count)
}
