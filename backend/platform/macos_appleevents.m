//go:build darwin

// macOS Apple Event handler bridge.
//
// Finder file-open ('Open With Mosaic') and browser magnet:// clicks both
// dispatch via Apple Events (kAEOpenDocuments / kAEGetURL respectively),
// not via argv. Wails doesn't expose hooks for these. This bridge registers
// our own NSAppleEventManager handlers and forwards each event to Go via
// the cgo-exported callbacks declared in appleevents_darwin.go.

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>

#include "macos_appleevents.h"
#include "_cgo_export.h"

@interface MosaicAppleEventHandler : NSObject
+ (instancetype)shared;
- (void)install;
- (void)handleGetURL:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent;
- (void)handleOpenDocuments:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent;
@end

@implementation MosaicAppleEventHandler

+ (instancetype)shared {
    static MosaicAppleEventHandler *s;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ s = [[MosaicAppleEventHandler alloc] init]; });
    return s;
}

- (void)install {
    NSAppleEventManager *m = [NSAppleEventManager sharedAppleEventManager];
    [m setEventHandler:self
          andSelector:@selector(handleGetURL:withReplyEvent:)
        forEventClass:kInternetEventClass
           andEventID:kAEGetURL];
    [m setEventHandler:self
          andSelector:@selector(handleOpenDocuments:withReplyEvent:)
        forEventClass:kCoreEventClass
           andEventID:kAEOpenDocuments];
}

- (void)handleGetURL:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent {
    NSString *url = [[event paramDescriptorForKeyword:keyDirectObject] stringValue];
    if (url.length == 0) return;
    goHandleURL((char *)[url UTF8String]);
}

- (void)handleOpenDocuments:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent {
    NSAppleEventDescriptor *list = [event paramDescriptorForKeyword:keyDirectObject];
    if (list == nil) return;
    NSInteger count = [list numberOfItems];
    for (NSInteger i = 1; i <= count; i++) {
        NSAppleEventDescriptor *item = [list descriptorAtIndex:i];
        // Each item is typically a typeFileURL alias. Coerce to file URL,
        // then read its data, build NSURL, get the path.
        NSAppleEventDescriptor *coerced = [item coerceToDescriptorType:typeFileURL];
        NSString *path = nil;
        if (coerced != nil) {
            NSData *urlData = [coerced data];
            NSURL *url = [NSURL URLWithDataRepresentation:urlData relativeToURL:nil];
            path = [url path];
        }
        if (path.length > 0) {
            goHandleFile((char *)[path UTF8String]);
        }
    }
}

@end

void mosaic_install_apple_event_handlers(void) {
    [[MosaicAppleEventHandler shared] install];
}
