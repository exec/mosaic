package persistence

import (
	"context"
	"database/sql"
	"errors"
)

type ScheduleRule struct {
	ID       int
	DaysMask int
	StartMin int
	EndMin   int
	DownKbps int
	UpKbps   int
	AltOnly  bool
	Enabled  bool
}

type ScheduleRules struct{ db *DB }

func NewScheduleRules(db *DB) *ScheduleRules { return &ScheduleRules{db: db} }

func (s *ScheduleRules) Create(ctx context.Context, r ScheduleRule) (int, error) {
	res, err := s.db.SQL().ExecContext(ctx, `
INSERT INTO schedule_rules (days_mask, start_min, end_min, down_kbps, up_kbps, alt_only, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.DaysMask, r.StartMin, r.EndMin, r.DownKbps, r.UpKbps, boolToInt(r.AltOnly), boolToInt(r.Enabled))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (s *ScheduleRules) Get(ctx context.Context, id int) (ScheduleRule, error) {
	var r ScheduleRule
	var altOnly, enabled int
	err := s.db.SQL().QueryRowContext(ctx,
		`SELECT id, days_mask, start_min, end_min, down_kbps, up_kbps, alt_only, enabled FROM schedule_rules WHERE id = ?`, id).
		Scan(&r.ID, &r.DaysMask, &r.StartMin, &r.EndMin, &r.DownKbps, &r.UpKbps, &altOnly, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	r.AltOnly = altOnly == 1
	r.Enabled = enabled == 1
	return r, err
}

func (s *ScheduleRules) List(ctx context.Context) ([]ScheduleRule, error) {
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT id, days_mask, start_min, end_min, down_kbps, up_kbps, alt_only, enabled FROM schedule_rules ORDER BY days_mask, start_min`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduleRule
	for rows.Next() {
		var r ScheduleRule
		var altOnly, enabled int
		if err := rows.Scan(&r.ID, &r.DaysMask, &r.StartMin, &r.EndMin, &r.DownKbps, &r.UpKbps, &altOnly, &enabled); err != nil {
			return nil, err
		}
		r.AltOnly = altOnly == 1
		r.Enabled = enabled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *ScheduleRules) Update(ctx context.Context, r ScheduleRule) error {
	_, err := s.db.SQL().ExecContext(ctx,
		`UPDATE schedule_rules SET days_mask = ?, start_min = ?, end_min = ?, down_kbps = ?, up_kbps = ?, alt_only = ?, enabled = ? WHERE id = ?`,
		r.DaysMask, r.StartMin, r.EndMin, r.DownKbps, r.UpKbps, boolToInt(r.AltOnly), boolToInt(r.Enabled), r.ID)
	return err
}

func (s *ScheduleRules) Delete(ctx context.Context, id int) error {
	_, err := s.db.SQL().ExecContext(ctx, `DELETE FROM schedule_rules WHERE id = ?`, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
