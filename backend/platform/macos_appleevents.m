//go:build darwin

// macOS Apple Event handler bridge.
//
// Finder file-open ("Open With Mosaic") and browser magnet:// clicks both
// dispatch via Apple Events (kAEOpenDocuments / kAEGetURL respectively),
// not via argv. Wails doesn't expose hooks for these. This bridge registers
// our own NSAppleEventManager handlers and forwards each event to Go via
// the cgo-exported callbacks declared in appleevents_darwin.go.
//
// Critically, NSAppleEventManager dispatches the launch-time event during
// applicationWillFinishLaunching: — earlier than Wails's OnStartup callback.
// If we register only when Go's startup runs, the launch event has already
// been dropped. We register from +load instead (runs at dylib load, before
// main()) and queue events until the Go bridge signals readiness via
// mosaic_install_apple_event_handlers, then drain.

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>

#include "macos_appleevents.h"
#include "_cgo_export.h"

@interface MosaicAppleEventHandler : NSObject
+ (instancetype)shared;
- (void)handleGetURL:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent;
- (void)handleOpenDocuments:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent;
@end

// Pending-queue entry: tag is "F" (file) or "U" (url); value is the path/url.
@interface MosaicPendingEvent : NSObject
@property (nonatomic, copy) NSString *tag;
@property (nonatomic, copy) NSString *value;
@end
@implementation MosaicPendingEvent
@end

static NSMutableArray<MosaicPendingEvent *> *gPending;
static BOOL gReady;
static NSLock *gLock;

@implementation MosaicAppleEventHandler

+ (void)load {
    // Runs at dylib load time, before main(). Register Apple Event
    // handlers immediately so launch-time kAEOpenDocuments / kAEGetURL
    // get captured even before the Go runtime starts. Events that arrive
    // before the Go bridge calls mosaic_install_apple_event_handlers are
    // queued and replayed on bridge install.
    @autoreleasepool {
        gPending = [[NSMutableArray alloc] init];
        gLock = [[NSLock alloc] init];
        NSAppleEventManager *m = [NSAppleEventManager sharedAppleEventManager];
        MosaicAppleEventHandler *h = [MosaicAppleEventHandler shared];
        [m setEventHandler:h
              andSelector:@selector(handleGetURL:withReplyEvent:)
            forEventClass:kInternetEventClass
               andEventID:kAEGetURL];
        [m setEventHandler:h
              andSelector:@selector(handleOpenDocuments:withReplyEvent:)
            forEventClass:kCoreEventClass
               andEventID:kAEOpenDocuments];
    }
}

+ (instancetype)shared {
    static MosaicAppleEventHandler *s;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ s = [[MosaicAppleEventHandler alloc] init]; });
    return s;
}

- (void)dispatchOrQueue:(NSString *)tag value:(NSString *)value {
    if (value.length == 0) return;
    [gLock lock];
    BOOL ready = gReady;
    if (!ready) {
        MosaicPendingEvent *p = [MosaicPendingEvent new];
        p.tag = tag;
        p.value = value;
        [gPending addObject:p];
    }
    [gLock unlock];
    if (!ready) return;
    if ([tag isEqualToString:@"F"]) {
        goHandleFile((char *)[value UTF8String]);
    } else {
        goHandleURL((char *)[value UTF8String]);
    }
}

- (void)handleGetURL:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent {
    NSString *url = [[event paramDescriptorForKeyword:keyDirectObject] stringValue];
    [self dispatchOrQueue:@"U" value:url];
}

- (void)handleOpenDocuments:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent {
    NSAppleEventDescriptor *list = [event paramDescriptorForKeyword:keyDirectObject];
    if (list == nil) return;
    NSInteger count = [list numberOfItems];
    for (NSInteger i = 1; i <= count; i++) {
        NSAppleEventDescriptor *item = [list descriptorAtIndex:i];
        NSAppleEventDescriptor *coerced = [item coerceToDescriptorType:typeFileURL];
        NSString *path = nil;
        if (coerced != nil) {
            NSData *urlData = [coerced data];
            NSURL *url = [NSURL URLWithDataRepresentation:urlData relativeToURL:nil];
            path = [url path];
        }
        if (path.length > 0) {
            [self dispatchOrQueue:@"F" value:path];
        }
    }
}

@end

void mosaic_install_apple_event_handlers(void) {
    // Handlers are already wired in +load; this call signals that the Go
    // bridge is ready to receive events. Drain anything that arrived
    // pre-startup (the typical Finder-double-click launch case).
    NSArray<MosaicPendingEvent *> *drain = nil;
    [gLock lock];
    gReady = YES;
    drain = [gPending copy];
    [gPending removeAllObjects];
    [gLock unlock];
    for (MosaicPendingEvent *p in drain) {
        if ([p.tag isEqualToString:@"F"]) {
            goHandleFile((char *)[p.value UTF8String]);
        } else {
            goHandleURL((char *)[p.value UTF8String]);
        }
    }
}
