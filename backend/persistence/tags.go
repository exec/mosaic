package persistence

import (
	"context"
	"database/sql"
	"errors"
)

type Tag struct {
	ID    int
	Name  string
	Color string
}

type Tags struct{ db *DB }

func NewTags(db *DB) *Tags { return &Tags{db: db} }

func (t *Tags) Create(ctx context.Context, tag Tag) (int, error) {
	color := tag.Color
	if color == "" {
		color = "#71717a"
	}
	res, err := t.db.SQL().ExecContext(ctx,
		`INSERT INTO tags (name, color) VALUES (?, ?)`, tag.Name, color)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (t *Tags) Get(ctx context.Context, id int) (Tag, error) {
	var tag Tag
	err := t.db.SQL().QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE id = ?`, id,
	).Scan(&tag.ID, &tag.Name, &tag.Color)
	if errors.Is(err, sql.ErrNoRows) {
		return tag, ErrNotFound
	}
	return tag, err
}

func (t *Tags) List(ctx context.Context) ([]Tag, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `SELECT id, name, color FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

func (t *Tags) Update(ctx context.Context, tag Tag) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE tags SET name = ?, color = ? WHERE id = ?`, tag.Name, tag.Color, tag.ID)
	return err
}

func (t *Tags) Delete(ctx context.Context, id int) error {
	_, err := t.db.SQL().ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	return err
}

func (t *Tags) Assign(ctx context.Context, infohash string, tagID int) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`INSERT INTO torrent_tags (infohash, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
		infohash, tagID)
	return err
}

func (t *Tags) Unassign(ctx context.Context, infohash string, tagID int) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`DELETE FROM torrent_tags WHERE infohash = ? AND tag_id = ?`, infohash, tagID)
	return err
}

func (t *Tags) ForTorrent(ctx context.Context, infohash string) ([]Tag, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `
		SELECT t.id, t.name, t.color FROM tags t
		JOIN torrent_tags tt ON tt.tag_id = t.id
		WHERE tt.infohash = ? ORDER BY t.name`, infohash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}
