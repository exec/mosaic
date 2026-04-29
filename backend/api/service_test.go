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

	svc := NewService(eng,
		persistence.NewTorrents(db),
		persistence.NewCategories(db),
		persistence.NewTags(db),
		persistence.NewSettings(db),
		"/tmp/dl")
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

func TestService_InspectorFocus_StoresAndReturnsDetail(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:focus", "")

	// No focus set — DetailForFocus returns nil, nil
	got, err := svc.DetailForFocus(ctx)
	require.NoError(t, err)
	require.Nil(t, got)

	// Set focus to this torrent with all tabs
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview", "files", "peers", "trackers"}))

	got, err = svc.DetailForFocus(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, string(id), got.ID)
	require.Len(t, got.Files, 2)
	require.Len(t, got.PeersList, 1)
	require.Len(t, got.Trackers, 1)
}

func TestService_ClearInspectorFocus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:cf", "")
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview"}))
	svc.ClearInspectorFocus()

	got, err := svc.DetailForFocus(ctx)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestService_InspectorFocus_ScopesByVisibleTabs(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:scope2", "")

	// Only Overview tab visible — files/peers/trackers should be empty
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview"}))
	got, _ := svc.DetailForFocus(ctx)
	require.NotNil(t, got)
	require.Empty(t, got.Files)
	require.Empty(t, got.PeersList)
	require.Empty(t, got.Trackers)

	// Switch to peers tab
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview", "peers"}))
	got, _ = svc.DetailForFocus(ctx)
	require.Empty(t, got.Files)
	require.Len(t, got.PeersList, 1)
	require.Empty(t, got.Trackers)
}

func TestService_CategoryCRUD(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, err := svc.CreateCategory(ctx, "Movies", "/Volumes/media", "#ef4444")
	require.NoError(t, err)
	require.Greater(t, id, 0)

	cats, err := svc.ListCategories(ctx)
	require.NoError(t, err)
	require.Len(t, cats, 1)
	require.Equal(t, "Movies", cats[0].Name)

	require.NoError(t, svc.UpdateCategory(ctx, id, "Cinema", "/v/m", "#000"))
	cats, _ = svc.ListCategories(ctx)
	require.Equal(t, "Cinema", cats[0].Name)

	require.NoError(t, svc.DeleteCategory(ctx, id))
	cats, _ = svc.ListCategories(ctx)
	require.Empty(t, cats)
}

func TestService_TagAssignment(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:tag", "")
	tagID, err := svc.CreateTag(ctx, "#priority", "#3b82f6")
	require.NoError(t, err)

	require.NoError(t, svc.AssignTag(ctx, string(id), tagID))

	tags, err := svc.ListTagsFor(ctx, string(id))
	require.NoError(t, err)
	require.Len(t, tags, 1)
}

func TestService_SetTorrentCategory(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:cat", "")
	catID, _ := svc.CreateCategory(ctx, "Linux ISOs", "", "#22c55e")

	require.NoError(t, svc.SetTorrentCategory(ctx, string(id), &catID))

	rows, _ := svc.ListTorrents(ctx)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].CategoryID)
	require.Equal(t, catID, *rows[0].CategoryID)
}

func TestService_SetFilePriorities(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:fp", "")

	require.NoError(t, svc.SetFilePriorities(ctx, string(id), map[int]string{
		0: "high",
		1: "skip",
	}))
}

func TestService_DefaultSavePath_Persistence(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	got, err := svc.GetDefaultSavePath(ctx)
	require.NoError(t, err)
	require.Equal(t, "/tmp/dl", got, "no override yet — falls back to constructor default")

	require.NoError(t, svc.SetDefaultSavePath(ctx, "/Volumes/torrents"))

	got, err = svc.GetDefaultSavePath(ctx)
	require.NoError(t, err)
	require.Equal(t, "/Volumes/torrents", got)
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
