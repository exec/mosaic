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

func (f *Feeds) Create(ctx context.Context, feed Feed) (int, error) {
	interval := feed.IntervalMin
	if interval <= 0 {
		interval = 30
	}
	res, err := f.db.SQL().ExecContext(ctx,
		`INSERT INTO rss_feeds (url, name, interval_min, last_polled, etag, enabled)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		feed.URL, feed.Name, interval, feed.LastPolled.Unix(), feed.ETag, boolToInt(feed.Enabled))
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
	feed.LastPolled = time.Unix(lastPolled, 0)
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
		feed.LastPolled = time.Unix(lastPolled, 0)
		feed.Enabled = enabled == 1
		out = append(out, feed)
	}
	return out, rows.Err()
}

func (f *Feeds) Update(ctx context.Context, feed Feed) error {
	_, err := f.db.SQL().ExecContext(ctx,
		`UPDATE rss_feeds SET url = ?, name = ?, interval_min = ?, enabled = ? WHERE id = ?`,
		feed.URL, feed.Name, feed.IntervalMin, boolToInt(feed.Enabled), feed.ID)
	return err
}

func (f *Feeds) Delete(ctx context.Context, id int) error {
	_, err := f.db.SQL().ExecContext(ctx, `DELETE FROM rss_feeds WHERE id = ?`, id)
	return err
}

func (f *Feeds) UpdatePollResult(ctx context.Context, id int, lastPolled time.Time, etag string) error {
	_, err := f.db.SQL().ExecContext(ctx,
		`UPDATE rss_feeds SET last_polled = ?, etag = ? WHERE id = ?`,
		lastPolled.Unix(), etag, id)
	return err
}
