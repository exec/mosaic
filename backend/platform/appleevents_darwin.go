//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework AppKit
#include "macos_appleevents.h"
*/
import "C"

import "sync"

// Apple Event delivery on macOS for Finder file-open and browser magnet:
// URL handoff. Wails on darwin doesn't expose either; we wire our own
// NSAppleEventManager handler in macos_appleevents.m and pipe the event
// through cgo-exported callbacks back into Go.

var (
	handlerMu   sync.Mutex
	fileHandler func(string)
	urlHandler  func(string)
)

// InstallAppleEventHandlers registers NSAppleEventManager handlers for
// kAEOpenDocuments + kAEGetURL. Subsequent file/URL events fire onFile and
// onURL respectively, with the file path or URL string.
//
// Call this once early in startup (after Service is alive but ideally
// before the user can interact). Calling more than once replaces the
// previous handlers.
func InstallAppleEventHandlers(onFile, onURL func(string)) {
	handlerMu.Lock()
	fileHandler = onFile
	urlHandler = onURL
	handlerMu.Unlock()
	C.mosaic_install_apple_event_handlers()
}

//export goHandleFile
func goHandleFile(cpath *C.char) {
	if cpath == nil {
		return
	}
	p := C.GoString(cpath)
	handlerMu.Lock()
	h := fileHandler
	handlerMu.Unlock()
	if h != nil {
		go h(p)
	}
}

//export goHandleURL
func goHandleURL(curl *C.char) {
	if curl == nil {
		return
	}
	u := C.GoString(curl)
	handlerMu.Lock()
	h := urlHandler
	handlerMu.Unlock()
	if h != nil {
		go h(u)
	}
}
