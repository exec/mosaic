//go:build !darwin

package tray

import (
	_ "embed"
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
	switch IconState(o.t.iconState.Load()) {
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

func (o *otherImpl) onReady() {
	systray.SetTitle("Mosaic")
	systray.SetTooltip("Mosaic — BitTorrent client")
	systray.SetIcon(o.iconBytesForState())

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
