package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEngine_AddMagnet_EmitsAddedEvent(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	sub := eng.Subscribe()

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc", "/tmp")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	select {
	case ev := <-sub:
		require.Equal(t, EventAdded, ev.Kind)
		require.Equal(t, id, ev.ID)
	case <-time.After(time.Second):
		t.Fatal("expected EventAdded within 1s")
	}
}

func TestEngine_Tick_EmitsForActiveTorrents(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 30*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	sub := eng.Subscribe()
	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:xyz", "/tmp")
	require.NoError(t, err)

	// drain Added
	<-sub

	fb.AdvanceProgress(id, 1024)

	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Kind == EventTick && ev.ID == id {
				return // success
			}
		case <-deadline:
			t.Fatal("expected EventTick within 1s")
		}
	}
}

func TestEngine_Remove_EmitsRemovedEvent(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:rm", "/tmp")
	require.NoError(t, err)

	sub := eng.Subscribe()
	require.NoError(t, eng.Remove(id, false))

	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Kind == EventRemoved && ev.ID == id {
				return
			}
		case <-deadline:
			t.Fatal("expected EventRemoved within 1s")
		}
	}
}

func TestEngine_DetailedSnapshot_RoutesThroughBackend(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:detail", "/tmp")
	require.NoError(t, err)

	d, err := eng.DetailedSnapshot(id, DetailScope{Files: true, Peers: true, Trackers: true})
	require.NoError(t, err)
	require.Equal(t, id, d.Snapshot.ID)
	// FakeBackend's seeded fixture returns 2 files / 1 peer / 1 tracker
	require.Len(t, d.Files, 2)
	require.Len(t, d.Peers, 1)
	require.Len(t, d.Trackers, 1)
}

func TestEngine_DetailedSnapshot_ScopeFlagsExcludeData(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:scope", "/tmp")

	// All three scopes off
	d, err := eng.DetailedSnapshot(id, DetailScope{})
	require.NoError(t, err)
	require.Empty(t, d.Files)
	require.Empty(t, d.Peers)
	require.Empty(t, d.Trackers)
}

func TestEngine_Snapshot_HasSeparateBytesAndRateFields(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:fields", "/tmp")
	require.NoError(t, err)
	fb.AdvanceProgress(id, 1024)

	snap, err := eng.Snapshot(id)
	require.NoError(t, err)
	require.Equal(t, int64(1024), snap.BytesDone)
	// BytesDown/BytesUp default to 0 in the fake; RateDown/RateUp default to 0
	require.Equal(t, int64(0), snap.BytesDown)
	require.Equal(t, int64(0), snap.BytesUp)
	require.Equal(t, int64(0), snap.RateDown)
	require.Equal(t, int64(0), snap.RateUp)
}

func TestEngine_Pause_ReflectsInSnapshot(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:pause", "/tmp")
	require.NoError(t, err)

	snap, _ := eng.Snapshot(id)
	require.False(t, snap.Paused, "fresh torrent is not paused")

	require.NoError(t, eng.Pause(id))
	snap, _ = eng.Snapshot(id)
	require.True(t, snap.Paused, "after Pause, Snapshot.Paused should be true")

	require.NoError(t, eng.Resume(id))
	snap, _ = eng.Snapshot(id)
	require.False(t, snap.Paused, "after Resume, Snapshot.Paused should be false")
}

func TestEngine_SetFilePriorities(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:prio", "/tmp")

	require.NoError(t, eng.SetFilePriorities(id, map[int]Priority{
		0: PriorityHigh,
		1: PrioritySkip,
	}))

	d, _ := eng.DetailedSnapshot(id, DetailScope{Files: true})
	require.Len(t, d.Files, 2)
	for _, f := range d.Files {
		switch f.Index {
		case 0:
			require.Equal(t, PriorityHigh, f.Priority)
		case 1:
			require.Equal(t, PrioritySkip, f.Priority)
		}
	}
}
