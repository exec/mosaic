package api

import (
	"context"
	"fmt"
	"regexp"

	"mosaic/backend/persistence"
)

// This file groups the feed-and-filter slice of the Service surface: CRUD on
// RSS feed subscriptions and per-feed regex filters, plus the thin delegation
// methods that route into the RSSPoller for on-demand polls and item fetches.
//
// Methods stay on *Service so the public API surface and Wails bindings are
// unchanged; this is purely a navigational extraction from the 2.5k-line
// service.go god file. The RSSPoller itself depends only on the narrow
// TorrentAdder interface (see torrent_adder.go), so the circular
// poller-needs-Service / Service-needs-poller import smell is gone — what
// remains is just bidirectional struct references during construction, and
// bootstrap.Init wires them up in the only correct order.

// FeedItemDTO is a single item from a live-fetched RSS feed.
type FeedItemDTO struct {
	GUID       string `json:"guid"`
	Title      string `json:"title"`
	PubDate    string `json:"pub_date"`
	TorrentURL string `json:"torrent_url"` // magnet: URI or https://…torrent; empty if unresolvable
}

// FeedDTO is the transport shape for an RSS/Atom feed subscription.
type FeedDTO struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	IntervalMin int    `json:"interval_min"`
	LastPolled  int64  `json:"last_polled"`
	ETag        string `json:"etag"`
	Enabled     bool   `json:"enabled"`
}

// FilterDTO is the transport shape for a per-feed regex filter rule.
type FilterDTO struct {
	ID         int    `json:"id"`
	FeedID     int    `json:"feed_id"`
	Regex      string `json:"regex"`
	CategoryID *int   `json:"category_id"`
	SavePath   string `json:"save_path"`
	Enabled    bool   `json:"enabled"`
}

func toFeedDTO(f persistence.Feed) FeedDTO {
	dto := FeedDTO{
		ID: f.ID, URL: f.URL, Name: f.Name, IntervalMin: f.IntervalMin,
		ETag: f.ETag, Enabled: f.Enabled,
	}
	if !f.LastPolled.IsZero() {
		dto.LastPolled = f.LastPolled.Unix()
	}
	return dto
}

func toFilterDTO(f persistence.Filter) FilterDTO {
	return FilterDTO{
		ID: f.ID, FeedID: f.FeedID, Regex: f.Regex, CategoryID: f.CategoryID,
		SavePath: f.SavePath, Enabled: f.Enabled,
	}
}

// ─── RSSPoller delegation ─────────────────────────────────────────────────

// AttachRSSPoller wires the live *RSSPoller into the Service so that
// PollFeedNow and the SPA's "refresh" button on a feed row can trigger an
// immediate poll. bootstrap.Init calls this once at startup; the poller and
// Service live in the same package so this is purely a back-reference, not a
// layering escape hatch.
func (s *Service) AttachRSSPoller(p *RSSPoller) {
	s.rssPoller = p
}

// PollFeedNow polls a single RSS feed immediately, bypassing its
// scheduled interval. Used by the SPA's per-row refresh icon. Returns
// an error if the poller hasn't been attached or the feed lookup /
// HTTP fetch fails.
func (s *Service) PollFeedNow(ctx context.Context, feedID int) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	if s.rssPoller == nil {
		return fmt.Errorf("rss poller not attached")
	}
	return s.rssPoller.PollNow(ctx, feedID)
}

// GetFeedItems fetches the feed live and returns all its browsable items.
func (s *Service) GetFeedItems(ctx context.Context, feedID int) ([]FeedItemDTO, error) {
	if !CallerFrom(ctx).CanManageRSS() {
		return nil, ErrForbidden
	}
	if s.rssPoller == nil {
		return nil, fmt.Errorf("rss poller not attached")
	}
	return s.rssPoller.GetFeedItems(ctx, feedID)
}

// AddFeedItem adds a torrent from a URL (magnet URI or direct .torrent link)
// sourced from a feed item. Uses the default save path when savePath is empty.
func (s *Service) AddFeedItem(ctx context.Context, torrentURL, savePath string) (string, error) {
	if !CallerFrom(ctx).CanAddTorrents() {
		return "", ErrForbidden
	}
	if s.rssPoller == nil {
		return "", fmt.Errorf("rss poller not attached")
	}
	return s.rssPoller.AddFeedItem(ctx, torrentURL, savePath)
}

// ─── Feed CRUD ────────────────────────────────────────────────────────────

func (s *Service) ListFeeds(ctx context.Context) ([]FeedDTO, error) {
	if s.feeds == nil {
		return nil, nil
	}
	rows, err := s.feeds.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FeedDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, toFeedDTO(r))
	}
	return out, nil
}

func (s *Service) CreateFeed(ctx context.Context, dto FeedDTO) (int, error) {
	if !CallerFrom(ctx).CanManageRSS() {
		return 0, ErrForbidden
	}
	if _, err := validateFetchURL(dto.URL); err != nil {
		return 0, fmt.Errorf("feed URL must be http or https and not point at a private/loopback address: %w", err)
	}
	return s.feeds.Create(ctx, persistence.Feed{
		URL: dto.URL, Name: dto.Name, IntervalMin: dto.IntervalMin,
		ETag: dto.ETag, Enabled: dto.Enabled,
	})
}

func (s *Service) UpdateFeed(ctx context.Context, dto FeedDTO) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	if _, err := validateFetchURL(dto.URL); err != nil {
		return fmt.Errorf("feed URL must be http or https and not point at a private/loopback address: %w", err)
	}
	return s.feeds.Update(ctx, persistence.Feed{
		ID: dto.ID, URL: dto.URL, Name: dto.Name, IntervalMin: dto.IntervalMin,
		Enabled: dto.Enabled,
	})
}

func (s *Service) DeleteFeed(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	return s.feeds.Delete(ctx, id)
}

// ─── Filter CRUD ──────────────────────────────────────────────────────────

func (s *Service) ListFiltersByFeed(ctx context.Context, feedID int) ([]FilterDTO, error) {
	if s.filters == nil {
		return nil, nil
	}
	rows, err := s.filters.ListByFeed(ctx, feedID)
	if err != nil {
		return nil, err
	}
	out := make([]FilterDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, toFilterDTO(r))
	}
	return out, nil
}

// validateFilterRegex rejects patterns that won't compile. The poller
// compiles each filter per poll and skips (with a log) any that fail — so an
// invalid pattern stored here would be accepted silently and simply never
// match. The message prefix is registered in remote/handlers.go's
// userFacingValidationPrefixes so the SPA gets a 400 with the parse error.
func validateFilterRegex(pattern string) error {
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("filter regex is invalid: %v", err)
	}
	return nil
}

func (s *Service) CreateFilter(ctx context.Context, dto FilterDTO) (int, error) {
	if !CallerFrom(ctx).CanManageRSS() {
		return 0, ErrForbidden
	}
	if err := validateFilterRegex(dto.Regex); err != nil {
		return 0, err
	}
	return s.filters.Create(ctx, persistence.Filter{
		FeedID: dto.FeedID, Regex: dto.Regex, CategoryID: dto.CategoryID,
		SavePath: dto.SavePath, Enabled: dto.Enabled,
	})
}

func (s *Service) UpdateFilter(ctx context.Context, dto FilterDTO) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	if err := validateFilterRegex(dto.Regex); err != nil {
		return err
	}
	return s.filters.Update(ctx, persistence.Filter{
		ID: dto.ID, FeedID: dto.FeedID, Regex: dto.Regex, CategoryID: dto.CategoryID,
		SavePath: dto.SavePath, Enabled: dto.Enabled,
	})
}

func (s *Service) DeleteFilter(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanManageRSS() {
		return ErrForbidden
	}
	return s.filters.Delete(ctx, id)
}
