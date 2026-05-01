// Package tray exposes a small cross-platform system-tray surface backed by
// github.com/energye/systray on Linux+Windows and a no-op stub on macOS.
//
// The package is callback-driven: the embedder (main.go) registers a Callbacks
// struct describing how to react to each menu item, then calls Start to spin
// up the tray's event loop in a goroutine. Stop tears it down on shutdown.
//
// Icon variants (Idle/Active/Error) are runtime-mutable via SetIconState so
// the engine tick can reflect "downloads happening now" without re-creating
// the tray. Menu labels can also be updated mid-flight via SetPaused /
// SetAltSpeedActive.
package tray

import (
	"sync/atomic"
)

// IconState selects which embedded PNG variant to display.
type IconState int

const (
	IconIdle   IconState = iota // greyscale, nothing happening
	IconActive                  // accent-dot, at least one torrent downloading
	IconError                   // red-dot, something is wrong
)

// Callbacks is the set of user-action handlers wired by main.go.
//
// All callbacks are invoked from the systray event-loop goroutine; handlers
// must be cheap or hand off to another goroutine. Nil callbacks are safe and
// silently ignored.
type Callbacks struct {
	OnShow             func()       // "Show Mosaic" — un-minimise + show window
	OnPauseAll         func()       // "Pause all"
	OnResumeAll        func()       // "Resume all"
	OnToggleAltSpeed   func()       // "Alt-speed: ON/OFF"
	OnOpenSettings     func()       // "Settings…" — show window + emit navigate:settings
	OnQuit             func()       // "Quit Mosaic" — bypasses close-to-tray hook
}

// Tray is the handle the embedder holds; it's safe to call any method before
// Start (the call is buffered) or after Stop (the call becomes a no-op).
type Tray struct {
	cb        Callbacks
	started   atomic.Bool
	stopped   atomic.Bool

	// Mirrored state — read by the systray event loop when refreshing labels.
	paused          atomic.Bool
	altSpeedActive  atomic.Bool
	iconState       atomic.Int32 // IconState

	// Implementation-specific: see tray_other.go / tray_darwin.go.
	impl trayImpl
}

// New returns a Tray ready to be Start()ed. cb may have nil fields — the menu
// items still render but click-handlers no-op.
func New(cb Callbacks) *Tray {
	t := &Tray{cb: cb}
	t.impl = newImpl(t)
	t.iconState.Store(int32(IconIdle))
	return t
}

// Start begins the tray's event loop. On Linux/Windows this spawns a goroutine
// running energye/systray.Register + nativeLoop equivalents; on macOS this is
// a no-op until the native NSStatusItem path is wired.
func (t *Tray) Start() {
	if !t.started.CompareAndSwap(false, true) {
		return
	}
	t.impl.start()
}

// Stop signals the tray to tear down. Safe to call multiple times.
func (t *Tray) Stop() {
	if !t.stopped.CompareAndSwap(false, true) {
		return
	}
	t.impl.stop()
}

// SetIconState swaps the active icon variant. Callable from any goroutine.
func (t *Tray) SetIconState(s IconState) {
	prev := IconState(t.iconState.Swap(int32(s)))
	if prev == s {
		return
	}
	t.impl.refreshIcon()
}

// SetPaused tells the tray whether the engine is in a "globally paused" state
// so the toggle item can show "Pause all" vs "Resume all".
func (t *Tray) SetPaused(paused bool) {
	prev := t.paused.Swap(paused)
	if prev == paused {
		return
	}
	t.impl.refreshLabels()
}

// SetAltSpeedActive updates the alt-speed toggle label.
func (t *Tray) SetAltSpeedActive(active bool) {
	prev := t.altSpeedActive.Swap(active)
	if prev == active {
		return
	}
	t.impl.refreshLabels()
}

// trayImpl is the platform-specific surface; see tray_other.go / tray_darwin.go.
type trayImpl interface {
	start()
	stop()
	refreshIcon()
	refreshLabels()
}
