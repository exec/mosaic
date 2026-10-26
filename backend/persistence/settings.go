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

// SetMany commits a validated settings backup without leaving a partial import.
func (s *Settings) SetMany(ctx context.Context, values map[string]string) error {
	tx, err := s.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}
