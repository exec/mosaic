package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"mosaic/backend/persistence"
)

func TestSettingsBackupValidateRoundTripAndAtomicRollback(t *testing.T) {
	s, _, db := newTestServiceWithDB(t)
	ctx := sysCtx()
	require.NoError(t, s.settings.Set(ctx, settingWebAPIKey, "secret"))
	b, err := s.ExportSettings(ctx)
	require.NoError(t, err)
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
	b.Limits.DownKbps = 512
	b.Queue.MaxActiveDownloads = 4
	b.Peers.ListenPort = 45678
	b.Peers.DHTEnabled = true
	ratio := 2.0
	b.Seeding.RatioLimit = &ratio
	require.NoError(t, s.ImportSettings(ctx, b))
	got, err := s.ExportSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, b, got)
	secret, err := s.settings.Get(ctx, settingWebAPIKey)
	require.NoError(t, err)
	require.Equal(t, "secret", secret)
	b.Limits.DownKbps = -1
	require.Error(t, s.ImportSettings(ctx, b))
	unchanged, err := s.ExportSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 512, unchanged.Limits.DownKbps)
	require.Error(t, s.ImportSettings(ctx, SettingsBackupDTO{Version: 1}))
	b.Limits.DownKbps = 999
	b.Queue.MaxActiveDownloads = 9
	_, err = db.SQL().Exec(`CREATE TRIGGER reject_backup BEFORE UPDATE ON settings WHEN NEW.key='down_kbps' BEGIN SELECT RAISE(ABORT,'failure'); END`)
	require.NoError(t, err)
	require.Error(t, s.ImportSettings(ctx, b))
	unchanged, err = s.ExportSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 512, unchanged.Limits.DownKbps)
	require.Equal(t, 4, unchanged.Queue.MaxActiveDownloads)
	_, err = s.ExportSettings(context.Background())
	require.ErrorIs(t, err, ErrForbidden)
	require.ErrorIs(t, s.ImportSettings(context.Background(), b), ErrForbidden)
	_, err = s.GetConnectionDiagnostics(context.Background())
	require.ErrorIs(t, err, ErrForbidden)
}

func TestRSSPreviewUsesGoMatcherAndNeverAddsOrMarksItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(rssFeedXML)) }))
	defer srv.Close()
	p, s, feeds, _ := newPollerForTest(t)
	s.AttachRSSPoller(p)
	ctx := sysCtx()
	feedID, err := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "test", IntervalMin: 30})
	require.NoError(t, err)
	p.markSeen(ctx, feedID, "ubuntu-2404-amd64")
	f := FilterDTO{FeedID: feedID, Regex: `(?i)ubuntu.*amd64`, SavePath: t.TempDir()}
	result, err := s.PreviewRSSFilter(ctx, f)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.True(t, result.Items[0].Matches)
	require.True(t, result.Items[0].AlreadyHandled)
	require.False(t, result.Items[1].Matches)
	require.Equal(t, f.SavePath, result.Destination)
	require.False(t, p.alreadySeen(feedID, "fedora-40"))
	rows, err := s.ListTorrents(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
	f.Regex = "["
	_, err = s.PreviewRSSFilter(ctx, f)
	require.ErrorContains(t, err, "filter regex is invalid")
	_, err = s.PreviewRSSFilter(context.Background(), f)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestTorrentMetadataExportChecksAccessAndAvailability(t *testing.T) {
	s, _ := newTestService(t)
	ctx := sysCtx()
	id, err := s.AddMagnet(ctx, "magnet:?xt=urn:btih:export", "")
	require.NoError(t, err)
	_, err = s.ExportTorrent(context.Background(), string(id))
	require.Error(t, err)
	_, err = s.ExportTorrent(ctx, string(id))
	require.ErrorContains(t, err, "metadata is unavailable")
	rec, err := s.torrents.Get(ctx, string(id))
	require.NoError(t, err)
	rec.Metainfo = []byte("stored metadata")
	require.NoError(t, s.torrents.Save(ctx, rec))
	data, err := s.ExportTorrent(ctx, string(id))
	require.NoError(t, err)
	bytes, err := base64.StdEncoding.DecodeString(data)
	require.NoError(t, err)
	require.Equal(t, rec.Metainfo, bytes)
}
