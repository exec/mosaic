package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

func newTestService(t *testing.T) (*Service, *engine.FakeBackend) {
	t.Helper()
	db, err := persistence.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	fb := engine.NewFakeBackend()
	eng := engine.NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	svc := NewService(eng, persistence.NewTorrents(db), "/tmp/dl")
	return svc, fb
}

func TestService_AddMagnet_PersistsAndAddsToEngine(t *testing.T) {
	svc, fb := newTestService(t)

	id, err := svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc", "")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// engine sees it
	require.Len(t, fb.List(), 1)

	// persistence sees it
	rows, err := svc.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, string(id), rows[0].ID)
}

func TestService_AddMagnet_UsesDefaultSavePathWhenEmpty(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:def", "")
	require.NoError(t, err)

	rows, err := svc.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Equal(t, "/tmp/dl", rows[0].SavePath)
}

func TestService_Remove_RemovesFromEngineAndPersistence(t *testing.T) {
	svc, fb := newTestService(t)
	id, _ := svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:rm", "/tmp")
	require.NoError(t, svc.Remove(context.Background(), id, false))

	require.Len(t, fb.List(), 0)
	rows, _ := svc.ListTorrents(context.Background())
	require.Empty(t, rows)
}
