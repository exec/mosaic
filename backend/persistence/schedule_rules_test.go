package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScheduleRules_CreateGet(t *testing.T) {
	db := newTestDB(t)
	s := NewScheduleRules(db)
	ctx := context.Background()

	id, err := s.Create(ctx, ScheduleRule{
		DaysMask: 0b0111110, // Mon-Fri
		StartMin: 22 * 60,
		EndMin:   6 * 60,
		DownKbps: 500,
		UpKbps:   100,
		AltOnly:  true,
		Enabled:  true,
	})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, 0b0111110, got.DaysMask)
	require.Equal(t, 22*60, got.StartMin)
	require.Equal(t, 6*60, got.EndMin)
	require.Equal(t, 500, got.DownKbps)
	require.Equal(t, 100, got.UpKbps)
	require.True(t, got.AltOnly)
	require.True(t, got.Enabled)
}

func TestScheduleRules_List(t *testing.T) {
	db := newTestDB(t)
	s := NewScheduleRules(db)
	ctx := context.Background()

	// Insert in non-sorted order to verify ORDER BY days_mask, start_min
	_, err := s.Create(ctx, ScheduleRule{DaysMask: 4, StartMin: 600, EndMin: 700, Enabled: true})
	require.NoError(t, err)
	_, err = s.Create(ctx, ScheduleRule{DaysMask: 2, StartMin: 800, EndMin: 900, Enabled: true})
	require.NoError(t, err)
	_, err = s.Create(ctx, ScheduleRule{DaysMask: 2, StartMin: 100, EndMin: 200, Enabled: true})
	require.NoError(t, err)

	rows, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, 2, rows[0].DaysMask)
	require.Equal(t, 100, rows[0].StartMin)
	require.Equal(t, 2, rows[1].DaysMask)
	require.Equal(t, 800, rows[1].StartMin)
	require.Equal(t, 4, rows[2].DaysMask)
}

func TestScheduleRules_Update(t *testing.T) {
	db := newTestDB(t)
	s := NewScheduleRules(db)
	ctx := context.Background()

	id, err := s.Create(ctx, ScheduleRule{DaysMask: 1, StartMin: 0, EndMin: 60, DownKbps: 100, UpKbps: 50, AltOnly: false, Enabled: true})
	require.NoError(t, err)

	require.NoError(t, s.Update(ctx, ScheduleRule{
		ID: id, DaysMask: 64, StartMin: 120, EndMin: 240, DownKbps: 999, UpKbps: 333, AltOnly: true, Enabled: false,
	}))

	got, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 64, got.DaysMask)
	require.Equal(t, 120, got.StartMin)
	require.Equal(t, 240, got.EndMin)
	require.Equal(t, 999, got.DownKbps)
	require.Equal(t, 333, got.UpKbps)
	require.True(t, got.AltOnly)
	require.False(t, got.Enabled)
}

func TestScheduleRules_Delete(t *testing.T) {
	db := newTestDB(t)
	s := NewScheduleRules(db)
	ctx := context.Background()

	id, err := s.Create(ctx, ScheduleRule{DaysMask: 1, StartMin: 0, EndMin: 60, Enabled: true})
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, id))

	_, err = s.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}
