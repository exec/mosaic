package persistence

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// TorrentRecord is the persisted metadata for a single torrent.
type TorrentRecord struct {
	InfoHash    string
	Name        string
	Magnet      string
	SavePath    string
	AddedAt     time.Time
	CompletedAt *time.Time
	Paused      bool
}

// Torrents is the DAO for the torrents table.
type Torrents struct{ db *DB }

func NewTorrents(db *DB) *Torrents { return &Torrents{db: db} }

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// Save inserts or updates a torrent record.
func (t *Torrents) Save(ctx context.Context, r TorrentRecord) error {
	var completed sql.NullInt64
	if r.CompletedAt != nil {
		completed = sql.NullInt64{Int64: r.CompletedAt.Unix(), Valid: true}
	}
	paused := 0
	if r.Paused {
		paused = 1
	}
	_, err := t.db.SQL().ExecContext(ctx, `
INSERT INTO torrents (infohash, name, magnet, save_path, added_at, completed_at, paused)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(infohash) DO UPDATE SET
  name = excluded.name,
  magnet = excluded.magnet,
  save_path = excluded.save_path,
  added_at = excluded.added_at,
  completed_at = excluded.completed_at,
  paused = excluded.paused
`, r.InfoHash, r.Name, r.Magnet, r.SavePath, r.AddedAt.Unix(), completed, paused)
	return err
}

// Get returns a single record by infohash.
func (t *Torrents) Get(ctx context.Context, infohash string) (TorrentRecord, error) {
	row := t.db.SQL().QueryRowContext(ctx, `
SELECT infohash, name, COALESCE(magnet, ''), save_path, added_at, completed_at, paused
FROM torrents WHERE infohash = ?`, infohash)
	return scanTorrent(row)
}

// List returns all records ordered by added_at descending.
func (t *Torrents) List(ctx context.Context) ([]TorrentRecord, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `
SELECT infohash, name, COALESCE(magnet, ''), save_path, added_at, completed_at, paused
FROM torrents ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TorrentRecord
	for rows.Next() {
		r, err := scanTorrent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Remove deletes by infohash. Missing rows are not an error.
func (t *Torrents) Remove(ctx context.Context, infohash string) error {
	_, err := t.db.SQL().ExecContext(ctx, `DELETE FROM torrents WHERE infohash = ?`, infohash)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTorrent(s scanner) (TorrentRecord, error) {
	var r TorrentRecord
	var addedAt int64
	var completedAt sql.NullInt64
	var paused int
	if err := s.Scan(&r.InfoHash, &r.Name, &r.Magnet, &r.SavePath, &addedAt, &completedAt, &paused); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, ErrNotFound
		}
		return r, err
	}
	r.AddedAt = time.Unix(addedAt, 0)
	if completedAt.Valid {
		t := time.Unix(completedAt.Int64, 0)
		r.CompletedAt = &t
	}
	r.Paused = paused == 1
	return r, nil
}
