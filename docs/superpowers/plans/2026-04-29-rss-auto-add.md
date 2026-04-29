# Mosaic — Plan 5: RSS Auto-Add

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add RSS feed support so users can paste a feed URL once and have new torrents automatically added when they match user-defined regex filters. After this plan: subscribe to a tracker's RSS feed, set a filter like `^Ubuntu.*amd64\.iso$`, and the next time the feed polls (every N minutes), matching items become torrents in the configured category. The icon rail RSS item becomes functional.

**Architecture:** Backend grows two persistence tables (`rss_feeds`, `rss_filters` — already in spec §6 schema), a `RSSPoller` goroutine that ticks at the most-frequent feed interval and fetches each due feed (using ETag/If-Modified-Since to avoid re-downloading unchanged feeds), parses with `feed-rs` (handles RSS + Atom), evaluates each item against the feed's enabled filters in order, and on match calls `Service.AddMagnet` with the resolved magnet URL. We track a per-feed `seen_guids` set in memory (size-capped) so the same item doesn't get re-added. Frontend gets a Settings → RSS pane with feed list + per-feed filter sub-list + "+ Add feed" / "+ Add filter" forms. The icon rail's RSS item becomes a real route (not just settings — full-window RSS view with feeds list + recent matches log).

**Tech additions:**
- `github.com/mmcdole/gofeed` — battle-tested RSS+Atom parser for Go (the design spec named `feed-rs` but that's Rust; `gofeed` is the Go equivalent)
- `regexp` (stdlib) — for filter matching

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §6 (rss_feeds + rss_filters schema), §4 icon rail RSS item.

**Aesthetic continuity:** RSS pane in Settings uses the same Categories/Schedule pattern (list with inline edit forms). Icon-rail RSS view is a proper route (like Settings) with its own layout: header showing total feeds + recent-matches feed, then a feed list with filter counts, click a feed → expand its filters and recent items.

---

## Out of Scope (deferred to Plan 6+)

- **HTTPS+WS remote** — Plan 6
- **Auto-update** — Plan 7
- **Packaging / signing / CI** — Plan 8
- **RSS feed authentication** (cookies, basic auth) — Plan 6+
- **Per-filter save-path override beyond category default** — possible v1 limitation, document

---

## File Structure (final state)

```
backend/
├── persistence/
│   ├── migrations/0005_rss.sql                 # NEW: rss_feeds + rss_filters
│   ├── rss_feeds.go                            # NEW: Feed DAO
│   ├── rss_filters.go                          # NEW: Filter DAO
│   └── *_test.go
└── api/
    ├── rss_poller.go                           # NEW: poller goroutine
    ├── service.go                              # MODIFIED: RSS CRUD + DTOs
    └── *_test.go
app.go                                          # NEW: bindings

frontend/src/
├── lib/
│   ├── bindings.ts                             # MODIFIED
│   └── store.ts                                # MODIFIED: rss state
└── components/
    ├── settings/
    │   ├── RSSPane.tsx                         # NEW: feeds + filters CRUD
    │   ├── SettingsSidebar.tsx                 # MODIFIED: add RSS
    │   └── SettingsRoute.tsx                   # MODIFIED
    └── shell/
        └── IconRail.tsx                        # MODIFIED: RSS no longer stub
```

---

## Section A — Persistence

### Task 1: Migration 0005

`backend/persistence/migrations/0005_rss.sql`:

```sql
-- +goose Up
CREATE TABLE rss_feeds (
  id            INTEGER PRIMARY KEY,
  url           TEXT NOT NULL,
  name          TEXT NOT NULL,
  interval_min  INTEGER NOT NULL DEFAULT 30,
  last_polled   INTEGER NOT NULL DEFAULT 0,  -- unix seconds
  etag          TEXT NOT NULL DEFAULT '',
  enabled       INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE rss_filters (
  id           INTEGER PRIMARY KEY,
  feed_id      INTEGER NOT NULL REFERENCES rss_feeds(id) ON DELETE CASCADE,
  regex        TEXT NOT NULL,
  category_id  INTEGER REFERENCES categories(id),
  save_path    TEXT,
  enabled      INTEGER NOT NULL DEFAULT 1
);

-- +goose Down
DROP TABLE rss_filters;
DROP TABLE rss_feeds;
```

- [ ] Update `TestOpen_RunsMigrations` to assert both tables exist. Commit: `feat(persistence): migration 0005 — rss_feeds + rss_filters`.

---

### Task 2: Feed DAO

`backend/persistence/rss_feeds.go`:

```go
type Feed struct {
	ID          int
	URL         string
	Name        string
	IntervalMin int
	LastPolled  time.Time
	ETag        string
	Enabled     bool
}

type Feeds struct{ db *DB }

func NewFeeds(db *DB) *Feeds { return &Feeds{db: db} }

// Standard CRUD: Create, Get, List (ordered by name), Update, Delete, UpdatePollResult(id, lastPolled, etag).
```

Implement Create/Get/List/Update/Delete + `UpdatePollResult(ctx, id, lastPolled time.Time, etag string)`. Tests mirror Categories pattern.

- [ ] Commit: `feat(persistence): Feeds DAO`.

---

### Task 3: Filter DAO

`backend/persistence/rss_filters.go`:

```go
type Filter struct {
	ID         int
	FeedID     int
	Regex      string
	CategoryID *int  // nullable
	SavePath   string  // empty = use category default or service default
	Enabled    bool
}

type Filters struct{ db *DB }

// Standard CRUD plus ListByFeed(ctx, feedID) []Filter.
```

Tests for round-trip + ListByFeed + ON DELETE CASCADE (deleting a feed cleans up its filters).

- [ ] Commit: `feat(persistence): Filters DAO with ListByFeed + cascade verification`.

---

## Section B — RSS Poller

### Task 4: Add gofeed dep

```bash
go get github.com/mmcdole/gofeed@latest
```

Confirm `go build ./...` clean. Commit: `chore: add gofeed for RSS+Atom parsing`.

---

### Task 5: RSSPoller goroutine

`backend/api/rss_poller.go`:

```go
package api

import (
	"context"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/rs/zerolog/log"

	"mosaic/backend/persistence"
)

type RSSPoller struct {
	svc       *Service
	feeds     *persistence.Feeds
	filters   *persistence.Filters
	parser    *gofeed.Parser
	httpC     *http.Client

	mu        sync.Mutex
	seenByID  map[int]map[string]struct{}  // feedID → set of seen guids/links

	stop chan struct{}
}

func NewRSSPoller(svc *Service, feeds *persistence.Feeds, filters *persistence.Filters) *RSSPoller {
	p := &RSSPoller{
		svc: svc, feeds: feeds, filters: filters,
		parser:   gofeed.NewParser(),
		httpC:    &http.Client{Timeout: 30 * time.Second},
		seenByID: make(map[int]map[string]struct{}),
		stop:     make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *RSSPoller) Close() { close(p.stop) }

func (p *RSSPoller) run() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	p.tick(context.Background())
	for {
		select {
		case <-p.stop: return
		case <-t.C: p.tick(context.Background())
		}
	}
}

func (p *RSSPoller) tick(ctx context.Context) {
	feeds, err := p.feeds.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("rss_poller: list feeds")
		return
	}
	now := time.Now()
	for _, f := range feeds {
		if !f.Enabled { continue }
		dueAt := f.LastPolled.Add(time.Duration(f.IntervalMin) * time.Minute)
		if now.Before(dueAt) { continue }
		if err := p.pollOne(ctx, f); err != nil {
			log.Warn().Err(err).Int("feed_id", f.ID).Str("name", f.Name).Msg("rss_poller: poll failed")
		}
	}
}

func (p *RSSPoller) pollOne(ctx context.Context, f persistence.Feed) error {
	req, err := http.NewRequestWithContext(ctx, "GET", f.URL, nil)
	if err != nil { return err }
	if f.ETag != "" { req.Header.Set("If-None-Match", f.ETag) }
	if !f.LastPolled.IsZero() { req.Header.Set("If-Modified-Since", f.LastPolled.UTC().Format(http.TimeFormat)) }

	resp, err := p.httpC.Do(req)
	if err != nil {
		_ = p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag)
	}
	if resp.StatusCode >= 400 {
		_ = p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag)
		return fmt.Errorf("rss: HTTP %d", resp.StatusCode)
	}

	feed, err := p.parser.Parse(resp.Body)
	if err != nil {
		_ = p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag)
		return err
	}

	filters, err := p.filters.ListByFeed(ctx, f.ID)
	if err != nil { return err }
	enabledFilters := filterEnabled(filters)

	matched := 0
	p.mu.Lock()
	if p.seenByID[f.ID] == nil { p.seenByID[f.ID] = make(map[string]struct{}) }
	seen := p.seenByID[f.ID]
	p.mu.Unlock()

	for _, item := range feed.Items {
		key := item.GUID
		if key == "" { key = item.Link }
		if key == "" { continue }
		if _, alreadySeen := seen[key]; alreadySeen { continue }

		mark := func() { p.mu.Lock(); seen[key] = struct{}{}; if len(seen) > 1000 { /* simple cap; reset on overflow */ p.seenByID[f.ID] = make(map[string]struct{}); }; p.mu.Unlock() }

		for _, fil := range enabledFilters {
			re, err := regexp.Compile(fil.Regex)
			if err != nil { continue }
			if !re.MatchString(item.Title) { continue }
			magnet := extractMagnet(item)
			if magnet == "" { mark(); break }
			savePath := fil.SavePath
			id, err := p.svc.AddMagnet(ctx, magnet, savePath)
			if err != nil {
				log.Warn().Err(err).Str("title", item.Title).Msg("rss: add magnet failed")
				continue
			}
			if fil.CategoryID != nil {
				_ = p.svc.SetTorrentCategory(ctx, string(id), fil.CategoryID)
			}
			matched++
			mark()
			break // first matching filter wins
		}
	}

	etag := resp.Header.Get("ETag")
	if etag == "" { etag = f.ETag } // preserve previous if server didn't send

	if matched > 0 {
		log.Info().Int("feed_id", f.ID).Int("matched", matched).Msg("rss: matched items added")
	}
	return p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), etag)
}

func filterEnabled(all []persistence.Filter) []persistence.Filter {
	out := make([]persistence.Filter, 0, len(all))
	for _, f := range all { if f.Enabled { out = append(out, f) } }
	return out
}

func extractMagnet(item *gofeed.Item) string {
	// Try common locations: enclosure URL if magnet, item.Link if magnet, item.Description for href, custom <torrent:magnetURI> in extensions
	if item.Link != "" && len(item.Link) > 7 && item.Link[:7] == "magnet:" {
		return item.Link
	}
	for _, enc := range item.Enclosures {
		if enc.URL != "" && len(enc.URL) > 7 && enc.URL[:7] == "magnet:" { return enc.URL }
	}
	// torznab/jackett-style: magnet in <torrent:magnetURI> or <torznab:attr name="magneturl" />
	for _, exts := range item.Extensions {
		for _, ext := range exts {
			if ext.Name == "magnetURI" && ext.Value != "" { return ext.Value }
			for _, child := range ext.Children {
				for _, c := range child {
					if c.Attrs["name"] == "magneturl" && c.Attrs["value"] != "" { return c.Attrs["value"] }
				}
			}
		}
	}
	return ""
}
```

Add `"fmt"` import. Tests with httptest.Server + a fixture RSS XML body. Commit: `feat(api): RSSPoller goroutine with ETag + regex filters`.

---

### Task 6: Service RSS CRUD methods + DTOs

In `backend/api/service.go`:

```go
type FeedDTO struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	IntervalMin int    `json:"interval_min"`
	LastPolled  int64  `json:"last_polled"`
	ETag        string `json:"etag"`
	Enabled     bool   `json:"enabled"`
}

type FilterDTO struct {
	ID         int    `json:"id"`
	FeedID     int    `json:"feed_id"`
	Regex      string `json:"regex"`
	CategoryID *int   `json:"category_id"`
	SavePath   string `json:"save_path"`
	Enabled    bool   `json:"enabled"`
}

// Service struct gains: feeds *persistence.Feeds, filters *persistence.Filters
// NewService takes both; main.go constructs.
//
// Methods: ListFeeds, CreateFeed, UpdateFeed, DeleteFeed, ListFiltersByFeed,
// CreateFilter, UpdateFilter, DeleteFilter — total 8.
```

Tests for CRUD round-trip. Commit: `feat(api): RSS feed + filter CRUD methods`.

---

### Task 7: Wails bindings + main.go wiring

App.go gains 8 bindings. main.go constructs `feeds := persistence.NewFeeds(db)`, `filters := persistence.NewFilters(db)`, `poller := api.NewRSSPoller(svc, feeds, filters)` (after svc), `defer poller.Close()`. Regenerate Wails. Commit: `feat: Wails bindings + main wiring for RSS`.

---

## Section C — Frontend

### Task 8: Bindings + store

bindings.ts: FeedDTO, FilterDTO, 8 api methods. store.ts: feeds, filtersByFeed: Record<number, FilterDTO[]>; initial fetch of feeds; lazy fetch filters when a feed is expanded; mutating methods. Commit: `feat(frontend): bindings + store for RSS feeds + filters`.

---

### Task 9: RSSPane (Settings)

`frontend/src/components/settings/RSSPane.tsx`. Mirrors Categories pattern. List of feeds. Each feed row expands to show its filters with a `+ Add filter` button. Inline forms for both. Commit: `feat(frontend): RSSPane with feed + filter CRUD`.

---

### Task 10: Sidebar + Route + IconRail

SettingsSidebar adds 'rss' (Rss icon) between Schedule and Categories. SettingsRoute Match. App.tsx threads. IconRail's `rss` item drops the `soon` annotation — clicking it sets view to a new `'rss'` AppView OR (simpler v1) routes to Settings → RSS pane. Plan picks the simpler v1: clicking IconRail RSS sets `store.setView('settings')` AND `setSettingsPane('rss')`. Commit: `feat(frontend): SettingsRoute wires RSS; IconRail RSS routes to Settings RSS pane`.

> If a separate top-level RSS view becomes desirable later (with recent-matches log + feed health dashboard), Plan 6+ can promote it. v1 keeps it as a Settings sub-pane for simplicity.

---

## Section D — Smoke

### Task 11: User-driven smoke

- [ ] Run `wails dev -skipembedcreate`
- [ ] Settings → RSS → New feed: name "Ubuntu", URL `https://torrent.ubuntu.com/rss.xml`, interval 30. Save.
- [ ] Add filter to that feed: regex `(?i)24\.04.*amd64`, category Linux ISOs, enabled.
- [ ] Wait ≤1 minute for first poll. Verify last-polled timestamp updates. If a matching item is in the feed, a torrent gets added to the Linux ISOs category.
- [ ] Disable feed → wait → confirm no new poll attempts.
- [ ] Tag `plan-5-rss-complete`, push.

---

**End of Plan 5.**
