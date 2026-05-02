package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduler_PausesOverflow(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	for i := 0; i < 5; i++ {
		id, _ := eng.AddMagnet(context.Background(), fmt.Sprintf("magnet:?xt=urn:btih:s%d", i), "/tmp")
		fb.SetQueuePosition(id, i)
	}

	s := NewScheduler(eng, 2, 0, 50*time.Millisecond)
	t.Cleanup(s.Close)

	require.Eventually(t, func() bool {
		queued := 0
		for _, snap := range eng.List() {
			if snap.Queued {
				queued++
			}
		}
		return queued == 3 // 5 - 2 active = 3 queued
	}, 2*time.Second, 100*time.Millisecond)
}

// Regression: when multiple torrents share the same QueuePosition (the
// state right after a fresh RestoreOnStartup before the user has reordered
// anything — or any time queue positions tie), sort.Slice's UNSTABLE
// ordering used to rotate which torrent got ScheduledPaused each tick.
// With 2s ticks and SetMaxEstablishedConns(0/80) firing on each rotation,
// peers churned and downloads/seeds never progressed. Tie-breaking on ID
// makes the chosen victim deterministic per process — same torrent stays
// queued, the rest stay running, peers don't churn.
func TestScheduler_TieBreaksDeterministicallyOnEqualQueuePosition(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	// Three torrents, all at QueuePosition 0 (the default). With cap=2,
	// exactly one must be queued — and it must be the same one across
	// many ticks.
	ids := make([]TorrentID, 3)
	for i := 0; i < 3; i++ {
		id, _ := eng.AddMagnet(context.Background(), fmt.Sprintf("magnet:?xt=urn:btih:t%d", i), "/tmp")
		ids[i] = id
		// Leave QueuePosition at default 0 — the tie-break is what's under test.
	}

	s := NewScheduler(eng, 2, 0, 25*time.Millisecond)
	t.Cleanup(s.Close)

	queuedVictim := func() TorrentID {
		for _, snap := range eng.List() {
			if snap.Queued {
				return snap.ID
			}
		}
		return ""
	}

	// Wait for the first stable selection.
	require.Eventually(t, func() bool { return queuedVictim() != "" }, time.Second, 25*time.Millisecond)
	first := queuedVictim()

	// Run for 30 ticks (~750ms with 25ms tick). Victim must not change.
	for i := 0; i < 30; i++ {
		time.Sleep(25 * time.Millisecond)
		require.Equal(t, first, queuedVictim(), "scheduler victim must be stable across ticks")
	}
}

func TestScheduler_ForceStartBypassesLimit(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id1, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:f1", "/tmp")
	id2, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:f2", "/tmp")
	id3, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:f3", "/tmp")
	fb.SetQueuePosition(id1, 0)
	fb.SetQueuePosition(id2, 1)
	fb.SetQueuePosition(id3, 2)
	fb.SetForceStart(id3, true) // bottom of queue but force-started

	s := NewScheduler(eng, 1, 0, 50*time.Millisecond)
	t.Cleanup(s.Close)

	require.Eventually(t, func() bool {
		var snap1, snap3 Snapshot
		for _, s := range eng.List() {
			if s.ID == id1 {
				snap1 = s
			}
			if s.ID == id3 {
				snap3 = s
			}
		}
		return !snap1.Queued && !snap3.Queued
	}, 2*time.Second, 100*time.Millisecond)
}
