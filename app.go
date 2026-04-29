package main

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"mosaic/backend/api"
	"mosaic/backend/engine"
)

// App is the Wails-bound type. Methods on App become callable from the
// frontend via the auto-generated bindings in frontend/wailsjs/.
type App struct {
	svc *api.Service
	ctx context.Context
}

func NewApp(svc *api.Service) *App {
	return &App{svc: svc}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.streamTicks(ctx)
}

// AddMagnet adds a magnet link. Returns the torrent ID.
func (a *App) AddMagnet(magnet string) (string, error) {
	id, err := a.svc.AddMagnet(a.ctx, magnet, "")
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
func (a *App) PickAndAddTorrent() (string, error) {
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
	id, err := a.svc.AddTorrentFile(a.ctx, path)
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
		case <-stats.C:
			s, err := a.svc.GlobalStats(ctx)
			if err != nil {
				log.Error().Err(err).Msg("global stats during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "stats:tick", s)
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
		}
	}
}
