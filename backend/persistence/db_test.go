package persistence

import (
	"context"
	"path/filepath"
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
