package tray

import (
	"runtime"
	"testing"
)

// TestNew_constructAndStop is the minimal smoke test: build a Tray with
// nil-ish callbacks and tear it down. On macOS this runs against the no-op
// stub; on Linux/Windows we skip Start (which would actually try to draw
// a tray icon and block the test process). The point is to make sure
// (a) the package compiles per-platform and (b) wiring doesn't panic.
func TestNew_constructAndStop(t *testing.T) {
	if runtime.GOOS != "darwin" {
		// On Linux/Windows systray.Run binds an OS thread and depends on
		// an X/D-Bus/Win32 event loop that doesn't exist in CI — exercising
		// Start would either deadlock or write to a missing display. We
		// still verify construction + Stop are panic-free.
	}

	tr := New(Callbacks{})
	if tr == nil {
		t.Fatal("New returned nil")
	}

	// State setters should not panic before Start.
	tr.SetIconState(IconActive)
	tr.SetIconState(IconError)
	tr.SetIconState(IconIdle)
	tr.SetPaused(true)
	tr.SetPaused(false)
	tr.SetAltSpeedActive(true)
	tr.SetAltSpeedActive(false)

	// Stop without Start must be safe (the impl tracks started/stopped flags).
	tr.Stop()
	tr.Stop() // idempotent
}

func TestNew_callbacksWiredButNotInvoked(t *testing.T) {
	called := 0
	cb := Callbacks{
		OnShow:           func() { called++ },
		OnPauseAll:       func() { called++ },
		OnResumeAll:      func() { called++ },
		OnToggleAltSpeed: func() { called++ },
		OnOpenSettings:   func() { called++ },
		OnQuit:           func() { called++ },
	}
	tr := New(cb)
	tr.Stop()
	if called != 0 {
		t.Fatalf("expected callbacks not to fire on construction; got %d", called)
	}
}
