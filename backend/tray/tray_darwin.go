//go:build darwin

package tray

// macOS tray is currently a no-op stub.
//
// TODO(macos): native NSStatusItem via Cgo. backend/platform/macos_appleevents.{h,m}
// shows the in-tree pattern for Cgo on darwin (Objective-C side compiled with
// CGO_ENABLED=1, header exposed to Go via cgo directives). When implementing,
// we want NSStatusItemBehaviorTerminationOnRemoval semantics and a template
// image (NSImage with isTemplate=true) so the icon adapts to dark/light menu
// bars. Until that lands the user-facing impact is:
//   - On macOS, the "Show Mosaic" / "Pause all" / "Alt-speed" / "Settings…"
//     / "Quit Mosaic" menu items are unavailable from the menu bar.
//   - Close-to-tray is also disabled on macOS by spec (the existing
//     hide-window-on-close convention from Wails options handles X-button).
//   - All other functionality (window controls, notifications via beeep's
//     native osascript path, Settings, etc.) works as on Linux/Windows.

// energye/systray does compile on darwin, but bringing it up here would clash
// with Wails' own NSApp event loop and crash on startup; the supported
// integration path on macOS is a Cgo-managed NSStatusItem owned by us.

type darwinImpl struct{}

func newImpl(_ *Tray) trayImpl { return &darwinImpl{} }

func (darwinImpl) start()         {}
func (darwinImpl) stop()          {}
func (darwinImpl) refreshIcon()   {}
func (darwinImpl) refreshLabels() {}
