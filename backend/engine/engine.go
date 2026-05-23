package engine

import (
	"context"
	"io"
	"sync"
	"time"
)

// Engine wraps a Backend and emits a stream of EngineEvent via Subscribe.
type Engine struct {
	backend  Backend
	tickRate time.Duration

	mu     sync.RWMutex
	subs   []chan EngineEvent
	stop   chan struct{}
	closed bool

	// tickRun gates the per-tick poll loop. The loop emits EventTick/
	// EventComplete to subscribers; with zero subscribers (the mosaicd
	// daemon, which never calls Subscribe — it streams via streamTicks
	// instead) it produces nothing but a 500ms backend.List() that nobody
	// reads. tickOnce starts run() lazily on the FIRST Subscribe() so the
	// desktop app (which subscribes via notifications.Subscriber) still
	// gets its event stream, while mosaicd pays nothing. Rate sampling no
	// longer depends on this loop — the backend owns its own sampler.
	tickOnce sync.Once
}

// NewEngine returns an Engine that polls Backend.List() every tickRate and
// emits EventTick for each torrent. The caller must call Close().
func NewEngine(b Backend, tickRate time.Duration) *Engine {
	e := &Engine{
		backend:  b,
		tickRate: tickRate,
		stop:     make(chan struct{}),
	}
	// Wire backend errors (out-of-disk-space, storage write failures) into
	// the engine event stream so the notifications subscriber + UI surface
	// them. Pre-v0.5.1 the backend's chunk-write errors fell into
	// anacrolix's silent-disable path; users saw "50 peers, 0 bytes" with
	// no signal anything was wrong. Backends that don't surface errors
	// (FakeBackend) don't implement this optional method, so the assert
	// is conditional.
	if eh, ok := b.(interface{ SetErrorHandler(func(TorrentID, error)) }); ok {
		eh.SetErrorHandler(func(id TorrentID, err error) {
			snap, _ := b.Snapshot(id)
			e.emit(EngineEvent{Kind: EventError, ID: id, Snapshot: snap, Err: err})
		})
	}
	// run() is started lazily on the first Subscribe() — see Engine.tickOnce.
	return e
}

func (e *Engine) Subscribe() <-chan EngineEvent {
	ch := make(chan EngineEvent, 64)
	e.mu.Lock()
	if e.closed {
		// Engine already torn down — hand back a closed channel so a late
		// Subscribe() drains immediately instead of blocking forever, and
		// don't start the poll loop on a dead engine.
		e.mu.Unlock()
		close(ch)
		return ch
	}
	e.subs = append(e.subs, ch)
	e.mu.Unlock()
	// Start the poll loop on the first subscriber. A daemon that never
	// subscribes never spins it up.
	e.tickOnce.Do(func() { go e.run() })
	return ch
}

func (e *Engine) emit(ev EngineEvent) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, ch := range e.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (e *Engine) AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error) {
	id, err := e.backend.AddMagnet(ctx, magnet, savePath)
	if err != nil {
		return "", err
	}
	snap, err := e.backend.Snapshot(id)
	if err != nil {
		return "", err
	}
	e.emit(EngineEvent{Kind: EventAdded, ID: id, Snapshot: snap})
	return id, nil
}

func (e *Engine) AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error) {
	id, err := e.backend.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", err
	}
	snap, err := e.backend.Snapshot(id)
	if err != nil {
		return "", err
	}
	e.emit(EngineEvent{Kind: EventAdded, ID: id, Snapshot: snap})
	return id, nil
}

func (e *Engine) Pause(id TorrentID) error   { return e.backend.Pause(id) }
func (e *Engine) Resume(id TorrentID) error  { return e.backend.Resume(id) }
func (e *Engine) Recheck(id TorrentID) error { return e.backend.Recheck(id) }

func (e *Engine) ApplyPerTorrentMaxPeers(n int) error {
	return e.backend.ApplyPerTorrentMaxPeers(n)
}

func (e *Engine) Remove(id TorrentID, deleteFiles bool) error {
	if err := e.backend.Remove(id, deleteFiles); err != nil {
		return err
	}
	e.emit(EngineEvent{Kind: EventRemoved, ID: id})
	return nil
}

func (e *Engine) List() []Snapshot { return e.backend.List() }

func (e *Engine) Snapshot(id TorrentID) (Snapshot, error) { return e.backend.Snapshot(id) }

// DetailedSnapshot delegates to the backend, packaging the file/peer/tracker
// data per scope alongside the standard Snapshot.
func (e *Engine) DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error) {
	return e.backend.DetailedSnapshot(id, scope)
}

func (e *Engine) SetFilePriorities(id TorrentID, prios map[int]Priority) error {
	return e.backend.SetFilePriorities(id, prios)
}

func (e *Engine) SetGlobalRateLimits(d, u int) error { return e.backend.SetGlobalRateLimits(d, u) }
func (e *Engine) SetTorrentRateLimits(id TorrentID, d, u int64) error {
	return e.backend.SetTorrentRateLimits(id, d, u)
}
func (e *Engine) SetIPBlocklist(r io.Reader) error { return e.backend.SetIPBlocklist(r) }
func (e *Engine) SetQueuePosition(id TorrentID, pos int) {
	e.backend.SetQueuePosition(id, pos)
}
func (e *Engine) SetForceStart(id TorrentID, force bool) {
	e.backend.SetForceStart(id, force)
}
func (e *Engine) SetSequential(id TorrentID, enabled bool) {
	e.backend.SetSequential(id, enabled)
}
func (e *Engine) ScheduledPause(id TorrentID, paused bool) {
	e.backend.ScheduledPause(id, paused)
}
func (e *Engine) MarkExpectedComplete(id TorrentID) {
	e.backend.MarkExpectedComplete(id)
}

func (e *Engine) AddTracker(id TorrentID, url string) error {
	return e.backend.AddTracker(id, url)
}

func (e *Engine) RemoveTracker(id TorrentID, url string) error {
	return e.backend.RemoveTracker(id, url)
}

func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	close(e.stop)
	for _, ch := range e.subs {
		close(ch)
	}
	e.subs = nil
	e.mu.Unlock()
	return e.backend.Close()
}

func (e *Engine) run() {
	t := time.NewTicker(e.tickRate)
	defer t.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-t.C:
			for _, snap := range e.backend.List() {
				kind := EventTick
				if snap.Completed {
					kind = EventComplete
				}
				e.emit(EngineEvent{Kind: kind, ID: snap.ID, Snapshot: snap})
			}
		}
	}
}
