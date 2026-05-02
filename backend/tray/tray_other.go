//go:build !darwin

package tray

import (
	_ "embed"
	"runtime"
	"sync"

	"github.com/energye/systray"
	"github.com/rs/zerolog/log"
)

// Embedded icon assets. The build/ directory ships PNGs; Windows technically
// expects ICO bytes for the systray surface — if you're packaging for Windows
// and the tray icon doesn't render, drop ICO equivalents at the same paths
// and they'll be picked up here automatically.
//
// TODO(tray-icons): if the variant PNGs are missing, fall back to appicon.png
// at runtime instead of failing the embed; for now we ship single-variant
// fallbacks via go:embed defaults.

//go:embed icons/idle.png
var iconIdleBytes []byte

//go:embed icons/active.png
var iconActiveBytes []byte

//go:embed icons/error.png
var iconErrorBytes []byte

type otherImpl struct {
	t *Tray

	// Items must be assigned inside the systray onReady callback (the systray
	// lib is not safe to call before Register).
	itemMu       sync.Mutex
	showItem     *systray.MenuItem
	pauseItem    *systray.MenuItem
	altSpeedItem *systray.MenuItem
	settingsItem *systray.MenuItem
	quitItem     *systray.MenuItem
	ready        bool
}

func newImpl(t *Tray) trayImpl { return &otherImpl{t: t} }

func (o *otherImpl) start() {
	go func() {
		// systray.Run blocks until Quit is called. It's the canonical entry
		// point and guarantees the LockOSThread invariant the platform tray
		// APIs require.
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("tray: panic in systray.Run")
			}
		}()
		systray.Run(o.onReady, o.onExit)
	}()
}

func (o *otherImpl) stop() {
	systray.Quit()
}

func (o *otherImpl) refreshIcon() {
	o.itemMu.Lock()
	ready := o.ready
	o.itemMu.Unlock()
	if !ready {
		return
	}
	systray.SetIcon(o.iconBytesForState())
}

func (o *otherImpl) refreshLabels() {
	o.itemMu.Lock()
	defer o.itemMu.Unlock()
	if !o.ready {
		return
	}
	if o.pauseItem != nil {
		if o.t.paused.Load() {
			o.pauseItem.SetTitle("Resume all")
		} else {
			o.pauseItem.SetTitle("Pause all")
		}
	}
	if o.altSpeedItem != nil {
		if o.t.altSpeedActive.Load() {
			o.altSpeedItem.SetTitle("Alt-speed: ON")
		} else {
			o.altSpeedItem.SetTitle("Alt-speed: OFF")
		}
	}
}

func (o *otherImpl) iconBytesForState() []byte {
	var raw []byte
	switch IconState(o.t.iconState.Load()) {
	case IconActive:
		raw = iconActiveBytes
	case IconError:
		raw = iconErrorBytes
	}
	if len(raw) == 0 {
		raw = iconIdleBytes
	}
	// energye/systray on Linux feeds the PNG bytes straight to GTK /
	// libayatana-appindicator (which decodes PNG natively). On Windows
	// the same Set call expects an ICO — Win32 NotifyIcon's HICON loader
	// rejects raw PNG bytes silently, which is why the tray entry was
	// appearing icon-less. Wrap the PNG in a one-image ICO container so
	// the same source asset works on both. ICO has supported PNG-encoded
	// entries since Vista, so the wrapped file decodes fine on every
	// supported Windows version.
	if runtime.GOOS == "windows" {
		return wrapPNGAsICO(raw)
	}
	return raw
}

// wrapPNGAsICO produces a single-image ICO whose payload is the input
// PNG verbatim. Layout:
//
//	0..5    ICONDIR    reserved=0, type=1 (icon), count=1
//	6..21   ICONDIRENTRY  width, height, 0, 0, planes=1, bitCount=32,
//	                       sizeBytes (LE u32), offset=22 (LE u32)
//	22..N   PNG bytes
//
// Width/height in the directory entry are advisory — Windows reads the
// real dimensions from the PNG IHDR. We pass 0 (which means 256 per
// the ICO spec) because our source PNGs are 36×36; the actual render
// path scales to whatever the shell needs (typically 16×16 for the
// system tray).
func wrapPNGAsICO(png []byte) []byte {
	const headerSize = 22
	out := make([]byte, headerSize+len(png))
	// ICONDIR
	out[0] = 0
	out[1] = 0
	out[2] = 1 // type = icon
	out[3] = 0
	out[4] = 1 // image count = 1
	out[5] = 0
	// ICONDIRENTRY
	out[6] = 0  // width (0 = 256 / "see PNG IHDR")
	out[7] = 0  // height
	out[8] = 0  // colorCount (0 = >=256)
	out[9] = 0  // reserved
	out[10] = 1 // planes
	out[11] = 0
	out[12] = 32 // bitCount
	out[13] = 0
	sz := uint32(len(png))
	out[14] = byte(sz)
	out[15] = byte(sz >> 8)
	out[16] = byte(sz >> 16)
	out[17] = byte(sz >> 24)
	out[18] = headerSize // offset = 22
	out[19] = 0
	out[20] = 0
	out[21] = 0
	copy(out[headerSize:], png)
	return out
}

func (o *otherImpl) onReady() {
	systray.SetTitle("Mosaic")
	systray.SetTooltip("Mosaic — BitTorrent client")
	systray.SetIcon(o.iconBytesForState())
	// Single-click on the tray icon = "Show Mosaic". Matches qBittorrent /
	// Discord / Steam / Slack — left-click reveals the window without
	// requiring the user to fish through a context menu. Pre-fix the only
	// way back to the window was right-click → "Show Mosaic", which most
	// users (reasonably) didn't think to try.
	//
	// energye/systray dispatches onClick on left-click on Windows + Linux
	// (it's not wired on macOS — we use NSStatusItem there directly via
	// tray_darwin.{m,h}, which has its own click semantics).
	systray.SetOnClick(func(_ systray.IMenu) {
		if o.t.cb.OnShow != nil {
			o.t.cb.OnShow()
		}
	})

	o.itemMu.Lock()

	o.showItem = systray.AddMenuItem("Show Mosaic", "Show the Mosaic window")
	o.showItem.Click(func() {
		if o.t.cb.OnShow != nil {
			o.t.cb.OnShow()
		}
	})

	systray.AddSeparator()

	pauseLabel := "Pause all"
	if o.t.paused.Load() {
		pauseLabel = "Resume all"
	}
	o.pauseItem = systray.AddMenuItem(pauseLabel, "Pause/resume all torrents")
	o.pauseItem.Click(func() {
		if o.t.paused.Load() {
			if o.t.cb.OnResumeAll != nil {
				o.t.cb.OnResumeAll()
			}
		} else {
			if o.t.cb.OnPauseAll != nil {
				o.t.cb.OnPauseAll()
			}
		}
	})

	altLabel := "Alt-speed: OFF"
	if o.t.altSpeedActive.Load() {
		altLabel = "Alt-speed: ON"
	}
	o.altSpeedItem = systray.AddMenuItem(altLabel, "Toggle alternate speed limits")
	o.altSpeedItem.Click(func() {
		if o.t.cb.OnToggleAltSpeed != nil {
			o.t.cb.OnToggleAltSpeed()
		}
	})

	systray.AddSeparator()

	o.settingsItem = systray.AddMenuItem("Settings…", "Open Mosaic settings")
	o.settingsItem.Click(func() {
		if o.t.cb.OnOpenSettings != nil {
			o.t.cb.OnOpenSettings()
		}
	})

	o.quitItem = systray.AddMenuItem("Quit Mosaic", "Quit the application")
	o.quitItem.Click(func() {
		if o.t.cb.OnQuit != nil {
			o.t.cb.OnQuit()
		}
	})

	o.ready = true
	o.itemMu.Unlock()
}

func (o *otherImpl) onExit() {
	o.itemMu.Lock()
	o.ready = false
	o.itemMu.Unlock()
}
