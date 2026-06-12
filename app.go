package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"mosaic/backend/api"
	"mosaic/backend/bootstrap"
	"mosaic/backend/engine"
	"mosaic/backend/platform"
	"mosaic/backend/remote"
	"mosaic/backend/tray"
)

// App is the Wails-bound type. Methods on App become callable from the
// frontend via the auto-generated bindings in frontend/wailsjs/.
type App struct {
	svc *api.Service
	hub *remote.Hub // for the per-user tick goroutine started in startup()

	// ctxMu guards ctx, cancelTicks, and pendingArgs. ctx is published once
	// by startup() (Wails OnStartup) but is read from goroutines that may run
	// before or concurrently with it: the Linux second-instance listener,
	// tray callbacks, and the updater's OnAvailable. context() is the only
	// accessor. Launch args forwarded before startup() are buffered in
	// pendingArgs and drained once the context exists.
	ctxMu       sync.Mutex
	ctx         context.Context
	cancelTicks context.CancelFunc // stops the tick goroutines; see shutdown()
	pendingArgs [][]string

	// quitFully is set by QuitFully so the OnBeforeClose hook in main.go
	// can distinguish "tray asked us to fully quit" from "user clicked X
	// while close-to-tray is enabled". Without this the hook would hide
	// the window every time, including when the tray's Quit was used.
	quitFully atomic.Bool

	// windowVisible mirrors whether the OS window is currently shown.
	// streamWailsEvents skips Wails event emission while it's false (hidden
	// in the tray, nobody can see the SPA) to save the 2Hz list + 1Hz
	// stats/detail churn. Only the paths we control flip it (close-to-tray
	// WindowHide in main.go, tray ShowWindow/ShowSettings, StartHidden); on
	// macOS the X button hides at the AppKit layer without telling us, so
	// the flag conservatively stays true there. Seed-limit enforcement lives
	// in bootstrap.StreamTicks and is never gated by this.
	windowVisible atomic.Bool
}

func NewApp(svc *api.Service, hub *remote.Hub) *App {
	a := &App{svc: svc, hub: hub}
	a.windowVisible.Store(true)
	return a
}

// context returns the startup-published context, or nil before startup().
func (a *App) context() context.Context {
	a.ctxMu.Lock()
	defer a.ctxMu.Unlock()
	return a.ctx
}

// setWindowVisible records OS-window visibility; see the field comment.
func (a *App) setWindowVisible(v bool) {
	a.windowVisible.Store(v)
}

func (a *App) startup(ctx context.Context) {
	// The desktop app has no login — every Service call runs as the system
	// caller (full access, sees all torrents). Carrying it on a.ctx means all
	// the a.svc.* calls below inherit it without per-call plumbing; Wails
	// runtime calls ignore the extra context value.
	ctx = api.WithCaller(ctx, api.SystemCaller)
	// The tick goroutines get their own cancellable child: Wails never
	// cancels the OnStartup context, so without this they'd keep calling
	// the engine/DB while main.go's deferred cleanup tears everything down.
	// Cancelled in shutdown(), mirroring mosaicd's cancelCtx + grace sleep
	// (see cmd/mosaicd/main.go).
	tickCtx, cancelTicks := context.WithCancel(ctx)
	a.ctxMu.Lock()
	a.ctx = ctx
	a.cancelTicks = cancelTicks
	pending := a.pendingArgs
	a.pendingArgs = nil
	a.ctxMu.Unlock()
	// Two tick goroutines: one drives the embedded SPA via Wails events,
	// the other drives connected browser clients via the shared per-user
	// hub stream. The hub goroutine is a no-op when no one is connected.
	go a.streamWailsEvents(tickCtx)
	go bootstrap.StreamTicks(tickCtx, a.svc, a.hub)
	// Drain launch args that arrived before the context existed (e.g. a
	// file-handler forward from the Linux second-instance listener racing
	// boot — main.go binds that listener before Wails runs).
	for _, args := range pending {
		go a.HandleLaunchArgs(args)
	}
	// macOS routes Finder-clicked .torrent files and browser-clicked magnet:
	// URLs through Apple Events, not argv. Register NSAppleEventManager
	// handlers that funnel both into HandleLaunchArgs. No-op on other OSes.
	platform.InstallAppleEventHandlers(
		func(path string) { a.HandleLaunchArgs([]string{path}) },
		func(url string) { a.HandleLaunchArgs([]string{url}) },
	)
	// Self-heal Windows file associations. Auto-update doesn't run installer
	// code, so users on a stale install (or anyone who installed pre-v0.1.13
	// before the NSIS file-association block existed) will never see Mosaic
	// as an option in Settings → Default apps unless we write the registry
	// entries ourselves on startup. No-op on macOS / Linux.
	if exe, err := os.Executable(); err == nil {
		if err := platform.EnsureFileAssociations(exe); err != nil {
			log.Warn().Err(err).Msg("file-association registry write failed")
		}
	}
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
//
// Safe to call before startup(): args forwarded while we're still booting
// (the Linux second-instance listener binds before Wails runs) are buffered
// and drained by startup() once the context exists — calling svc.AddMagnet
// with a nil context here used to panic in a bare goroutine and abort the
// whole process.
//
// Each invocation emits a `launch:notice` Wails event with the outcome so the
// SPA can toast immediately — useful both as user feedback and as a
// diagnostic when file-association routing seems silent.
func (a *App) HandleLaunchArgs(args []string) {
	a.ctxMu.Lock()
	ctx := a.ctx
	if ctx == nil {
		a.pendingArgs = append(a.pendingArgs, args)
		a.ctxMu.Unlock()
		log.Info().Strs("args", args).Msg("HandleLaunchArgs: buffered until startup completes")
		return
	}
	a.ctxMu.Unlock()
	log.Info().Strs("args", args).Msg("HandleLaunchArgs invoked")
	a.emitLaunchNotice(map[string]any{"event": "received", "count": len(args), "args": args})
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "magnet:"):
			id, err := a.svc.AddMagnet(ctx, arg, "")
			if err != nil {
				log.Warn().Err(err).Msg("launch arg: AddMagnet failed")
				a.emitLaunchNotice(map[string]any{"event": "magnet_error", "error": err.Error(), "magnet": arg})
				continue
			}
			log.Info().Str("magnet", arg).Str("id", string(id)).Msg("added magnet from launch arg")
			a.emitLaunchNotice(map[string]any{"event": "magnet_added", "id": string(id)})
		case strings.HasSuffix(strings.ToLower(arg), ".torrent"):
			id, err := a.svc.AddTorrentFile(ctx, arg, "")
			if err != nil {
				log.Warn().Err(err).Str("path", arg).Msg("launch arg: AddTorrentFile failed")
				a.emitLaunchNotice(map[string]any{"event": "torrent_error", "error": err.Error(), "path": arg})
				continue
			}
			log.Info().Str("path", arg).Str("id", string(id)).Msg("added .torrent from launch arg")
			a.emitLaunchNotice(map[string]any{"event": "torrent_added", "id": string(id), "path": arg})
		default:
			log.Debug().Str("arg", arg).Msg("launch arg: ignored (not a magnet: or .torrent)")
		}
	}
}

func (a *App) emitLaunchNotice(payload map[string]any) {
	ctx := a.context()
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, "launch:notice", payload)
}

// shutdown is wired as Wails's OnShutdown hook. It cancels the tick
// goroutines started in startup() and then sleeps a brief grace so an
// in-flight tick can finish (or notice cancellation) before main.go's
// deferred cleanup closes the hub, engine, and DB underneath them —
// the same mitigation mosaicd applies after SIGTERM (cmd/mosaicd/main.go).
func (a *App) shutdown(_ context.Context) {
	a.ctxMu.Lock()
	cancel := a.cancelTicks
	a.cancelTicks = nil
	a.ctxMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	time.Sleep(200 * time.Millisecond)
}

// AddMagnet adds a magnet link. Returns the torrent ID.
func (a *App) AddMagnet(magnet, savePath string) (string, error) {
	id, err := a.svc.AddMagnet(a.context(), magnet, savePath)
	if err != nil {
		return "", err
	}
	return string(id), nil
}

// ListTorrents returns the current list as DTOs.
func (a *App) ListTorrents() ([]api.TorrentDTO, error) {
	return a.svc.ListTorrents(a.context())
}

// PickAndAddTorrent opens a native file dialog, lets the user choose a
// .torrent file, and adds it to the engine + persistence. Returns the new
// torrent ID, or "" if the user cancelled.
func (a *App) PickAndAddTorrent(savePath string) (string, error) {
	path, err := wailsruntime.OpenFileDialog(a.context(), wailsruntime.OpenDialogOptions{
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
	id, err := a.svc.AddTorrentFile(a.context(), path, savePath)
	if err != nil {
		return "", err
	}
	return string(id), nil
}

// Pause/Resume/Remove operate by id.
func (a *App) Pause(id string) error   { return a.svc.Pause(a.context(), engine.TorrentID(id)) }
func (a *App) Resume(id string) error  { return a.svc.Resume(a.context(), engine.TorrentID(id)) }
func (a *App) Recheck(id string) error { return a.svc.Recheck(a.context(), engine.TorrentID(id)) }
func (a *App) Remove(id string, deleteFiles bool) error {
	return a.svc.Remove(a.context(), engine.TorrentID(id), deleteFiles)
}

func (a *App) GlobalStats() (api.GlobalStats, error) {
	return a.svc.GlobalStats(a.context())
}

// SetInspectorFocus tells the backend the inspector is open on torrent `id`
// with `tabs` visible. The next inspector:tick (and subsequent ticks at 1Hz)
// will include data scoped to those tabs.
func (a *App) SetInspectorFocus(id string, tabs []string) error {
	return a.svc.SetInspectorFocus(a.context(), id, tabs)
}

// ClearInspectorFocus stops inspector:tick emission until SetInspectorFocus is called again.
func (a *App) ClearInspectorFocus() {
	a.svc.ClearInspectorFocus(a.context())
}

func (a *App) ListCategories() ([]api.CategoryDTO, error) {
	return a.svc.ListCategories(a.context())
}

func (a *App) CreateCategory(name, defaultPath, color string) (int, error) {
	return a.svc.CreateCategory(a.context(), name, defaultPath, color)
}

func (a *App) UpdateCategory(id int, name, defaultPath, color string) error {
	return a.svc.UpdateCategory(a.context(), id, name, defaultPath, color)
}

func (a *App) DeleteCategory(id int) error {
	return a.svc.DeleteCategory(a.context(), id)
}

func (a *App) ListTags() ([]api.TagDTO, error) {
	return a.svc.ListTags(a.context())
}

func (a *App) CreateTag(name, color string) (int, error) {
	return a.svc.CreateTag(a.context(), name, color)
}

func (a *App) DeleteTag(id int) error {
	return a.svc.DeleteTag(a.context(), id)
}

func (a *App) AssignTag(infohash string, tagID int) error {
	return a.svc.AssignTag(a.context(), infohash, tagID)
}

func (a *App) UnassignTag(infohash string, tagID int) error {
	return a.svc.UnassignTag(a.context(), infohash, tagID)
}

func (a *App) SetTorrentCategory(infohash string, categoryID *int) error {
	return a.svc.SetTorrentCategory(a.context(), infohash, categoryID)
}

func (a *App) SetFilePriorities(infohash string, prios map[int]string) error {
	return a.svc.SetFilePriorities(a.context(), infohash, prios)
}

func (a *App) AddTracker(infohash, trackerURL string) error {
	return a.svc.AddTracker(a.context(), infohash, trackerURL)
}

func (a *App) RemoveTracker(infohash, trackerURL string) error {
	return a.svc.RemoveTracker(a.context(), infohash, trackerURL)
}

func (a *App) GetDefaultSavePath() (string, error) {
	return a.svc.GetDefaultSavePath(a.context())
}

func (a *App) SetDefaultSavePath(path string) error {
	return a.svc.SetDefaultSavePath(a.context(), path)
}

func (a *App) AddTorrentBytes(blob []byte, savePath string) (string, error) {
	id, err := a.svc.AddTorrentBytes(a.context(), blob, savePath)
	if err != nil {
		return "", err
	}
	return string(id), nil
}

func (a *App) GetLimits() (api.LimitsDTO, error) { return a.svc.GetLimits(a.context()) }
func (a *App) SetLimits(l api.LimitsDTO) error   { return a.svc.SetLimits(a.context(), l) }
func (a *App) ToggleAltSpeed() (bool, error)     { return a.svc.ToggleAltSpeed(a.context()) }

func (a *App) GetQueueLimits() api.QueueLimitsDTO        { return a.svc.GetQueueLimits(a.context()) }
func (a *App) SetQueueLimits(q api.QueueLimitsDTO) error { return a.svc.SetQueueLimits(a.context(), q) }

func (a *App) GetPeerLimits() api.PeerLimitsDTO        { return a.svc.GetPeerLimits(a.context()) }
func (a *App) SetPeerLimits(p api.PeerLimitsDTO) error { return a.svc.SetPeerLimits(a.context(), p) }

func (a *App) SetQueuePosition(infohash string, pos int) error {
	return a.svc.SetQueuePosition(a.context(), infohash, pos)
}

func (a *App) SetForceStart(infohash string, force bool) error {
	return a.svc.SetForceStart(a.context(), infohash, force)
}

func (a *App) SetSequential(infohash string, enabled bool) error {
	return a.svc.SetSequential(a.context(), infohash, enabled)
}

func (a *App) ListScheduleRules() ([]api.ScheduleRuleDTO, error) {
	return a.svc.ListScheduleRules(a.context())
}

func (a *App) CreateScheduleRule(r api.ScheduleRuleDTO) (int, error) {
	return a.svc.CreateScheduleRule(a.context(), r)
}

func (a *App) UpdateScheduleRule(r api.ScheduleRuleDTO) error {
	return a.svc.UpdateScheduleRule(a.context(), r)
}

func (a *App) DeleteScheduleRule(id int) error {
	return a.svc.DeleteScheduleRule(a.context(), id)
}

func (a *App) GetBlocklist() api.BlocklistDTO {
	return a.svc.GetBlocklist(a.context())
}

func (a *App) SetBlocklistURL(url string, enabled bool) error {
	return a.svc.SetBlocklistURL(a.context(), url, enabled)
}

func (a *App) RefreshBlocklist() error {
	return a.svc.RefreshBlocklist(a.context())
}

func (a *App) ListFeeds() ([]api.FeedDTO, error) {
	return a.svc.ListFeeds(a.context())
}

func (a *App) CreateFeed(f api.FeedDTO) (int, error) {
	return a.svc.CreateFeed(a.context(), f)
}

func (a *App) UpdateFeed(f api.FeedDTO) error {
	return a.svc.UpdateFeed(a.context(), f)
}

func (a *App) DeleteFeed(id int) error {
	return a.svc.DeleteFeed(a.context(), id)
}

// PollFeedNow polls a single RSS feed immediately, bypassing its
// scheduled interval. Surfaced by the SPA's per-row refresh icon.
func (a *App) PollFeedNow(id int) error {
	return a.svc.PollFeedNow(a.context(), id)
}

func (a *App) GetFeedItems(feedID int) ([]api.FeedItemDTO, error) {
	return a.svc.GetFeedItems(a.context(), feedID)
}

func (a *App) AddFeedItem(torrentURL, savePath string) error {
	_, err := a.svc.AddFeedItem(a.context(), torrentURL, savePath)
	return err
}

func (a *App) ListFiltersByFeed(feedID int) ([]api.FilterDTO, error) {
	return a.svc.ListFiltersByFeed(a.context(), feedID)
}

func (a *App) CreateFilter(f api.FilterDTO) (int, error) {
	return a.svc.CreateFilter(a.context(), f)
}

func (a *App) UpdateFilter(f api.FilterDTO) error {
	return a.svc.UpdateFilter(a.context(), f)
}

func (a *App) DeleteFilter(id int) error {
	return a.svc.DeleteFilter(a.context(), id)
}

func (a *App) GetWebConfig() api.WebConfigDTO {
	return a.svc.GetWebConfig(a.context())
}

func (a *App) SetWebConfig(c api.WebConfigDTO) error {
	return a.svc.SetWebConfig(a.context(), c)
}

func (a *App) SetWebPassword(plain string) error {
	return a.svc.SetWebPassword(a.context(), plain)
}

func (a *App) RotateAPIKey() (string, error) {
	return a.svc.RotateAPIKey(a.context())
}

// AppVersion returns the build-time version string (e.g. "v0.7.0" or "dev").
func (a *App) AppVersion() string {
	return version
}

// GetDesktopIntegration / SetDesktopIntegration / QuitFully are the bindings
// the system-tray + close-to-tray + Settings UI depend on. The DTO field names
// are part of the wire contract with the frontend (see api.DesktopIntegrationDTO).
func (a *App) GetDesktopIntegration() api.DesktopIntegrationDTO {
	return a.svc.GetDesktopIntegration(a.context())
}

func (a *App) SetDesktopIntegration(c api.DesktopIntegrationDTO) error {
	return a.svc.SetDesktopIntegration(a.context(), c)
}

func (a *App) GetWatchFolder() api.WatchFolderDTO {
	return a.svc.GetWatchFolder(a.context())
}

func (a *App) SetWatchFolder(c api.WatchFolderDTO) error {
	return a.svc.SetWatchFolder(a.context(), c)
}

// PickWatchFolder opens a native directory dialog so the user can choose a
// watch folder path. Returns the selected path or "" if cancelled.
func (a *App) PickWatchFolder() (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.context(), wailsruntime.OpenDialogOptions{
		Title: "Select Watch Folder",
	})
}

// QuitFully bypasses the close-to-tray OnBeforeClose hook and tears the
// process down. Used by the tray's "Quit Mosaic" item — without this the
// hook would just hide the window again, leaving the app un-quit-able from
// the tray.
func (a *App) QuitFully() {
	ctx := a.context()
	if ctx == nil {
		return
	}
	a.quitFully.Store(true)
	wailsruntime.Quit(ctx)
}

// QuittingFully reports whether the most recent quit request was from
// QuitFully (true) versus a normal X-button close (false). main.go's
// OnBeforeClose hook reads this to decide whether to honor close-to-tray.
func (a *App) QuittingFully() bool {
	return a.quitFully.Load()
}

// ShowWindow is the tray "Show Mosaic" callback target — un-minimize and
// raise the window. Safe to call before startup() (no-ops if ctx is nil).
func (a *App) ShowWindow() {
	ctx := a.context()
	if ctx == nil {
		return
	}
	a.setWindowVisible(true)
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.WindowShow(ctx)
}

// ShowSettings raises the window AND emits navigate:settings so the SPA can
// route to the Settings pane. Used by the tray's "Settings…" item.
func (a *App) ShowSettings() {
	ctx := a.context()
	if ctx == nil {
		return
	}
	a.setWindowVisible(true)
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.WindowShow(ctx)
	wailsruntime.EventsEmit(ctx, "navigate:settings")
}

func (a *App) GetUpdaterConfig() api.UpdaterConfigDTO {
	return a.svc.GetUpdaterConfig(a.context())
}

func (a *App) SetUpdaterConfig(c api.UpdaterConfigDTO) error {
	return a.svc.SetUpdaterConfig(a.context(), c)
}

func (a *App) CheckForUpdate() (api.UpdateInfoDTO, error) {
	return a.svc.CheckForUpdate(a.context())
}

func (a *App) InstallUpdate() error {
	return a.svc.InstallUpdate(a.context())
}

// NotifyUpdateAvailable emits the Wails-side `update:available` event so the
// desktop SPA can render its toast. Called from main.go's updater OnAvailable
// callback, off the updater goroutine; safe to invoke before startup() has
// run (the context may be nil) — emission is silently skipped in that case.
func (a *App) NotifyUpdateAvailable(info api.UpdateInfoDTO) {
	ctx := a.context()
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, "update:available", info)
}

// Platform returns the OS the desktop shell is running on ("darwin", "windows",
// "linux"). The SPA queries this once at startup to decide whether to render
// custom Win11-style window controls (Windows runs frameless).
func (a *App) Platform() string {
	return runtime.GOOS
}

// GnomeTrayStatus reports the live state of Gnome's StatusNotifierItem
// support so the SPA can render an in-app prompt to enable / install
// the AppIndicator extension when the user can't otherwise see Mosaic's
// tray icon. Always returns "not_applicable" off Linux/Gnome, when the
// user has previously dismissed the prompt, or when the tray watcher is
// already serving (the icon will render — no prompt needed).
//
// Possible values: "not_applicable", "needs_install", "needs_enable",
// "needs_restart". See backend/tray/gnome_linux.go for the precise
// classification logic.
func (a *App) GnomeTrayStatus() string {
	if runtime.GOOS != "linux" {
		return string(tray.GnomePromptStatusNotApplicable)
	}
	if a.svc.IsGnomeAppIndicatorPromptDismissed(a.context()) {
		return string(tray.GnomePromptStatusNotApplicable)
	}
	return string(tray.EvaluateGnomePromptStatus(a.context()))
}

// EnableGnomeTray flips the dconf key that gnome-shell reads at startup
// to enable the AppIndicator extension for the current user. The user
// still needs to log out + back in (or restart gnome-shell) for the
// change to take effect; the SPA surfaces that requirement after this
// returns.
func (a *App) EnableGnomeTray() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("EnableGnomeTray: only supported on Linux")
	}
	if !tray.IsGnomeSession() {
		return fmt.Errorf("EnableGnomeTray: not a Gnome session")
	}
	return tray.EnableAppIndicatorExtension(a.context())
}

// DismissGnomeTrayPrompt persists the user's "don't show this again"
// choice. Cleared only by manually wiping the desktop.gnome_appindicator_dismissed
// setting (we don't surface a UI for that yet — re-installing the deb
// also doesn't clear it, by design).
func (a *App) DismissGnomeTrayPrompt() error {
	return a.svc.DismissGnomeAppIndicatorPrompt(a.context())
}

// WindowMinimise minimizes the desktop window.
func (a *App) WindowMinimise() {
	ctx := a.context()
	if ctx == nil {
		return
	}
	wailsruntime.WindowMinimise(ctx)
}

// WindowMaximise toggles between maximized and restored states.
func (a *App) WindowMaximise() {
	ctx := a.context()
	if ctx == nil {
		return
	}
	wailsruntime.WindowToggleMaximise(ctx)
}

// WindowClose quits the app. Single-window desktop convention: closing the
// only window terminates the process.
func (a *App) WindowClose() {
	ctx := a.context()
	if ctx == nil {
		return
	}
	wailsruntime.Quit(ctx)
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

// GetSeedingDefaults returns global seeding stop-condition defaults.
func (a *App) GetSeedingDefaults() api.SeedingDefaultsDTO {
	return a.svc.GetSeedingDefaults(a.context())
}

// SetSeedingDefaults persists global seeding stop-condition defaults.
func (a *App) SetSeedingDefaults(d api.SeedingDefaultsDTO) error {
	return a.svc.SetSeedingDefaults(a.context(), d)
}

// GetTorrentSeedPolicy returns the per-torrent seed policy override.
func (a *App) GetTorrentSeedPolicy(infohash string) (api.SeedPolicyDTO, error) {
	return a.svc.GetTorrentSeedPolicy(a.context(), infohash)
}

// SetTorrentSeedPolicy sets (or clears) the per-torrent seed policy override.
func (a *App) SetTorrentSeedPolicy(infohash string, p api.SeedPolicyDTO) error {
	return a.svc.SetTorrentSeedPolicy(a.context(), infohash, p)
}

// streamWailsEvents emits state snapshots to the embedded SPA via Wails
// events. The companion bootstrap.StreamTicks goroutine, started alongside
// this one in startup(), handles the per-user hub fan-out for connected
// browser clients — and also owns the periodic seed-limit enforcement
// (CheckSeedLimits), so it runs in the daemon too and never twice here.
//
// While the window is hidden in the tray (windowVisible false) the emission
// work is skipped entirely — nobody can see the SPA, so the 2Hz list + 1Hz
// stats/detail snapshots are pure waste. The flag flips back before the
// window is shown again, so at most one tick interval of staleness is
// visible on restore.
func (a *App) streamWailsEvents(ctx context.Context) {
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
			if !a.windowVisible.Load() {
				continue
			}
			rows, err := a.svc.ListTorrents(ctx)
			if err != nil {
				log.Error().Err(err).Msg("list torrents during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "torrents:tick", rows)
		case <-stats.C:
			if !a.windowVisible.Load() {
				continue
			}
			s, err := a.svc.GlobalStats(ctx)
			if err != nil {
				log.Error().Err(err).Msg("global stats during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "stats:tick", s)
		case <-inspector.C:
			if !a.windowVisible.Load() {
				continue
			}
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
