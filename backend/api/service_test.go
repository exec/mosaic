package api

import (
	"context"
	"os"
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

func TestService_AddTorrentFile_PersistsAndAddsToEngine(t *testing.T) {
	svc, fb := newTestService(t)

	// FakeBackend.AddFile hashes the blob bytes; any non-empty blob works for
	// the api-layer contract. (Bencoded validity is the engine's concern.)
	path := filepath.Join(t.TempDir(), "fixture.torrent")
	require.NoError(t, os.WriteFile(path, []byte("d4:infod6:lengthi42e4:name3:abcee"), 0o644))

	id, err := svc.AddTorrentFile(context.Background(), path)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	require.Len(t, fb.List(), 1)

	rows, err := svc.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "/tmp/dl", rows[0].SavePath)
}

func TestService_AddTorrentFile_ErrorOnMissingFile(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.AddTorrentFile(context.Background(), filepath.Join(t.TempDir(), "does-not-exist.torrent"))
	require.Error(t, err)
}

func TestService_Remove_RemovesFromEngineAndPersistence(t *testing.T) {
	svc, fb := newTestService(t)
	id, _ := svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:rm", "/tmp")
	require.NoError(t, svc.Remove(context.Background(), id, false))

	require.Len(t, fb.List(), 0)
	rows, _ := svc.ListTorrents(context.Background())
	require.Empty(t, rows)
}

func TestService_GlobalStats(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Empty state
	stats, err := svc.GlobalStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, stats.TotalTorrents)
	require.Equal(t, 0, stats.ActiveTorrents)
	require.Equal(t, 0, stats.SeedingTorrents)

	// Add two torrents, one paused
	id1, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:abc", "")
	_, _ = svc.AddMagnet(ctx, "magnet:?xt=urn:btih:def", "")
	require.NoError(t, svc.Pause(id1))

	stats, err = svc.GlobalStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, stats.TotalTorrents)
}
