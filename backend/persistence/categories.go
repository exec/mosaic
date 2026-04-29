package persistence

import (
	"context"
	"database/sql"
	"errors"
)

type Category struct {
	ID              int
	Name            string
	DefaultSavePath string
	Color           string
}

type Categories struct{ db *DB }

func NewCategories(db *DB) *Categories { return &Categories{db: db} }

func (c *Categories) Create(ctx context.Context, cat Category) (int, error) {
	color := cat.Color
	if color == "" {
		color = "#71717a"
	}
	res, err := c.db.SQL().ExecContext(ctx,
		`INSERT INTO categories (name, default_save_path, color) VALUES (?, ?, ?)`,
		cat.Name, cat.DefaultSavePath, color)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (c *Categories) Get(ctx context.Context, id int) (Category, error) {
	var cat Category
	err := c.db.SQL().QueryRowContext(ctx,
		`SELECT id, name, COALESCE(default_save_path, ''), color FROM categories WHERE id = ?`, id,
	).Scan(&cat.ID, &cat.Name, &cat.DefaultSavePath, &cat.Color)
	if errors.Is(err, sql.ErrNoRows) {
		return cat, ErrNotFound
	}
	return cat, err
}

func (c *Categories) List(ctx context.Context) ([]Category, error) {
	rows, err := c.db.SQL().QueryContext(ctx,
		`SELECT id, name, COALESCE(default_save_path, ''), color FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var cat Category
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.DefaultSavePath, &cat.Color); err != nil {
			return nil, err
		}
		out = append(out, cat)
	}
	return out, rows.Err()
}

func (c *Categories) Update(ctx context.Context, cat Category) error {
	_, err := c.db.SQL().ExecContext(ctx,
		`UPDATE categories SET name = ?, default_save_path = ?, color = ? WHERE id = ?`,
		cat.Name, cat.DefaultSavePath, cat.Color, cat.ID)
	return err
}

func (c *Categories) Delete(ctx context.Context, id int) error {
	_, err := c.db.SQL().ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	return err
}
