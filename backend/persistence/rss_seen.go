package persistence

import (
	"context"
	"time"
)

// RSSSeen is the DAO for the rss_seen table: per-feed GUIDs of feed items the
// poller has already handled, persisted so dedup survives restarts. Rows
// cascade-delete with their feed.
type RSSSeen struct{ db *DB }

func NewRSSSeen(db *DB) *RSSSeen { return &RSSSeen{db: db} }

// All returns every seen GUID grouped by feed id. The RSS poller loads this
// once at startup to seed its in-memory cache.
func (s *RSSSeen) All(ctx context.Context) (map[int]map[string]struct{}, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `SELECT feed_id, guid FROM rss_seen`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int]map[string]struct{})
	for rows.Next() {
		var feedID int
		var guid string
		if err := rows.Scan(&feedID, &guid); err != nil {
			return nil, err
		}
		if out[feedID] == nil {
			out[feedID] = make(map[string]struct{})
		}
		out[feedID][guid] = struct{}{}
	}
	return out, rows.Err()
}

// Add records a seen GUID, then prunes the oldest rows beyond keep so a
// high-churn feed can't grow the table without bound. Re-marking an
// already-seen GUID is a no-op.
func (s *RSSSeen) Add(ctx context.Context, feedID int, guid string, keep int) error {
	if _, err := s.db.SQL().ExecContext(ctx,
		`INSERT INTO rss_seen (feed_id, guid, seen_at) VALUES (?, ?, ?)
		 ON CONFLICT(feed_id, guid) DO NOTHING`,
		feedID, guid, time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.db.SQL().ExecContext(ctx, `
DELETE FROM rss_seen WHERE feed_id = ? AND id NOT IN (
  SELECT id FROM rss_seen WHERE feed_id = ? ORDER BY seen_at DESC, id DESC LIMIT ?
)`, feedID, feedID, keep)
	return err
}
