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

// GetWithBitmap returns Get + the saved piece-completion bitmap. bitmap
// may be nil even when ok=true (legacy rows or rows saved before piece
// state was tracked); callers fall back to verify in that case.
func (s *VerifySnapshots) GetWithBitmap(ctx context.Context, infohash string) (snapshot []byte, wasComplete bool, bitmap []byte, ok bool, err error) {
	var (
		wc int
		bm sql.RawBytes
	)
	err = s.db.SQL().QueryRowContext(ctx,
		`SELECT snapshot, was_complete, piece_bitmap FROM verify_snapshots WHERE infohash = ?`,
		infohash).Scan(&snapshot, &wc, &bm)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil, false, nil
	}
	if err != nil {
		return nil, false, nil, false, err
	}
	if len(bm) > 0 {
		bitmap = make([]byte, len(bm))
		copy(bitmap, bm)
	}
	return snapshot, wc == 1, bitmap, true, nil
}

// Upsert writes snapshot + wasComplete for the torrent. Leaves any
// existing piece_bitmap unchanged — callers that have a bitmap should
// use UpsertWithBitmap.
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

// UpsertWithBitmap writes the file snapshot AND the piece-completion
// bitmap atomically. The bitmap is what lets the next launch repopulate
// anacrolix's bolt store after its per-add storage init wipes
// "complete" entries — without it the engine has to re-hash every
// piece on disk to figure out what's actually present.
func (s *VerifySnapshots) UpsertWithBitmap(ctx context.Context, infohash string, snapshot []byte, wasComplete bool, bitmap []byte) error {
	wc := 0
	if wasComplete {
		wc = 1
	}
	_, err := s.db.SQL().ExecContext(ctx, `
INSERT INTO verify_snapshots (infohash, snapshot, was_complete, updated_at, piece_bitmap)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(infohash) DO UPDATE SET
  snapshot = excluded.snapshot,
  was_complete = excluded.was_complete,
  updated_at = excluded.updated_at,
  piece_bitmap = excluded.piece_bitmap`,
		infohash, snapshot, wc, time.Now().Unix(), bitmap)
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
