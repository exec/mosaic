package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/rs/zerolog/log"

	"mosaic/backend/persistence"
)

// RSSPoller fetches each enabled feed at its configured interval, parses items,
// matches their titles against per-feed regex filters, and on first match adds
// the resolved magnet URI as a torrent (optionally tagged with a category).
type RSSPoller struct {
	svc     *Service
	feeds   *persistence.Feeds
	filters *persistence.Filters
	parser  *gofeed.Parser
	httpC   *http.Client

	mu       sync.Mutex
	seenByID map[int]map[string]struct{} // feedID → set of seen guids/links

	stop chan struct{}
}

const rssSeenCap = 1000

// NewRSSPoller starts a goroutine that ticks every 60 seconds and polls feeds
// whose LastPolled + IntervalMin has elapsed. Call Close() to stop the poller.
func NewRSSPoller(svc *Service, feeds *persistence.Feeds, filters *persistence.Filters) *RSSPoller {
	p := &RSSPoller{
		svc: svc, feeds: feeds, filters: filters,
		parser:   gofeed.NewParser(),
		httpC:    safeHTTPClient(30 * time.Second),
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
		case <-p.stop:
			return
		case <-t.C:
			p.tick(context.Background())
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
		if !f.Enabled {
			continue
		}
		dueAt := f.LastPolled.Add(time.Duration(f.IntervalMin) * time.Minute)
		if now.Before(dueAt) {
			continue
		}
		if err := p.pollOne(ctx, f); err != nil {
			log.Warn().Err(err).Int("feed_id", f.ID).Str("name", f.Name).Msg("rss_poller: poll failed")
		}
	}
}

func (p *RSSPoller) pollOne(ctx context.Context, f persistence.Feed) error {
	if _, err := validateFetchURL(f.URL); err != nil {
		return fmt.Errorf("rss: refusing to fetch %q: %w", f.Name, err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", f.URL, nil)
	if err != nil {
		return err
	}
	if f.ETag != "" {
		req.Header.Set("If-None-Match", f.ETag)
	}
	if !f.LastPolled.IsZero() && f.LastPolled.Unix() > 0 {
		req.Header.Set("If-Modified-Since", f.LastPolled.UTC().Format(http.TimeFormat))
	}

	resp, err := p.httpC.Do(req)
	if err != nil {
		if uerr := p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag); uerr != nil {
			log.Warn().Err(uerr).Int("feed", f.ID).Msg("rss: update poll result failed")
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag)
	}
	if resp.StatusCode >= 400 {
		if uerr := p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag); uerr != nil {
			log.Warn().Err(uerr).Int("feed", f.ID).Msg("rss: update poll result failed")
		}
		return fmt.Errorf("rss: HTTP %d", resp.StatusCode)
	}

	feed, err := p.parser.Parse(resp.Body)
	if err != nil {
		if uerr := p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), f.ETag); uerr != nil {
			log.Warn().Err(uerr).Int("feed", f.ID).Msg("rss: update poll result failed")
		}
		return err
	}

	filters, err := p.filters.ListByFeed(ctx, f.ID)
	if err != nil {
		return err
	}
	enabledFilters := filterEnabled(filters)

	matched := 0
	for _, item := range feed.Items {
		key := item.GUID
		if key == "" {
			key = item.Link
		}
		if key == "" {
			continue
		}
		if p.alreadySeen(f.ID, key) {
			continue
		}

		for _, fil := range enabledFilters {
			re, err := regexp.Compile(fil.Regex)
			if err != nil {
				continue
			}
			if !re.MatchString(item.Title) {
				continue
			}
			magnet := extractMagnet(item)
			if magnet == "" {
				p.markSeen(f.ID, key)
				break
			}
			id, err := p.svc.AddMagnet(ctx, magnet, fil.SavePath)
			if err != nil {
				log.Warn().Err(err).Str("title", item.Title).Msg("rss: add magnet failed")
				continue
			}
			if fil.CategoryID != nil {
				if cerr := p.svc.SetTorrentCategory(ctx, string(id), fil.CategoryID); cerr != nil {
					log.Warn().Err(cerr).Str("title", item.Title).Int("category", *fil.CategoryID).Msg("rss: assign category failed")
				}
			}
			matched++
			p.markSeen(f.ID, key)
			break // first matching filter wins
		}
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		etag = f.ETag // preserve previous if server didn't send
	}

	if matched > 0 {
		log.Info().Int("feed_id", f.ID).Int("matched", matched).Msg("rss: matched items added")
	}
	return p.feeds.UpdatePollResult(ctx, f.ID, time.Now(), etag)
}

func (p *RSSPoller) alreadySeen(feedID int, key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seenByID[feedID] == nil {
		return false
	}
	_, ok := p.seenByID[feedID][key]
	return ok
}

func (p *RSSPoller) markSeen(feedID int, key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seenByID[feedID] == nil {
		p.seenByID[feedID] = make(map[string]struct{})
	}
	if len(p.seenByID[feedID]) >= rssSeenCap {
		p.seenByID[feedID] = make(map[string]struct{})
	}
	p.seenByID[feedID][key] = struct{}{}
}

func filterEnabled(all []persistence.Filter) []persistence.Filter {
	out := make([]persistence.Filter, 0, len(all))
	for _, f := range all {
		if f.Enabled {
			out = append(out, f)
		}
	}
	return out
}

// extractMagnet locates a magnet URI on the gofeed item. We check, in order:
// item.Link, each enclosure URL, and any extension element named "magnetURI"
// or torznab "magneturl" attribute.
func extractMagnet(item *gofeed.Item) string {
	if isMagnet(item.Link) {
		return item.Link
	}
	for _, enc := range item.Enclosures {
		if enc != nil && isMagnet(enc.URL) {
			return enc.URL
		}
	}
	for _, exts := range item.Extensions {
		for _, list := range exts {
			for _, ext := range list {
				if ext.Name == "magnetURI" && isMagnet(ext.Value) {
					return ext.Value
				}
				if v, ok := ext.Attrs["value"]; ok && ext.Attrs["name"] == "magneturl" && isMagnet(v) {
					return v
				}
				for _, children := range ext.Children {
					for _, c := range children {
						if c.Name == "magnetURI" && isMagnet(c.Value) {
							return c.Value
						}
						if v, ok := c.Attrs["value"]; ok && c.Attrs["name"] == "magneturl" && isMagnet(v) {
							return v
						}
					}
				}
			}
		}
	}
	return ""
}

func isMagnet(s string) bool {
	return strings.HasPrefix(s, "magnet:")
}
