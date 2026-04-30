package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
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
	// Handle any magnet: URL or .torrent path passed on the command line at
	// first launch (Windows + Linux always; macOS when launched via `open`).
	// SecondInstanceLaunch (configured in main.go) routes args from a second
	// process invocation to a.HandleLaunchArgs as well.
	if len(os.Args) > 1 {
		go a.HandleLaunchArgs(os.Args[1:])
	}
}

// HandleLaunchArgs classifies each arg as a magnet URL or a .torrent file
// path and routes it to the engine. Unknown args are silently ignored
// (Wails or the OS may pass internal flags we don't care about).
// Exported so main.go's SingleInstanceLock OnSecondInstanceLaunch can call it.
func (a *App) HandleLaunchArgs(args []string) {
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "magnet:"):
			if _, err := a.svc.AddMagnet(a.ctx, arg, ""); err != nil {
				log.Warn().Err(err).Msg("launch arg: AddMagnet failed")
			} else {
				log.Info().Str("magnet", arg).Msg("added magnet from launch arg")
			}
		case strings.HasSuffix(strings.ToLower(arg), ".torrent"):
			if _, err := a.svc.AddTorrentFile(a.ctx, arg, ""); err != nil {
				log.Warn().Err(err).Str("path", arg).Msg("launch arg: AddTorrentFile failed")
			} else {
				log.Info().Str("path", arg).Msg("added .torrent from launch arg")
			}
		}
	}
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

func (a *App) GetWebConfig() api.WebConfigDTO {
	return a.svc.GetWebConfig(a.ctx)
}

func (a *App) SetWebConfig(c api.WebConfigDTO) error {
	return a.svc.SetWebConfig(a.ctx, c)
}

func (a *App) SetWebPassword(plain string) error {
	return a.svc.SetWebPassword(a.ctx, plain)
}

func (a *App) RotateAPIKey() (string, error) {
	return a.svc.RotateAPIKey(a.ctx)
}

// AppVersion returns the build-time version string (e.g. "v0.7.0" or "dev").
func (a *App) AppVersion() string {
	return version
}

func (a *App) GetUpdaterConfig() api.UpdaterConfigDTO {
	return a.svc.GetUpdaterConfig(a.ctx)
}

func (a *App) SetUpdaterConfig(c api.UpdaterConfigDTO) error {
	return a.svc.SetUpdaterConfig(a.ctx, c)
}

func (a *App) CheckForUpdate() (api.UpdateInfoDTO, error) {
	return a.svc.CheckForUpdate(a.ctx)
}

func (a *App) InstallUpdate() error {
	return a.svc.InstallUpdate(a.ctx)
}

// NotifyUpdateAvailable emits the Wails-side `update:available` event so the
// desktop SPA can render its toast. Called from main.go's updater OnAvailable
// callback, off the updater goroutine; safe to invoke before startup() has
// run (a.ctx may be nil) — emission is silently skipped in that case.
func (a *App) NotifyUpdateAvailable(info api.UpdateInfoDTO) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "update:available", info)
}

// Platform returns the OS the desktop shell is running on ("darwin", "windows",
// "linux"). The SPA queries this once at startup to decide whether to render
// custom Win11-style window controls (Windows runs frameless).
func (a *App) Platform() string {
	return runtime.GOOS
}

// WindowMinimise minimizes the desktop window.
func (a *App) WindowMinimise() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowMinimise(a.ctx)
}

// WindowMaximise toggles between maximized and restored states.
func (a *App) WindowMaximise() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowToggleMaximise(a.ctx)
}

// WindowClose quits the app. Single-window desktop convention: closing the
// only window terminates the process.
func (a *App) WindowClose() {
	if a.ctx == nil {
		return
	}
	wailsruntime.Quit(a.ctx)
}

// OpenFolder reveals the given path in the OS file manager. Desktop-only —
// browser shells have no equivalent affordance.
func (a *App) OpenFolder(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
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
