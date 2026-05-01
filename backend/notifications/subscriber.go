package notifications

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"mosaic/backend/engine"
)

// Settings is the subset of the desktop-integration config the Subscriber needs.
// The shape mirrors api.DesktopIntegrationDTO's notify_* toggles so the api
// layer can pass a snapshot in via SetSettings whenever the user updates
// their preferences.
type Settings struct {
	NotifyOnComplete bool
	NotifyOnError    bool
	NotifyOnUpdate   bool
}

// Subscriber bridges engine events to a Notifier, gated by per-trigger toggles
// in Settings. It keeps a tiny in-memory map of "last seen" completion state
// per torrent so it can detect the downloading→complete *transition* — not
// every tick of an already-complete torrent (which would spam the user).
type Subscriber struct {
	notifier Notifier

	mu       sync.RWMutex
	settings Settings
	// completed[id] = true once we've fired a "complete" notification for
	// that torrent in this process. Reset on a removed event so re-add
	// triggers the notification again.
	completed map[engine.TorrentID]bool
	// errored[id] = true once we've fired an "error" notification; same reset.
	errored map[engine.TorrentID]bool

	stop chan struct{}
	done chan struct{}
}

// NewSubscriber returns an unstarted Subscriber. Call Start(ctx, eng) to
// begin consuming engine events.
func NewSubscriber(notifier Notifier, initial Settings) *Subscriber {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	return &Subscriber{
		notifier:  notifier,
		settings:  initial,
		completed: make(map[engine.TorrentID]bool),
		errored:   make(map[engine.TorrentID]bool),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// SetSettings updates the per-trigger toggles. Safe to call from any goroutine.
func (s *Subscriber) SetSettings(cfg Settings) {
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
}

// Start spawns the consumer goroutine. ctx cancellation OR Stop() terminates it.
// Calling Start more than once panics (callers shouldn't need to re-subscribe).
func (s *Subscriber) Start(ctx context.Context, eng *engine.Engine) {
	ch := eng.Subscribe()
	go s.run(ctx, ch)
}

// Stop terminates the consumer goroutine. Idempotent.
func (s *Subscriber) Stop() {
	select {
	case <-s.stop:
		return
	default:
	}
	close(s.stop)
	<-s.done
}

func (s *Subscriber) run(ctx context.Context, ch <-chan engine.EngineEvent) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			s.handle(ev)
		}
	}
}

func (s *Subscriber) handle(ev engine.EngineEvent) {
	switch ev.Kind {
	case engine.EventRemoved:
		// Allow the same torrent to fire complete/error notifications
		// again if it's re-added in this process lifetime.
		s.mu.Lock()
		delete(s.completed, ev.ID)
		delete(s.errored, ev.ID)
		s.mu.Unlock()
		return

	case engine.EventComplete, engine.EventTick:
		// EventComplete fires once per tick when Snapshot.Completed is true,
		// so we de-dup against the per-torrent "already notified" map.
		if !ev.Snapshot.Completed {
			return
		}
		s.mu.Lock()
		already := s.completed[ev.ID]
		notify := s.settings.NotifyOnComplete
		if !already {
			s.completed[ev.ID] = true
		}
		s.mu.Unlock()
		if already || !notify {
			return
		}
		title := "Download complete"
		body := truncate(ev.Snapshot.Name, 120)
		if err := s.notifier.Notify(title, body, ""); err != nil {
			log.Warn().Err(err).Msg("notifications: complete delivery failed")
		}

	case engine.EventError:
		s.mu.Lock()
		already := s.errored[ev.ID]
		notify := s.settings.NotifyOnError
		if !already {
			s.errored[ev.ID] = true
		}
		s.mu.Unlock()
		if already || !notify {
			return
		}
		title := "Torrent error"
		msg := ev.Snapshot.Name
		if ev.Err != nil {
			if msg == "" {
				msg = ev.Err.Error()
			} else {
				msg = msg + ": " + ev.Err.Error()
			}
		}
		body := truncate(msg, 120)
		if err := s.notifier.Notify(title, body, ""); err != nil {
			log.Warn().Err(err).Msg("notifications: error delivery failed")
		}
	}
}

// NotifyUpdateInstalled is the manual hook for the updater finished-installing
// trigger. Called from api.Service.InstallUpdate after a successful Install,
// so we don't have to wedge a hook into the updater package itself.
//
// TODO(updater-hook): if/when updater.Updater grows an OnInstalled callback,
// wire that here too instead of relying on the caller to invoke this.
func (s *Subscriber) NotifyUpdateInstalled(version string) {
	s.mu.RLock()
	notify := s.settings.NotifyOnUpdate
	s.mu.RUnlock()
	if !notify {
		return
	}
	title := "Mosaic updated"
	body := "Restart to finish installing"
	if version != "" {
		body = "Updated to " + version + " — restart to apply"
	}
	if err := s.notifier.Notify(title, truncate(body, 120), ""); err != nil {
		log.Warn().Err(err).Msg("notifications: update delivery failed")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
