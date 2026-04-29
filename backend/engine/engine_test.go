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
