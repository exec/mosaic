package persistence

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// VerifySnapshots is the DAO for the verify_snapshots table. The engine uses
// it as a fast-resume hint: on startup-add, if the recorded snapshot still
// matches the on-disk file state and the prior session ended complete, the
// engine skips the full piece-by-piece verify and trusts anacrolix's
// bolt-backed piece-completion store.
type VerifySnapshots struct{ db *DB }

func NewVerifySnapshots(db *DB) *VerifySnapshots { return &VerifySnapshots{db: db} }

// Get returns (snapshot, wasComplete, ok, err). ok=false means no row.
// A SQL error returns ok=false and a non-nil err; callers may log and
// proceed (a missing or unreadable snapshot just means we fall back to
// the full verify).
func (s *VerifySnapshots) Get(ctx context.Context, infohash string) ([]byte, bool, bool, error) {
	var (
		snapshot []byte
		wc       int
	)
	err := s.db.SQL().QueryRowContext(ctx,
		`SELECT snapshot, was_complete FROM verify_snapshots WHERE infohash = ?`,
		infohash).Scan(&snapshot, &wc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	return snapshot, wc == 1, true, nil
}

// Upsert writes snapshot + wasComplete for the torrent.
func (s *VerifySnapshots) Upsert(ctx context.Context, infohash string, snapshot []byte, wasComplete bool) error {
	wc := 0
	if wasComplete {
		wc = 1
	}
	_, err := s.db.SQL().ExecContext(ctx, `
INSERT INTO verify_snapshots (infohash, snapshot, was_complete, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(infohash) DO UPDATE SET
  snapshot = excluded.snapshot,
  was_complete = excluded.was_complete,
  updated_at = excluded.updated_at`,
		infohash, snapshot, wc, time.Now().Unix())
	return err
}

// Delete removes the row. Used by Recheck so a forced full verify
// re-establishes the snapshot from the just-verified state. Missing rows
// are not an error.
func (s *VerifySnapshots) Delete(ctx context.Context, infohash string) error {
	_, err := s.db.SQL().ExecContext(ctx,
		`DELETE FROM verify_snapshots WHERE infohash = ?`, infohash)
	return err
}
