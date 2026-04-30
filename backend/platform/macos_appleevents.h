#ifndef MOSAIC_MACOS_APPLEEVENTS_H
#define MOSAIC_MACOS_APPLEEVENTS_H

// Installs handlers with NSAppleEventManager for kAEOpenDocuments and
// kAEGetURL. Idempotent. Safe to call before or after NSApplication starts.
//
// When events fire they call back into Go via the cgo-exported functions
// goHandleFile / goHandleURL declared in appleevents_darwin.go.
void mosaic_install_apple_event_handlers(void);

#endif
