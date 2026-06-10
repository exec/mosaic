package persistence

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// TorrentRecord is the persisted metadata for a single torrent.
type TorrentRecord struct {
	InfoHash      string
	Name          string
	Magnet        string
	SavePath      string
	CategoryID    *int // nullable foreign key
	AddedAt       time.Time
	CompletedAt   *time.Time
	Paused        bool
	QueuePosition int // 0 = top
	ForceStart    bool
	// Metainfo is the raw .torrent file bytes for file-added torrents. Empty
	// for magnet-only adds (the magnet URI itself is enough to round-trip).
	Metainfo []byte
	// Per-torrent bandwidth caps in bytes/sec. 0 means unlimited.
	DownRateLimit int64
	UpRateLimit   int64
	// SeedPolicy is the JSON-encoded per-torrent seeding stopping condition.
	// NULL means "use the global default".
	SeedPolicy *string
	// SeedingStartedAt is when the torrent first reached 100% completion.
	// Used by the seed-policy enforcement loop to measure seeding duration.
	SeedingStartedAt *time.Time
	Sequential       bool
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
	var catID sql.NullInt64
	if r.CategoryID != nil {
		catID = sql.NullInt64{Int64: int64(*r.CategoryID), Valid: true}
	}
	paused := 0
	if r.Paused {
		paused = 1
	}
	forceStart := 0
	if r.ForceStart {
		forceStart = 1
	}
	sequential := 0
	if r.Sequential {
		sequential = 1
	}
	_, err := t.db.SQL().ExecContext(ctx, `
INSERT INTO torrents (infohash, name, magnet, save_path, category_id, added_at, completed_at, paused, queue_position, force_start, metainfo, down_rate_limit, up_rate_limit, sequential)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(infohash) DO UPDATE SET
  name = excluded.name,
  magnet = COALESCE(NULLIF(excluded.magnet, ''), torrents.magnet),
  metainfo = COALESCE(excluded.metainfo, torrents.metainfo)
`, r.InfoHash, r.Name, r.Magnet, r.SavePath, catID, r.AddedAt.Unix(), completed, paused, r.QueuePosition, forceStart, nullableBytes(r.Metainfo), r.DownRateLimit, r.UpRateLimit, sequential)
	// On duplicate adds (RSS re-match, watch-folder re-scan, user re-dropping
	// the same file) only identity/metadata fields are refreshed. User state —
	// added_at, paused, queue_position, force_start, category_id, rate limits,
	// sequential, completed_at — is owned by the dedicated Set* methods and
	// must not be reset by a re-add. save_path stays too: the engine keeps
	// using the original storage location for an already-known infohash, so
	// persisting a new path would desync DB from disk. seed_policy and
	// seeding_started_at are likewise set by dedicated methods only.
	return err
}

// SetSeedPolicy stores the JSON-encoded seed policy for a torrent.
// Pass nil to revert to "use global default".
func (t *Torrents) SetSeedPolicy(ctx context.Context, infohash string, policyJSON *string) error {
	var v any
	if policyJSON != nil {
		v = *policyJSON
	}
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET seed_policy = ? WHERE infohash = ?`, v, infohash)
	return err
}

// SetSeedingStartedAt records the moment a torrent first completed.
// Pass nil to clear (e.g. if the torrent is re-verified / re-downloaded).
func (t *Torrents) SetSeedingStartedAt(ctx context.Context, infohash string, at *time.Time) error {
	var v sql.NullInt64
	if at != nil {
		v = sql.NullInt64{Int64: at.Unix(), Valid: true}
	}
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET seeding_started_at = ? WHERE infohash = ?`, v, infohash)
	return err
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// Get returns a single record by infohash.
func (t *Torrents) Get(ctx context.Context, infohash string) (TorrentRecord, error) {
	row := t.db.SQL().QueryRowContext(ctx, `
SELECT infohash, name, COALESCE(magnet, ''), save_path, category_id, added_at, completed_at, paused, queue_position, force_start, metainfo, down_rate_limit, up_rate_limit, seed_policy, seeding_started_at, sequential
FROM torrents WHERE infohash = ?`, infohash)
	return scanTorrent(row)
}

// List returns all records ordered by added_at descending.
func (t *Torrents) List(ctx context.Context) ([]TorrentRecord, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `
SELECT infohash, name, COALESCE(magnet, ''), save_path, category_id, added_at, completed_at, paused, queue_position, force_start, metainfo, down_rate_limit, up_rate_limit, seed_policy, seeding_started_at, sequential
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

// SetCategory assigns or clears the category for a torrent.
func (t *Torrents) SetCategory(ctx context.Context, infohash string, categoryID *int) error {
	var v sql.NullInt64
	if categoryID != nil {
		v = sql.NullInt64{Int64: int64(*categoryID), Valid: true}
	}
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET category_id = ? WHERE infohash = ?`, v, infohash)
	return err
}

// SetQueuePosition updates the queue position for a torrent.
func (t *Torrents) SetQueuePosition(ctx context.Context, infohash string, pos int) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET queue_position = ? WHERE infohash = ?`, pos, infohash)
	return err
}

// SetPaused persists whether a torrent is user-paused, so the state survives
// restarts (RestoreOnStartup re-pauses torrents with paused=1).
func (t *Torrents) SetPaused(ctx context.Context, infohash string, paused bool) error {
	v := 0
	if paused {
		v = 1
	}
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET paused = ? WHERE infohash = ?`, v, infohash)
	return err
}

// SetCompletedAt records when a torrent first finished downloading. Written
// once by the service when it observes the completed transition; never
// cleared. RestoreOnStartup uses it to arm missing-files detection.
func (t *Torrents) SetCompletedAt(ctx context.Context, infohash string, at time.Time) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET completed_at = ? WHERE infohash = ?`, at.Unix(), infohash)
	return err
}

// SetForceStart toggles whether a torrent bypasses the queue limit.
func (t *Torrents) SetForceStart(ctx context.Context, infohash string, force bool) error {
	v := 0
	if force {
		v = 1
	}
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET force_start = ? WHERE infohash = ?`, v, infohash)
	return err
}

// SetSequential toggles whether a torrent downloads pieces in sequential order.
func (t *Torrents) SetSequential(ctx context.Context, infohash string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET sequential = ? WHERE infohash = ?`, v, infohash)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTorrent(s scanner) (TorrentRecord, error) {
	var r TorrentRecord
	var addedAt int64
	var completedAt sql.NullInt64
	var categoryID sql.NullInt64
	var paused int
	var forceStart int
	var sequential int
	var metainfo []byte
	var seedPolicy sql.NullString
	var seedingStartedAt sql.NullInt64
	if err := s.Scan(&r.InfoHash, &r.Name, &r.Magnet, &r.SavePath, &categoryID, &addedAt, &completedAt, &paused, &r.QueuePosition, &forceStart, &metainfo, &r.DownRateLimit, &r.UpRateLimit, &seedPolicy, &seedingStartedAt, &sequential); err != nil {
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
	if categoryID.Valid {
		v := int(categoryID.Int64)
		r.CategoryID = &v
	}
	r.Paused = paused == 1
	r.ForceStart = forceStart == 1
	r.Sequential = sequential == 1
	r.Metainfo = metainfo
	if seedPolicy.Valid {
		r.SeedPolicy = &seedPolicy.String
	}
	if seedingStartedAt.Valid {
		t := time.Unix(seedingStartedAt.Int64, 0)
		r.SeedingStartedAt = &t
	}
	return r, nil
}

// SetRateLimits persists per-torrent download and upload bandwidth caps.
// Pass 0 for either to mean unlimited.
func (t *Torrents) SetRateLimits(ctx context.Context, infohash string, downBytesPerSec, upBytesPerSec int64) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET down_rate_limit = ?, up_rate_limit = ? WHERE infohash = ?`,
		downBytesPerSec, upBytesPerSec, infohash)
	return err
}
