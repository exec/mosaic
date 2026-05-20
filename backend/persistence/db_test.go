package persistence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// TestOpen_DBFileIsMode0600 verifies that Open tightens the on-disk sqlite
// file (and its WAL/SHM siblings, if present) to 0600 so other local accounts
// can't read the password hashes / API-key SHA-256s stored inside.
//
// Skipped on Windows because unix permission bits are not enforceable there
// — the chmod call still runs in production but the kernel ignores the
// non-owner bits.
func TestOpen_DBFileIsMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file mode semantics are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "perm.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm(),
		"db file %s should be mode 0600 (got %#o)", path, st.Mode().Perm())

	// WAL/SHM are created on first write; goose migrations write, so they
	// should exist after Open returns. Verify if they exist that they're
	// also 0600. If they're absent (some sqlite builds defer WAL creation
	// to the first non-migration write), the assertion is vacuously true.
	for _, sibling := range []string{path + "-wal", path + "-shm"} {
		st, err := os.Stat(sibling)
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), st.Mode().Perm(),
			"sibling %s should be mode 0600 (got %#o)", sibling, st.Mode().Perm())
	}
}

// TestOpen_DBFileChmodFailureNonFatal: an unwritable parent dir or a file
// the process can't chmod must NOT cause Open() to error out. We simulate
// this by pre-creating the db file as a different mode, then opening; the
// chmod inside Open will succeed in this scenario so it's mainly a smoke
// test that Open doesn't panic or fail-open on unusual starting modes.
func TestOpen_DBFileChmodIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file mode semantics are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "idem.db")
	// Pre-create with overly-permissive mode 0644 (the default umask
	// outcome we're trying to prevent).
	require.NoError(t, os.WriteFile(path, []byte{}, 0o644))

	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm(),
		"db file should be tightened from 0644 to 0600 by Open")
}
