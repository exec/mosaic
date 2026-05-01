//go:build darwin

#ifndef MOSAIC_TRAY_DARWIN_H
#define MOSAIC_TRAY_DARWIN_H

#include <stdint.h>
#include <stddef.h>

// Item IDs used by both the ObjC side (when invoking goTrayMenuClicked) and
// the Go side (when routing the click into Callbacks). Kept in sync with the
// switch in tray_darwin.go.
#define MOSAIC_TRAY_ITEM_SHOW       1
#define MOSAIC_TRAY_ITEM_PAUSE_ALL  2
#define MOSAIC_TRAY_ITEM_ALT_SPEED  3
#define MOSAIC_TRAY_ITEM_SETTINGS   4
#define MOSAIC_TRAY_ITEM_QUIT       5

// MosaicTrayCreate allocates a new NSStatusItem-backed tray controller and
// returns an opaque pointer to it. handleID is the cgo.Handle integer the
// Go side wants the controller to pass back when it invokes goTrayMenuClicked.
//
// All AppKit work happens on the main queue via dispatch_async; this call is
// safe from any goroutine. NSApplication must already exist (Wails creates
// it before our tray.Start runs).
//
// Returns NULL on failure.
void *MosaicTrayCreate(uintptr_t handleID);

// MosaicTrayBuildMenu rebuilds the menu items with the supplied labels. The
// label arguments are UTF-8 NUL-terminated C strings; ObjC copies them into
// NSStrings, so the caller can free them after the call returns. Safe to
// call multiple times to refresh labels.
//
// pauseLabel: e.g. "Pause all" or "Resume all"
// altSpeedLabel: e.g. "Alt-speed: ON" or "Alt-speed: OFF"
void MosaicTrayBuildMenu(void *controller,
                         const char *showLabel,
                         const char *pauseLabel,
                         const char *altSpeedLabel,
                         const char *settingsLabel,
                         const char *quitLabel);

// MosaicTraySetIcon swaps the status item's image. iconBytes points at PNG
// data of length iconLen; ObjC wraps it in NSImage via initWithData:. If
// isTemplate is non-zero the image's template flag is set so macOS auto-tints
// it for the menubar's light/dark appearance.
//
// A NULL or zero-length buffer leaves the existing icon in place.
void MosaicTraySetIcon(void *controller,
                       const void *iconBytes,
                       size_t iconLen,
                       int isTemplate);

// MosaicTrayDestroy removes the status item from the system status bar and
// releases the controller. After this call the pointer is invalid.
void MosaicTrayDestroy(void *controller);

#endif
