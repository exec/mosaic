package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/require"

	"mosaic/backend/persistence"
)

// rssFeedXML is a minimal RSS 2.0 body where each <item> has a magnet link as
// its <link>. Two items so we can test selective regex matching.
const rssFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <link>https://example.test/</link>
    <description>fixture</description>
    <item>
      <title>Ubuntu 24.04 amd64.iso</title>
      <link>magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</link>
      <guid>ubuntu-2404-amd64</guid>
    </item>
    <item>
      <title>Fedora 40 x86_64.iso</title>
      <link>magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb</link>
      <guid>fedora-40</guid>
    </item>
  </channel>
</rss>`

func newPollerForTest(t *testing.T) (*RSSPoller, *Service, *persistence.Feeds, *persistence.Filters) {
	t.Helper()
	svc, _ := newTestService(t)
	// Reach into the same DB the service uses for direct feed/filter inserts.
	// newTestService gives us a Service backed by its own DB; we don't have a
	// handle to it, so build a fresh DB for poller dependencies. The poller's
	// SetTorrentCategory / AddMagnet calls go through svc.* which uses svc's
	// own DB, but tests only assert on the engine list (not the categories).
	feeds, filters := newRSSDAOs(t)
	p := &RSSPoller{
		svc:      svc,
		feeds:    feeds,
		filters:  filters,
		parser:   gofeed.NewParser(),
		httpC:    &http.Client{Timeout: 5 * time.Second},
		seenByID: make(map[int]map[string]struct{}),
		stop:     make(chan struct{}),
	}
	return p, svc, feeds, filters
}

func newRSSDAOs(t *testing.T) (*persistence.Feeds, *persistence.Filters) {
	t.Helper()
	db, err := persistence.Open(context.Background(), t.TempDir()+"/rss.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return persistence.NewFeeds(db), persistence.NewFilters(db)
}

func TestRSSPoller_PollOne_MatchesAndAddsMagnet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssFeedXML))
	}))
	defer srv.Close()

	p, svc, feeds, filters := newPollerForTest(t)
	ctx := context.Background()

	feedID, err := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "test", IntervalMin: 1, Enabled: true})
	require.NoError(t, err)
	_, err = filters.Create(ctx, persistence.Filter{FeedID: feedID, Regex: `(?i)ubuntu.*amd64`, Enabled: true})
	require.NoError(t, err)

	f, err := feeds.Get(ctx, feedID)
	require.NoError(t, err)
	require.NoError(t, p.pollOne(ctx, f))

	rows, err := svc.ListTorrents(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the ubuntu item should match the filter")
	require.Contains(t, rows[0].Magnet, "aaaaaaaaaa")

	// LastPolled and ETag should be updated.
	updated, err := feeds.Get(ctx, feedID)
	require.NoError(t, err)
	require.False(t, updated.LastPolled.IsZero())
	require.Equal(t, `"abc"`, updated.ETag)
}

func TestRSSPoller_PollOne_NotModifiedSkips(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		require.Equal(t, `"abc"`, r.Header.Get("If-None-Match"))
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	p, svc, feeds, filters := newPollerForTest(t)
	ctx := context.Background()

	feedID, err := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "n", IntervalMin: 1, ETag: `"abc"`, Enabled: true})
	require.NoError(t, err)
	_, err = filters.Create(ctx, persistence.Filter{FeedID: feedID, Regex: `.*`, Enabled: true})
	require.NoError(t, err)

	f, err := feeds.Get(ctx, feedID)
	require.NoError(t, err)
	require.NoError(t, p.pollOne(ctx, f))
	require.Equal(t, 1, hits)

	rows, err := svc.ListTorrents(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)

	// LastPolled should still be bumped, ETag preserved.
	updated, err := feeds.Get(ctx, feedID)
	require.NoError(t, err)
	require.False(t, updated.LastPolled.IsZero())
	require.Equal(t, `"abc"`, updated.ETag)
}

func TestRSSPoller_PollOne_DedupViaSeenSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rssFeedXML))
	}))
	defer srv.Close()

	p, svc, feeds, filters := newPollerForTest(t)
	ctx := context.Background()

	feedID, _ := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "n", IntervalMin: 1, Enabled: true})
	_, _ = filters.Create(ctx, persistence.Filter{FeedID: feedID, Regex: `.*`, Enabled: true})

	f, _ := feeds.Get(ctx, feedID)
	require.NoError(t, p.pollOne(ctx, f))
	require.NoError(t, p.pollOne(ctx, f))

	rows, err := svc.ListTorrents(ctx)
	require.NoError(t, err)
	// engine de-dups by infohash; both items are added once on first poll, and
	// the seen-set prevents AddMagnet from running a second time.
	require.Len(t, rows, 2)
}

func TestRSSPoller_Tick_SkipsDisabledAndNotDue(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(rssFeedXML))
	}))
	defer srv.Close()

	p, _, feeds, filters := newPollerForTest(t)
	ctx := context.Background()

	disabledID, _ := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "off", IntervalMin: 1, Enabled: false})
	_, _ = filters.Create(ctx, persistence.Filter{FeedID: disabledID, Regex: `.*`, Enabled: true})

	freshID, _ := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "fresh", IntervalMin: 60, Enabled: true})
	_, _ = filters.Create(ctx, persistence.Filter{FeedID: freshID, Regex: `.*`, Enabled: true})
	require.NoError(t, feeds.UpdatePollResult(ctx, freshID, time.Now(), ""))

	p.tick(ctx)
	require.Equal(t, 0, hits, "neither feed should have been hit")
}

func TestRSSPoller_Tick_PollsDueFeed(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(rssFeedXML))
	}))
	defer srv.Close()

	p, svc, feeds, filters := newPollerForTest(t)
	ctx := context.Background()

	feedID, _ := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "due", IntervalMin: 1, Enabled: true})
	_, _ = filters.Create(ctx, persistence.Filter{FeedID: feedID, Regex: `(?i)ubuntu`, Enabled: true})

	p.tick(ctx)
	require.Equal(t, 1, hits)

	rows, err := svc.ListTorrents(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestRSSPoller_PollOne_HTTPErrorReturnsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, _, feeds, _ := newPollerForTest(t)
	ctx := context.Background()
	feedID, _ := feeds.Create(ctx, persistence.Feed{URL: srv.URL, Name: "n", IntervalMin: 1, Enabled: true})
	f, _ := feeds.Get(ctx, feedID)

	err := p.pollOne(ctx, f)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")

	// LastPolled bumped even on failure so we don't hammer broken feeds.
	updated, _ := feeds.Get(ctx, feedID)
	require.False(t, updated.LastPolled.IsZero())
}

func TestExtractMagnet_FromLink(t *testing.T) {
	got := extractMagnet(&gofeed.Item{Link: "magnet:?xt=urn:btih:zzz"})
	require.Equal(t, "magnet:?xt=urn:btih:zzz", got)
}

func TestExtractMagnet_FromEnclosure(t *testing.T) {
	got := extractMagnet(&gofeed.Item{
		Link:       "https://example.test/info",
		Enclosures: []*gofeed.Enclosure{{URL: "magnet:?xt=urn:btih:enc"}},
	})
	require.Equal(t, "magnet:?xt=urn:btih:enc", got)
}

func TestExtractMagnet_FromTorznabAttr(t *testing.T) {
	// torznab puts the magnet on a <torznab:attr name="magneturl" value="..."/>
	xml := `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <title>tn</title>
    <link>https://example.test/</link>
    <description>x</description>
    <item>
      <title>x</title>
      <link>https://example.test/dl/1</link>
      <guid>1</guid>
      <torznab:attr name="magneturl" value="magnet:?xt=urn:btih:tn"/>
    </item>
  </channel>
</rss>`
	feed, err := gofeed.NewParser().ParseString(xml)
	require.NoError(t, err)
	require.Len(t, feed.Items, 1)
	require.Equal(t, "magnet:?xt=urn:btih:tn", extractMagnet(feed.Items[0]))
}

func TestExtractMagnet_FromMagnetURIElement(t *testing.T) {
	xml := `<?xml version="1.0"?>
<rss version="2.0" xmlns:torrent="http://example.test/torrent">
  <channel>
    <title>t</title>
    <link>https://example.test/</link>
    <description>x</description>
    <item>
      <title>x</title>
      <link>https://example.test/dl/2</link>
      <guid>2</guid>
      <torrent:magnetURI>magnet:?xt=urn:btih:el</torrent:magnetURI>
    </item>
  </channel>
</rss>`
	feed, err := gofeed.NewParser().ParseString(xml)
	require.NoError(t, err)
	require.Len(t, feed.Items, 1)
	require.Equal(t, "magnet:?xt=urn:btih:el", extractMagnet(feed.Items[0]))
}

func TestExtractMagnet_None(t *testing.T) {
	got := extractMagnet(&gofeed.Item{Link: "https://example.test/info"})
	require.Equal(t, "", got)
}

func TestRSSPoller_Close_StopsRunLoop(t *testing.T) {
	p := &RSSPoller{stop: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		<-p.stop
		close(done)
	}()
	p.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not signal stop")
	}
}

// Sanity: rssFeedXML parses to two items with magnet links — guard against
// accidental fixture edits that would mask matching-logic regressions.
func TestRSSFixtureHasTwoMagnetItems(t *testing.T) {
	feed, err := gofeed.NewParser().ParseString(rssFeedXML)
	require.NoError(t, err)
	require.Len(t, feed.Items, 2)
	for _, it := range feed.Items {
		require.True(t, strings.HasPrefix(it.Link, "magnet:"))
	}
}
