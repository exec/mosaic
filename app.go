package main

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"mosaic/backend/api"
	"mosaic/backend/engine"
	"mosaic/backend/remote"
)

// App is the Wails-bound type. Methods on App become callable from the
// frontend via the auto-generated bindings in frontend/wailsjs/.
type App struct {
	svc *api.Service
	hub *remote.Hub // optional fan-out for browser clients; nil-safe
	ctx context.Context
}

func NewApp(svc *api.Service, hub *remote.Hub) *App {
	return &App{svc: svc, hub: hub}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.streamTicks(ctx)
}

// AddMagnet adds a magnet link. Returns the torrent ID.
func (a *App) AddMagnet(magnet, savePath string) (string, error) {
	id, err := a.svc.AddMagnet(a.ctx, magnet, savePath)
	if err != nil {
		return "", err
	}
	return string(id), nil
}

// ListTorrents returns the current list as DTOs.
func (a *App) ListTorrents() ([]api.TorrentDTO, error) {
	return a.svc.ListTorrents(a.ctx)
}

// PickAndAddTorrent opens a native file dialog, lets the user choose a
// .torrent file, and adds it to the engine + persistence. Returns the new
// torrent ID, or "" if the user cancelled.
func (a *App) PickAndAddTorrent(savePath string) (string, error) {
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select .torrent file",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Torrent files (*.torrent)", Pattern: "*.torrent"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" { // user cancelled
		return "", nil
	}
	id, err := a.svc.AddTorrentFile(a.ctx, path, savePath)
	if err != nil {
		return "", err
	}
	return string(id), nil
}

// Pause/Resume/Remove operate by id.
func (a *App) Pause(id string) error  { return a.svc.Pause(engine.TorrentID(id)) }
func (a *App) Resume(id string) error { return a.svc.Resume(engine.TorrentID(id)) }
func (a *App) Remove(id string, deleteFiles bool) error {
	return a.svc.Remove(a.ctx, engine.TorrentID(id), deleteFiles)
}

func (a *App) GlobalStats() (api.GlobalStats, error) {
	return a.svc.GlobalStats(a.ctx)
}

// SetInspectorFocus tells the backend the inspector is open on torrent `id`
// with `tabs` visible. The next inspector:tick (and subsequent ticks at 1Hz)
// will include data scoped to those tabs.
func (a *App) SetInspectorFocus(id string, tabs []string) error {
	return a.svc.SetInspectorFocus(id, tabs)
}

// ClearInspectorFocus stops inspector:tick emission until SetInspectorFocus is called again.
func (a *App) ClearInspectorFocus() {
	a.svc.ClearInspectorFocus()
}

func (a *App) ListCategories() ([]api.CategoryDTO, error) {
	return a.svc.ListCategories(a.ctx)
}

func (a *App) CreateCategory(name, defaultPath, color string) (int, error) {
	return a.svc.CreateCategory(a.ctx, name, defaultPath, color)
}

func (a *App) UpdateCategory(id int, name, defaultPath, color string) error {
	return a.svc.UpdateCategory(a.ctx, id, name, defaultPath, color)
}

func (a *App) DeleteCategory(id int) error {
	return a.svc.DeleteCategory(a.ctx, id)
}

func (a *App) ListTags() ([]api.TagDTO, error) {
	return a.svc.ListTags(a.ctx)
}

func (a *App) CreateTag(name, color string) (int, error) {
	return a.svc.CreateTag(a.ctx, name, color)
}

func (a *App) DeleteTag(id int) error {
	return a.svc.DeleteTag(a.ctx, id)
}

func (a *App) AssignTag(infohash string, tagID int) error {
	return a.svc.AssignTag(a.ctx, infohash, tagID)
}

func (a *App) UnassignTag(infohash string, tagID int) error {
	return a.svc.UnassignTag(a.ctx, infohash, tagID)
}

func (a *App) SetTorrentCategory(infohash string, categoryID *int) error {
	return a.svc.SetTorrentCategory(a.ctx, infohash, categoryID)
}

func (a *App) SetFilePriorities(infohash string, prios map[int]string) error {
	return a.svc.SetFilePriorities(a.ctx, infohash, prios)
}

func (a *App) GetDefaultSavePath() (string, error) {
	return a.svc.GetDefaultSavePath(a.ctx)
}

func (a *App) SetDefaultSavePath(path string) error {
	return a.svc.SetDefaultSavePath(a.ctx, path)
}

func (a *App) AddTorrentBytes(blob []byte, savePath string) (string, error) {
	id, err := a.svc.AddTorrentBytes(a.ctx, blob, savePath)
	if err != nil {
		return "", err
	}
	return string(id), nil
}

func (a *App) GetLimits() (api.LimitsDTO, error) { return a.svc.GetLimits(a.ctx) }
func (a *App) SetLimits(l api.LimitsDTO) error   { return a.svc.SetLimits(a.ctx, l) }
func (a *App) ToggleAltSpeed() (bool, error)     { return a.svc.ToggleAltSpeed(a.ctx) }

func (a *App) GetQueueLimits() api.QueueLimitsDTO        { return a.svc.GetQueueLimits(a.ctx) }
func (a *App) SetQueueLimits(q api.QueueLimitsDTO) error { return a.svc.SetQueueLimits(a.ctx, q) }

func (a *App) SetQueuePosition(infohash string, pos int) error {
	return a.svc.SetQueuePosition(a.ctx, infohash, pos)
}

func (a *App) SetForceStart(infohash string, force bool) error {
	return a.svc.SetForceStart(a.ctx, infohash, force)
}

func (a *App) ListScheduleRules() ([]api.ScheduleRuleDTO, error) {
	return a.svc.ListScheduleRules(a.ctx)
}

func (a *App) CreateScheduleRule(r api.ScheduleRuleDTO) (int, error) {
	return a.svc.CreateScheduleRule(a.ctx, r)
}

func (a *App) UpdateScheduleRule(r api.ScheduleRuleDTO) error {
	return a.svc.UpdateScheduleRule(a.ctx, r)
}

func (a *App) DeleteScheduleRule(id int) error {
	return a.svc.DeleteScheduleRule(a.ctx, id)
}

func (a *App) GetBlocklist() api.BlocklistDTO {
	return a.svc.GetBlocklist(a.ctx)
}

func (a *App) SetBlocklistURL(url string, enabled bool) error {
	return a.svc.SetBlocklistURL(a.ctx, url, enabled)
}

func (a *App) RefreshBlocklist() error {
	return a.svc.RefreshBlocklist(a.ctx)
}

func (a *App) ListFeeds() ([]api.FeedDTO, error) {
	return a.svc.ListFeeds(a.ctx)
}

func (a *App) CreateFeed(f api.FeedDTO) (int, error) {
	return a.svc.CreateFeed(a.ctx, f)
}

func (a *App) UpdateFeed(f api.FeedDTO) error {
	return a.svc.UpdateFeed(a.ctx, f)
}

func (a *App) DeleteFeed(id int) error {
	return a.svc.DeleteFeed(a.ctx, id)
}

func (a *App) ListFiltersByFeed(feedID int) ([]api.FilterDTO, error) {
	return a.svc.ListFiltersByFeed(a.ctx, feedID)
}

func (a *App) CreateFilter(f api.FilterDTO) (int, error) {
	return a.svc.CreateFilter(a.ctx, f)
}

func (a *App) UpdateFilter(f api.FilterDTO) error {
	return a.svc.UpdateFilter(a.ctx, f)
}

func (a *App) DeleteFilter(id int) error {
	return a.svc.DeleteFilter(a.ctx, id)
}

func (a *App) streamTicks(ctx context.Context) {
	torrents := time.NewTicker(500 * time.Millisecond)
	stats := time.NewTicker(1 * time.Second)
	inspector := time.NewTicker(1 * time.Second)
	defer torrents.Stop()
	defer stats.Stop()
	defer inspector.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-torrents.C:
			rows, err := a.svc.ListTorrents(ctx)
			if err != nil {
				log.Error().Err(err).Msg("list torrents during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "torrents:tick", rows)
			if a.hub != nil {
				a.hub.PublishTorrents(rows)
			}
		case <-stats.C:
			s, err := a.svc.GlobalStats(ctx)
			if err != nil {
				log.Error().Err(err).Msg("global stats during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "stats:tick", s)
			if a.hub != nil {
				a.hub.PublishStats(s)
			}
		case <-inspector.C:
			detail, err := a.svc.DetailForFocus(ctx)
			if err != nil {
				log.Error().Err(err).Msg("detail for focus during tick")
				continue
			}
			if detail == nil {
				continue
			}
			wailsruntime.EventsEmit(ctx, "inspector:tick", detail)
			if a.hub != nil {
				a.hub.PublishInspector(*detail)
			}
		}
	}
}
