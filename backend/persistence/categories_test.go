package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCategories_CreateGet(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, err := c.Create(ctx, Category{Name: "Movies", DefaultSavePath: "/Volumes/media", Color: "#ef4444"})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := c.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "Movies", got.Name)
	require.Equal(t, "/Volumes/media", got.DefaultSavePath)
	require.Equal(t, "#ef4444", got.Color)
}

func TestCategories_List(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	_, _ = c.Create(ctx, Category{Name: "Movies"})
	_, _ = c.Create(ctx, Category{Name: "Software"})

	rows, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestCategories_Update(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, _ := c.Create(ctx, Category{Name: "Movies"})
	require.NoError(t, c.Update(ctx, Category{ID: id, Name: "Cinema", Color: "#000000"}))

	got, _ := c.Get(ctx, id)
	require.Equal(t, "Cinema", got.Name)
	require.Equal(t, "#000000", got.Color)
}

func TestCategories_Delete(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, _ := c.Create(ctx, Category{Name: "Tmp"})
	require.NoError(t, c.Delete(ctx, id))
	_, err := c.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCategories_NameUnique(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()
	_, err := c.Create(ctx, Category{Name: "Movies"})
	require.NoError(t, err)
	_, err = c.Create(ctx, Category{Name: "Movies"})
	require.Error(t, err)
}
