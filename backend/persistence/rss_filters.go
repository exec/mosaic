package persistence

import (
	"context"
	"database/sql"
	"errors"
)

type Filter struct {
	ID         int
	FeedID     int
	Regex      string
	CategoryID *int
	SavePath   string
	Enabled    bool
}

type Filters struct{ db *DB }

func NewFilters(db *DB) *Filters { return &Filters{db: db} }

func (f *Filters) Create(ctx context.Context, fil Filter) (int, error) {
	var catID sql.NullInt64
	if fil.CategoryID != nil {
		catID = sql.NullInt64{Int64: int64(*fil.CategoryID), Valid: true}
	}
	var savePath sql.NullString
	if fil.SavePath != "" {
		savePath = sql.NullString{String: fil.SavePath, Valid: true}
	}
	res, err := f.db.SQL().ExecContext(ctx,
		`INSERT INTO rss_filters (feed_id, regex, category_id, save_path, enabled)
		 VALUES (?, ?, ?, ?, ?)`,
		fil.FeedID, fil.Regex, catID, savePath, boolToInt(fil.Enabled))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (f *Filters) Get(ctx context.Context, id int) (Filter, error) {
	row := f.db.SQL().QueryRowContext(ctx,
		`SELECT id, feed_id, regex, category_id, save_path, enabled FROM rss_filters WHERE id = ?`, id)
	fil, err := scanFilter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Filter{}, ErrNotFound
	}
	return fil, err
}

func (f *Filters) List(ctx context.Context) ([]Filter, error) {
	rows, err := f.db.SQL().QueryContext(ctx,
		`SELECT id, feed_id, regex, category_id, save_path, enabled FROM rss_filters ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectFilters(rows)
}

func (f *Filters) ListByFeed(ctx context.Context, feedID int) ([]Filter, error) {
	rows, err := f.db.SQL().QueryContext(ctx,
		`SELECT id, feed_id, regex, category_id, save_path, enabled FROM rss_filters WHERE feed_id = ? ORDER BY id`, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectFilters(rows)
}

func (f *Filters) Update(ctx context.Context, fil Filter) error {
	var catID sql.NullInt64
	if fil.CategoryID != nil {
		catID = sql.NullInt64{Int64: int64(*fil.CategoryID), Valid: true}
	}
	var savePath sql.NullString
	if fil.SavePath != "" {
		savePath = sql.NullString{String: fil.SavePath, Valid: true}
	}
	_, err := f.db.SQL().ExecContext(ctx,
		`UPDATE rss_filters SET feed_id = ?, regex = ?, category_id = ?, save_path = ?, enabled = ? WHERE id = ?`,
		fil.FeedID, fil.Regex, catID, savePath, boolToInt(fil.Enabled), fil.ID)
	return err
}

func (f *Filters) Delete(ctx context.Context, id int) error {
	_, err := f.db.SQL().ExecContext(ctx, `DELETE FROM rss_filters WHERE id = ?`, id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFilter(r rowScanner) (Filter, error) {
	var fil Filter
	var catID sql.NullInt64
	var savePath sql.NullString
	var enabled int
	if err := r.Scan(&fil.ID, &fil.FeedID, &fil.Regex, &catID, &savePath, &enabled); err != nil {
		return fil, err
	}
	if catID.Valid {
		v := int(catID.Int64)
		fil.CategoryID = &v
	}
	if savePath.Valid {
		fil.SavePath = savePath.String
	}
	fil.Enabled = enabled == 1
	return fil, nil
}

func collectFilters(rows *sql.Rows) ([]Filter, error) {
	var out []Filter
	for rows.Next() {
		fil, err := scanFilter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fil)
	}
	return out, rows.Err()
}
