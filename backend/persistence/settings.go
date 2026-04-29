package persistence

import (
	"context"
	"database/sql"
	"errors"
)

// Settings is a key/value DAO over the settings table.
type Settings struct{ db *DB }

func NewSettings(db *DB) *Settings { return &Settings{db: db} }

func (s *Settings) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.SQL().QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Settings) Set(ctx context.Context, key, value string) error {
	_, err := s.db.SQL().ExecContext(ctx, `
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
