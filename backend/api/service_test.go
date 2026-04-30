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
		persistence.NewScheduleRules(db),
		persistence.NewFeeds(db),
		persistence.NewFilters(db),
		nil, // no scheduler in unit tests
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

	id, err := svc.AddTorrentFile(context.Background(), path, "")
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
	_, err := svc.AddTorrentFile(context.Background(), filepath.Join(t.TempDir(), "does-not-exist.torrent"), "")
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

func TestService_LimitsRoundTrip(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.SetLimits(ctx, LimitsDTO{DownKbps: 1000, UpKbps: 100, AltDownKbps: 200, AltUpKbps: 50}))
	got, _ := svc.GetLimits(ctx)
	require.Equal(t, 1000, got.DownKbps)
	require.Equal(t, 200, got.AltDownKbps)
}

func TestService_ToggleAltSpeed(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	on, err := svc.ToggleAltSpeed(ctx)
	require.NoError(t, err)
	require.True(t, on)
	on, _ = svc.ToggleAltSpeed(ctx)
	require.False(t, on)
}

func TestService_QueuePosition(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:qp", "")
	require.NoError(t, svc.SetQueuePosition(ctx, string(id), 7))
	rows, _ := svc.ListTorrents(ctx)
	require.Equal(t, 7, rows[0].QueuePosition)
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

func TestService_FeedCRUD(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, err := svc.CreateFeed(ctx, FeedDTO{URL: "https://x.test/rss", Name: "X", IntervalMin: 30, Enabled: true})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	feeds, err := svc.ListFeeds(ctx)
	require.NoError(t, err)
	require.Len(t, feeds, 1)
	require.Equal(t, "X", feeds[0].Name)
	require.Equal(t, 30, feeds[0].IntervalMin)
	require.True(t, feeds[0].Enabled)

	require.NoError(t, svc.UpdateFeed(ctx, FeedDTO{ID: id, URL: "https://y.test/rss", Name: "Y", IntervalMin: 60, Enabled: false}))
	feeds, _ = svc.ListFeeds(ctx)
	require.Equal(t, "Y", feeds[0].Name)
	require.Equal(t, 60, feeds[0].IntervalMin)
	require.False(t, feeds[0].Enabled)

	require.NoError(t, svc.DeleteFeed(ctx, id))
	feeds, _ = svc.ListFeeds(ctx)
	require.Empty(t, feeds)
}

func TestService_FilterCRUD(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	feedID, err := svc.CreateFeed(ctx, FeedDTO{URL: "https://example.com/rss", Name: "f", IntervalMin: 30, Enabled: true})
	require.NoError(t, err)
	catID, err := svc.CreateCategory(ctx, "ISOs", "/iso", "#3b82f6")
	require.NoError(t, err)

	filterID, err := svc.CreateFilter(ctx, FilterDTO{
		FeedID: feedID, Regex: `(?i)ubuntu.*amd64`, CategoryID: &catID, SavePath: "/x", Enabled: true,
	})
	require.NoError(t, err)
	require.Greater(t, filterID, 0)

	got, err := svc.ListFiltersByFeed(ctx, feedID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, `(?i)ubuntu.*amd64`, got[0].Regex)
	require.NotNil(t, got[0].CategoryID)
	require.Equal(t, catID, *got[0].CategoryID)
	require.Equal(t, "/x", got[0].SavePath)

	require.NoError(t, svc.UpdateFilter(ctx, FilterDTO{
		ID: filterID, FeedID: feedID, Regex: `^debian`, SavePath: "/y", Enabled: false,
	}))
	got, _ = svc.ListFiltersByFeed(ctx, feedID)
	require.Equal(t, `^debian`, got[0].Regex)
	require.Nil(t, got[0].CategoryID)
	require.Equal(t, "/y", got[0].SavePath)
	require.False(t, got[0].Enabled)

	require.NoError(t, svc.DeleteFilter(ctx, filterID))
	got, _ = svc.ListFiltersByFeed(ctx, feedID)
	require.Empty(t, got)
}

func TestService_DeleteFeed_CascadesFilters(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	feedID, _ := svc.CreateFeed(ctx, FeedDTO{URL: "https://example.com/rss", Name: "f", IntervalMin: 30, Enabled: true})
	_, _ = svc.CreateFilter(ctx, FilterDTO{FeedID: feedID, Regex: ".*", Enabled: true})
	_, _ = svc.CreateFilter(ctx, FilterDTO{FeedID: feedID, Regex: "x", Enabled: true})

	pre, _ := svc.ListFiltersByFeed(ctx, feedID)
	require.Len(t, pre, 2)

	require.NoError(t, svc.DeleteFeed(ctx, feedID))
	post, _ := svc.ListFiltersByFeed(ctx, feedID)
	require.Empty(t, post)
}

func TestService_GetWebConfig_Defaults(t *testing.T) {
	svc, _ := newTestService(t)
	cfg := svc.GetWebConfig(context.Background())
	require.False(t, cfg.Enabled)
	require.Equal(t, 8080, cfg.Port)
	require.False(t, cfg.BindAll)
	require.Equal(t, "admin", cfg.Username)
	require.Empty(t, cfg.APIKey)
}

func TestService_SetWebConfig_RoundTrip(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.SetWebConfig(ctx, WebConfigDTO{
		Enabled: true, Port: 9091, BindAll: true, Username: "remote",
	}))
	got := svc.GetWebConfig(ctx)
	require.True(t, got.Enabled)
	require.Equal(t, 9091, got.Port)
	require.True(t, got.BindAll)
	require.Equal(t, "remote", got.Username)
}

func TestService_SetWebPassword_VerifyCredentials(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.SetWebConfig(ctx, WebConfigDTO{
		Enabled: true, Port: 8080, Username: "alice",
	}))
	require.NoError(t, svc.SetWebPassword(ctx, "s3cret"))

	require.True(t, svc.VerifyWebCredentials(ctx, "alice", "s3cret"))
	require.False(t, svc.VerifyWebCredentials(ctx, "alice", "wrong"))
	require.False(t, svc.VerifyWebCredentials(ctx, "bob", "s3cret"))
}

func TestService_VerifyWebCredentials_NoPasswordSet(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.SetWebConfig(ctx, WebConfigDTO{Username: "alice"}))
	require.False(t, svc.VerifyWebCredentials(ctx, "alice", "anything"))
}

func TestService_RotateAPIKey_AndVerify(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	require.False(t, svc.VerifyAPIKey(ctx, "anything"))

	key, err := svc.RotateAPIKey(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, key)

	require.True(t, svc.VerifyAPIKey(ctx, key))
	require.False(t, svc.VerifyAPIKey(ctx, "not-the-key"))
	require.False(t, svc.VerifyAPIKey(ctx, ""))

	// Rotate replaces; old key should no longer verify.
	newKey, err := svc.RotateAPIKey(ctx)
	require.NoError(t, err)
	require.NotEqual(t, key, newKey)
	require.False(t, svc.VerifyAPIKey(ctx, key))
	require.True(t, svc.VerifyAPIKey(ctx, newKey))
}

func TestService_GetWebConfig_ReturnsStoredAPIKey(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	key, err := svc.RotateAPIKey(ctx)
	require.NoError(t, err)
	require.Equal(t, key, svc.GetWebConfig(ctx).APIKey)
}

func TestUpdaterConfig_DefaultsAndRoundTrip(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	cfg := svc.GetUpdaterConfig(ctx)
	require.True(t, cfg.Enabled, "updater should be enabled by default")
	require.Equal(t, "stable", cfg.Channel)
	require.Zero(t, cfg.LastCheckedAt)
	require.Empty(t, cfg.LastSeenVersion)

	require.NoError(t, svc.SetUpdaterConfig(ctx, UpdaterConfigDTO{Enabled: false, Channel: "beta"}))
	got := svc.GetUpdaterConfig(ctx)
	require.False(t, got.Enabled)
	require.Equal(t, "beta", got.Channel)
}

func TestUpdaterConfig_RejectsUnknownChannel(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	err := svc.SetUpdaterConfig(ctx, UpdaterConfigDTO{Enabled: true, Channel: "nightly"})
	require.Error(t, err)
}

func TestCheckForUpdate_NoUpdater(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	info, err := svc.CheckForUpdate(ctx)
	require.Error(t, err, "expected updater-disabled error")
	require.False(t, info.Available)
	require.Empty(t, info.CurrentVersion, "appVersion is unset on a fresh test service")
}

func TestInstallUpdate_NoUpdater(t *testing.T) {
	svc, _ := newTestService(t)
	require.Error(t, svc.InstallUpdate(context.Background()))
}

// serviceWithDB lets two Service instances share one DB file across the test
// (to simulate a process restart).
func serviceWithDB(t *testing.T, dbPath string) (*Service, *engine.FakeBackend) {
	t.Helper()
	db, err := persistence.Open(context.Background(), dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fb := engine.NewFakeBackend()
	eng := engine.NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })
	return NewService(eng,
		persistence.NewTorrents(db),
		persistence.NewCategories(db),
		persistence.NewTags(db),
		persistence.NewSettings(db),
		persistence.NewScheduleRules(db),
		persistence.NewFeeds(db),
		persistence.NewFilters(db),
		nil,
		"/tmp/dl"), fb
}

func TestDesktopIntegration_DefaultsOnFreshDB(t *testing.T) {
	svc, _ := newTestService(t)
	got := svc.GetDesktopIntegration(context.Background())

	// Spec defaults: tray on, close-to-tray off, start-minimized off,
	// all three notification toggles on. Frontend depends on these.
	require.True(t, got.TrayEnabled, "TrayEnabled should default to true")
	require.False(t, got.CloseToTray, "CloseToTray should default to false")
	require.False(t, got.StartMinimized, "StartMinimized should default to false")
	require.True(t, got.NotifyOnComplete)
	require.True(t, got.NotifyOnError)
	require.True(t, got.NotifyOnUpdate)
}

func TestDesktopIntegration_SetRoundtrips(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	in := DesktopIntegrationDTO{
		TrayEnabled:      false,
		CloseToTray:      true,
		StartMinimized:   true,
		NotifyOnComplete: false,
		NotifyOnError:    true,
		NotifyOnUpdate:   false,
	}
	require.NoError(t, svc.SetDesktopIntegration(ctx, in))

	got := svc.GetDesktopIntegration(ctx)
	require.Equal(t, in, got)
}

// TestDesktopIntegration_AllFalseAccepted documents that we don't validate
// the combo: a user disabling every toggle is a legitimate choice (silence
// the app entirely).
func TestDesktopIntegration_AllFalseAccepted(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// All-false combo — no validation, no error.
	require.NoError(t, svc.SetDesktopIntegration(ctx, DesktopIntegrationDTO{}))

	got := svc.GetDesktopIntegration(ctx)
	require.False(t, got.TrayEnabled)
	require.False(t, got.CloseToTray)
	require.False(t, got.StartMinimized)
	require.False(t, got.NotifyOnComplete)
	require.False(t, got.NotifyOnError)
	require.False(t, got.NotifyOnUpdate)
}

func TestDesktopIntegration_OnChangeFiresWithLatest(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	var seen DesktopIntegrationDTO
	called := 0
	svc.OnDesktopIntegrationChange(func(c DesktopIntegrationDTO) {
		seen = c
		called++
	})

	in := DesktopIntegrationDTO{TrayEnabled: true, NotifyOnError: true}
	require.NoError(t, svc.SetDesktopIntegration(ctx, in))
	require.Equal(t, 1, called)
	require.Equal(t, in, seen)
}

func TestRestoreOnStartup_ReAddsPersistedMagnet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")

	// Session 1: add a magnet + persist it.
	svc1, fb1 := serviceWithDB(t, dbPath)
	_, err := svc1.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc", "")
	require.NoError(t, err)
	require.Len(t, fb1.List(), 1)

	// Session 2: fresh engine (empty), same DB. Restore should re-add.
	svc2, fb2 := serviceWithDB(t, dbPath)
	require.Empty(t, fb2.List(), "fresh engine starts empty")
	require.NoError(t, svc2.RestoreOnStartup(context.Background()))
	require.Len(t, fb2.List(), 1, "magnet torrent should be re-added")
}

func TestRestoreOnStartup_ReAddsPersistedFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")

	svc1, fb1 := serviceWithDB(t, dbPath)
	// FakeBackend.AddFile records the bytes verbatim; any non-empty blob suffices.
	_, err := svc1.AddTorrentBytes(context.Background(), []byte("fake-torrent-bytes"), "")
	require.NoError(t, err)
	require.Len(t, fb1.List(), 1)

	svc2, fb2 := serviceWithDB(t, dbPath)
	require.Empty(t, fb2.List())
	require.NoError(t, svc2.RestoreOnStartup(context.Background()))
	require.Len(t, fb2.List(), 1, "file-added torrent should be re-added via persisted metainfo")
}

func TestRestoreOnStartup_SkipsOrphanRecord(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	svc, fb := serviceWithDB(t, dbPath)

	// Insert a record with neither magnet nor metainfo (the pre-fix legacy state).
	require.NoError(t, svc.torrents.Save(context.Background(), persistence.TorrentRecord{
		InfoHash: "deadbeef", Name: "orphan", SavePath: "/tmp/dl", AddedAt: time.Now(),
	}))

	require.NoError(t, svc.RestoreOnStartup(context.Background()))
	require.Empty(t, fb.List(), "orphan should be skipped, not crash")
}
