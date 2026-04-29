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
