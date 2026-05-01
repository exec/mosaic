package notifications

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"mosaic/backend/engine"
)

// fakeNotifier is the test stand-in for an OS-level notifier. Records every
// call so the test can assert title/body content + invocation count.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
	err   error // optional: non-nil to simulate a delivery failure
}

type notifyCall struct {
	Title, Body, Icon string
}

func (f *fakeNotifier) Notify(title, body, icon string) error {
	f.mu.Lock()
	f.calls = append(f.calls, notifyCall{title, body, icon})
	f.mu.Unlock()
	return f.err
}

func (f *fakeNotifier) snapshot() []notifyCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]notifyCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// TestSubscriber_CompleteFiresOnceWhenEnabled simulates the same torrent
// emitting EventComplete on multiple ticks; only the first should fire.
func TestSubscriber_CompleteFiresOnceWhenEnabled(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnComplete: true})

	ev := engine.EngineEvent{
		Kind: engine.EventComplete,
		ID:   engine.TorrentID("abc"),
		Snapshot: engine.Snapshot{
			ID:        engine.TorrentID("abc"),
			Name:      "ubuntu-24.04.iso",
			Completed: true,
		},
	}

	sub.handle(ev)
	sub.handle(ev) // duplicate tick — should NOT re-fire
	sub.handle(ev)

	calls := fn.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "Download complete", calls[0].Title)
	require.Equal(t, "ubuntu-24.04.iso", calls[0].Body)
}

// TestSubscriber_CompleteSkippedWhenDisabled checks the gating toggle.
func TestSubscriber_CompleteSkippedWhenDisabled(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnComplete: false})

	sub.handle(engine.EngineEvent{
		Kind: engine.EventComplete,
		ID:   "x",
		Snapshot: engine.Snapshot{
			ID: "x", Name: "foo", Completed: true,
		},
	})

	require.Empty(t, fn.snapshot())
}

// TestSubscriber_ErrorFiresOnceWhenEnabled covers the error trigger and the
// dedup-per-torrent contract.
func TestSubscriber_ErrorFiresOnceWhenEnabled(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnError: true})

	ev := engine.EngineEvent{
		Kind: engine.EventError,
		ID:   "y",
		Snapshot: engine.Snapshot{
			ID: "y", Name: "broken-torrent",
		},
		Err: errors.New("tracker unreachable"),
	}

	sub.handle(ev)
	sub.handle(ev) // dup
	calls := fn.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "Torrent error", calls[0].Title)
	require.Contains(t, calls[0].Body, "broken-torrent")
	require.Contains(t, calls[0].Body, "tracker unreachable")
}

// TestSubscriber_ErrorSkippedWhenDisabled covers the gating toggle.
func TestSubscriber_ErrorSkippedWhenDisabled(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnError: false})

	sub.handle(engine.EngineEvent{
		Kind: engine.EventError,
		ID:   "z",
		Snapshot: engine.Snapshot{ID: "z", Name: "f"},
		Err:    errors.New("boom"),
	})

	require.Empty(t, fn.snapshot())
}

// TestSubscriber_RemovedResetsCompletionFlag ensures re-adding a torrent
// that previously completed will fire the notification again.
func TestSubscriber_RemovedResetsCompletionFlag(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnComplete: true})

	id := engine.TorrentID("re-add")

	completeEv := engine.EngineEvent{
		Kind:     engine.EventComplete,
		ID:       id,
		Snapshot: engine.Snapshot{ID: id, Name: "thing", Completed: true},
	}

	sub.handle(completeEv)
	require.Len(t, fn.snapshot(), 1)

	// Simulate user removing + re-adding + completion again.
	sub.handle(engine.EngineEvent{Kind: engine.EventRemoved, ID: id})
	sub.handle(completeEv)

	require.Len(t, fn.snapshot(), 2)
}

// TestSubscriber_TickEventCompletedAlsoTriggers covers the case where
// engine.run() emits EventTick (with Snapshot.Completed=true) instead of
// EventComplete — the subscriber should still treat it as a completion.
func TestSubscriber_TickEventCompletedAlsoTriggers(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnComplete: true})

	sub.handle(engine.EngineEvent{
		Kind: engine.EventTick,
		ID:   "t1",
		Snapshot: engine.Snapshot{
			ID: "t1", Name: "tickme", Completed: true,
		},
	})

	require.Len(t, fn.snapshot(), 1)
}

// TestSubscriber_TickEventNotCompletedIsIgnored makes sure regular ticks of
// in-progress torrents don't generate notifications.
func TestSubscriber_TickEventNotCompletedIsIgnored(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnComplete: true})

	sub.handle(engine.EngineEvent{
		Kind: engine.EventTick,
		ID:   "t2",
		Snapshot: engine.Snapshot{
			ID: "t2", Name: "downloading", Completed: false,
		},
	})

	require.Empty(t, fn.snapshot())
}

// TestSubscriber_NotifyUpdateInstalled covers the update trigger and toggle.
func TestSubscriber_NotifyUpdateInstalled(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnUpdate: true})

	sub.NotifyUpdateInstalled("v0.2.0")
	calls := fn.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "Mosaic updated", calls[0].Title)
	require.Contains(t, calls[0].Body, "v0.2.0")

	// Disable and re-fire — should not deliver.
	sub.SetSettings(Settings{NotifyOnUpdate: false})
	sub.NotifyUpdateInstalled("v0.3.0")
	require.Len(t, fn.snapshot(), 1)
}

// TestSubscriber_SetSettingsRespectsLatestToggleAtFireTime ensures that
// flipping a toggle off between events stops further notifications even
// though the subscriber was constructed with the toggle on.
func TestSubscriber_SetSettingsRespectsLatestToggleAtFireTime(t *testing.T) {
	fn := &fakeNotifier{}
	sub := NewSubscriber(fn, Settings{NotifyOnComplete: true})

	sub.handle(engine.EngineEvent{
		Kind:     engine.EventComplete,
		ID:       "a",
		Snapshot: engine.Snapshot{ID: "a", Name: "first", Completed: true},
	})
	require.Len(t, fn.snapshot(), 1)

	sub.SetSettings(Settings{NotifyOnComplete: false})
	sub.handle(engine.EngineEvent{
		Kind:     engine.EventComplete,
		ID:       "b",
		Snapshot: engine.Snapshot{ID: "b", Name: "second", Completed: true},
	})
	require.Len(t, fn.snapshot(), 1, "second torrent should not fire while toggle is off")
}

func TestNoopNotifier(t *testing.T) {
	require.NoError(t, NoopNotifier{}.Notify("a", "b", "c"))
}

func TestTruncate(t *testing.T) {
	require.Equal(t, "abc", truncate("abc", 5))
	require.Equal(t, "ab…", truncate("abcdef", 3))
	require.Equal(t, "a", truncate("abc", 1))
}
