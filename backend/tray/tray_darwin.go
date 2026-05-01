//go:build darwin

package tray

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc
#cgo LDFLAGS: -framework Cocoa -framework AppKit -framework Foundation
#include <stdlib.h>
#include "tray_darwin.h"
*/
import "C"

import (
	_ "embed"
	"runtime/cgo"
	"sync"
	"unsafe"

	"github.com/rs/zerolog/log"
)

// Native NSStatusItem-backed tray. The energye/systray path on macOS spins
// up its own NSApplication runloop and crashes when Wails owns NSApp; here
// we attach an NSStatusItem to the existing menu bar via Cgo and dispatch
// every AppKit interaction onto the main queue. See tray_darwin.m for the
// ObjC bridge.

// Embedded icon assets — same files as tray_other.go, but keyed off the
// darwin build tag so the !darwin variant doesn't pull them in twice.
//
//go:embed icons/idle.png
var iconIdleBytes []byte

//go:embed icons/active.png
var iconActiveBytes []byte

//go:embed icons/error.png
var iconErrorBytes []byte

// trayTemplateImages controls the [NSImage setTemplate:] flag we apply to
// the status item icon.
//
// TODO(tray-icons): Our shipped placeholders in backend/tray/icons/ are
// colored PNGs, not the monochrome-with-alpha artwork macOS expects for a
// template image. With setTemplate:YES macOS derives a black silhouette
// from the alpha channel, which usually looks correct in the menubar but
// can render as a solid blob if the artwork doesn't carry meaningful alpha.
// Once proper menubar artwork lands (single-color PNG with a transparent
// background, ideally @1x + @2x), this default is the right thing. If a
// user reports the menubar icon looking wrong on dark mode, flip this to
// false and re-test.
const trayTemplateImages = true

type darwinImpl struct {
	t *Tray

	mu         sync.Mutex
	controller unsafe.Pointer // *MosaicTrayController on the ObjC side
	handle     cgo.Handle     // pinned reference to *Tray for goTrayMenuClicked
	running    bool
}

func newImpl(t *Tray) trayImpl { return &darwinImpl{t: t} }

func (d *darwinImpl) start() {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}

	// Pin the *Tray so the ObjC side can pass an integer back across the
	// cgo boundary safely. The handle is released in stop().
	d.handle = cgo.NewHandle(d.t)

	ctrl := C.MosaicTrayCreate(C.uintptr_t(d.handle))
	if ctrl == nil {
		log.Error().Msg("tray: MosaicTrayCreate returned NULL; macOS tray disabled")
		d.handle.Delete()
		d.handle = 0
		d.mu.Unlock()
		return
	}
	d.controller = ctrl
	d.running = true
	d.mu.Unlock()

	// Initial icon + menu population. Both are dispatched onto the main
	// queue inside the ObjC bridge, so it's fine to issue them back-to-back
	// from this goroutine.
	d.applyIcon()
	d.applyMenu()
}

func (d *darwinImpl) stop() {
	d.mu.Lock()
	ctrl := d.controller
	handle := d.handle
	wasRunning := d.running
	d.controller = nil
	d.handle = 0
	d.running = false
	d.mu.Unlock()

	if !wasRunning {
		return
	}
	if ctrl != nil {
		C.MosaicTrayDestroy(ctrl)
	}
	if handle != 0 {
		handle.Delete()
	}
}

func (d *darwinImpl) refreshIcon() {
	d.applyIcon()
}

func (d *darwinImpl) refreshLabels() {
	d.applyMenu()
}

func (d *darwinImpl) applyIcon() {
	d.mu.Lock()
	ctrl := d.controller
	d.mu.Unlock()
	if ctrl == nil {
		return
	}

	bytes := d.iconBytesForState()
	if len(bytes) == 0 {
		return
	}
	tmpl := C.int(0)
	if trayTemplateImages {
		tmpl = 1
	}
	// CBytes copies into C-allocated memory so the slice stays GC-safe; the
	// ObjC bridge wraps it in NSData (which copies again), then we free.
	cbuf := C.CBytes(bytes)
	defer C.free(cbuf)
	C.MosaicTraySetIcon(ctrl, cbuf, C.size_t(len(bytes)), tmpl)
}

func (d *darwinImpl) applyMenu() {
	d.mu.Lock()
	ctrl := d.controller
	d.mu.Unlock()
	if ctrl == nil {
		return
	}

	pauseLabel := "Pause all"
	if d.t.paused.Load() {
		pauseLabel = "Resume all"
	}
	altLabel := "Alt-speed: OFF"
	if d.t.altSpeedActive.Load() {
		altLabel = "Alt-speed: ON"
	}

	cShow := C.CString("Show Mosaic")
	cPause := C.CString(pauseLabel)
	cAlt := C.CString(altLabel)
	cSettings := C.CString("Settings…")
	cQuit := C.CString("Quit Mosaic")
	defer C.free(unsafe.Pointer(cShow))
	defer C.free(unsafe.Pointer(cPause))
	defer C.free(unsafe.Pointer(cAlt))
	defer C.free(unsafe.Pointer(cSettings))
	defer C.free(unsafe.Pointer(cQuit))

	C.MosaicTrayBuildMenu(ctrl, cShow, cPause, cAlt, cSettings, cQuit)
}

func (d *darwinImpl) iconBytesForState() []byte {
	switch IconState(d.t.iconState.Load()) {
	case IconActive:
		if len(iconActiveBytes) > 0 {
			return iconActiveBytes
		}
	case IconError:
		if len(iconErrorBytes) > 0 {
			return iconErrorBytes
		}
	}
	return iconIdleBytes
}

//export goTrayMenuClicked
func goTrayMenuClicked(handleID C.uintptr_t, itemID C.int) {
	h := cgo.Handle(handleID)
	v := h.Value()
	t, ok := v.(*Tray)
	if !ok || t == nil {
		return
	}

	// Mirror tray_other.go's Click handlers: pause/resume splits on the
	// current paused state so we keep behavior identical across platforms.
	switch itemID {
	case C.MOSAIC_TRAY_ITEM_SHOW:
		if t.cb.OnShow != nil {
			t.cb.OnShow()
		}
	case C.MOSAIC_TRAY_ITEM_PAUSE_ALL:
		if t.paused.Load() {
			if t.cb.OnResumeAll != nil {
				t.cb.OnResumeAll()
			}
		} else {
			if t.cb.OnPauseAll != nil {
				t.cb.OnPauseAll()
			}
		}
	case C.MOSAIC_TRAY_ITEM_ALT_SPEED:
		if t.cb.OnToggleAltSpeed != nil {
			t.cb.OnToggleAltSpeed()
		}
	case C.MOSAIC_TRAY_ITEM_SETTINGS:
		if t.cb.OnOpenSettings != nil {
			t.cb.OnOpenSettings()
		}
	case C.MOSAIC_TRAY_ITEM_QUIT:
		if t.cb.OnQuit != nil {
			t.cb.OnQuit()
		}
	}
}
