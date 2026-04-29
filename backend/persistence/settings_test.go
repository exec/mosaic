package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettings_GetSet(t *testing.T) {
	db := newTestDB(t)
	s := NewSettings(db)
	ctx := context.Background()

	_, err := s.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.Set(ctx, "alt_speed", "true"))
	v, err := s.Get(ctx, "alt_speed")
	require.NoError(t, err)
	require.Equal(t, "true", v)

	require.NoError(t, s.Set(ctx, "alt_speed", "false"))
	v, err = s.Get(ctx, "alt_speed")
	require.NoError(t, err)
	require.Equal(t, "false", v)
}
