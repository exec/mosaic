package persistence

import (
	"context"
	"database/sql"
	"errors"
)

// TorrentTrackers is the DAO for per-torrent user-added tracker URLs.
type TorrentTrackers struct{ db *DB }

func NewTorrentTrackers(db *DB) *TorrentTrackers { return &TorrentTrackers{db: db} }

// Add records a tracker URL for a torrent. Idempotent: inserting the same
// (infohash, url) pair a second time is a no-op.
func (tt *TorrentTrackers) Add(ctx context.Context, infohash, url string) error {
	_, err := tt.db.SQL().ExecContext(ctx, `
INSERT INTO torrent_trackers (infohash, url) VALUES (?, ?)
ON CONFLICT (infohash, url) DO NOTHING
`, infohash, url)
	return err
}

// Remove deletes a tracker URL for a torrent. If the (infohash, url) pair does
// not exist the call is a no-op.
func (tt *TorrentTrackers) Remove(ctx context.Context, infohash, url string) error {
	_, err := tt.db.SQL().ExecContext(ctx, `
DELETE FROM torrent_trackers WHERE infohash = ? AND url = ?
`, infohash, url)
	return err
}

// List returns every tracker URL stored for a torrent. Returns an empty slice
// (not an error) when none are present.
func (tt *TorrentTrackers) List(ctx context.Context, infohash string) ([]string, error) {
	rows, err := tt.db.SQL().QueryContext(ctx, `
SELECT url FROM torrent_trackers WHERE infohash = ? ORDER BY url
`, infohash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
