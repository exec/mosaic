package persistence

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Feed struct {
	ID          int
	URL         string
	Name        string
	IntervalMin int
	LastPolled  time.Time
	ETag        string
	Enabled     bool
}

type Feeds struct{ db *DB }

func NewFeeds(db *DB) *Feeds { return &Feeds{db: db} }

// unixOrZero serializes a time.Time to a unix timestamp, mapping the zero
// time.Time{} to 0 rather than the bogus -62135596800 that t.Unix() yields for
// the zero value. The read side treats a stored 0 as the zero time.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// timeFromUnix is the inverse of unixOrZero: a stored 0 round-trips back to the
// zero time.Time{} (so IsZero() is true) rather than the 1970 epoch.
func timeFromUnix(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func (f *Feeds) Create(ctx context.Context, feed Feed) (int, error) {
	interval := feed.IntervalMin
	if interval <= 0 {
		interval = 30
	}
	res, err := f.db.SQL().ExecContext(ctx,
		`INSERT INTO rss_feeds (url, name, interval_min, last_polled, etag, enabled)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		feed.URL, feed.Name, interval, unixOrZero(feed.LastPolled), feed.ETag, boolToInt(feed.Enabled))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (f *Feeds) Get(ctx context.Context, id int) (Feed, error) {
	var feed Feed
	var lastPolled int64
	var enabled int
	err := f.db.SQL().QueryRowContext(ctx,
		`SELECT id, url, name, interval_min, last_polled, etag, enabled FROM rss_feeds WHERE id = ?`, id,
	).Scan(&feed.ID, &feed.URL, &feed.Name, &feed.IntervalMin, &lastPolled, &feed.ETag, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return feed, ErrNotFound
	}
	if err != nil {
		return feed, err
	}
	feed.LastPolled = timeFromUnix(lastPolled)
	feed.Enabled = enabled == 1
	return feed, nil
}

func (f *Feeds) List(ctx context.Context) ([]Feed, error) {
	rows, err := f.db.SQL().QueryContext(ctx,
		`SELECT id, url, name, interval_min, last_polled, etag, enabled FROM rss_feeds ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feed
	for rows.Next() {
		var feed Feed
		var lastPolled int64
		var enabled int
		if err := rows.Scan(&feed.ID, &feed.URL, &feed.Name, &feed.IntervalMin, &lastPolled, &feed.ETag, &enabled); err != nil {
			return nil, err
		}
		feed.LastPolled = timeFromUnix(lastPolled)
		feed.Enabled = enabled == 1
		out = append(out, feed)
	}
	return out, rows.Err()
}

func (f *Feeds) Update(ctx context.Context, feed Feed) error {
	interval := feed.IntervalMin
	if interval <= 0 {
		interval = 30
	}
	_, err := f.db.SQL().ExecContext(ctx,
		`UPDATE rss_feeds SET url = ?, name = ?, interval_min = ?, enabled = ? WHERE id = ?`,
		feed.URL, feed.Name, interval, boolToInt(feed.Enabled), feed.ID)
	return err
}

func (f *Feeds) Delete(ctx context.Context, id int) error {
	_, err := f.db.SQL().ExecContext(ctx, `DELETE FROM rss_feeds WHERE id = ?`, id)
	return err
}

func (f *Feeds) UpdatePollResult(ctx context.Context, id int, lastPolled time.Time, etag string) error {
	_, err := f.db.SQL().ExecContext(ctx,
		`UPDATE rss_feeds SET last_polled = ?, etag = ? WHERE id = ?`,
		unixOrZero(lastPolled), etag, id)
	return err
}
