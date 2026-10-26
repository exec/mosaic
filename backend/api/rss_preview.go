package api

import (
	"context"
	"fmt"
	"github.com/anacrolix/torrent/metainfo"
	"regexp"
)

type FilterPreviewItem struct {
	FeedItemDTO
	Matches        bool `json:"matches"`
	AlreadyHandled bool `json:"already_handled"`
	InLibrary      bool `json:"in_library"`
}
type FilterPreviewDTO struct {
	Destination string              `json:"destination"`
	Items       []FilterPreviewItem `json:"items"`
}

// Preview uses the same RE2 expression and feed extraction as automatic polling, without adding or marking items.
func (s *Service) PreviewRSSFilter(ctx context.Context, f FilterDTO) (FilterPreviewDTO, error) {
	if !CallerFrom(ctx).CanManageRSS() {
		return FilterPreviewDTO{}, ErrForbidden
	}
	re, err := regexp.Compile(f.Regex)
	if err != nil {
		return FilterPreviewDTO{}, fmt.Errorf("filter regex is invalid: %v", err)
	}
	path, err := s.resolveSavePath(ctx, CallerFrom(ctx), f.SavePath)
	if err != nil {
		return FilterPreviewDTO{}, err
	}
	items, err := s.GetFeedItems(ctx, f.FeedID)
	if err != nil {
		return FilterPreviewDTO{}, err
	}
	records, err := s.ListTorrents(ctx)
	if err != nil {
		return FilterPreviewDTO{}, err
	}
	library := make(map[string]bool, len(records))
	for _, r := range records {
		library[r.ID] = true
	}
	out := FilterPreviewDTO{Destination: path, Items: make([]FilterPreviewItem, 0, len(items))}
	for _, item := range items {
		inLibrary := false
		if m, err := metainfo.ParseMagnetUri(item.TorrentURL); err == nil {
			inLibrary = library[m.InfoHash.HexString()]
		}
		out.Items = append(out.Items, FilterPreviewItem{FeedItemDTO: item, Matches: re.MatchString(item.Title), AlreadyHandled: item.AlreadyHandled, InLibrary: inLibrary})
	}
	return out, nil
}
