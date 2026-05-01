//go:build darwin

// Native macOS NSStatusItem bridge for the Mosaic tray.
//
// energye/systray would otherwise own its own NSApplication runloop, which
// fights Wails for the main thread and crashes on startup. We sidestep that
// by talking to AppKit ourselves: the status bar item lives on the main
// queue, and every mutation gets dispatched there. NSApp is owned by Wails
// at the point tray.Start runs, so we never call [NSApp run] / sharedApplication
// run modes — we just attach our NSStatusItem to the existing menu bar.
//
// The Go side passes a cgo.Handle integer down at construction time; we hand
// that integer back to goTrayMenuClicked when a menu item fires so Go can
// dereference it to the right *Tray and route the callback.

#import <Cocoa/Cocoa.h>
#import <AppKit/AppKit.h>
#import <dispatch/dispatch.h>

#include "tray_darwin.h"
#include "_cgo_export.h"

@interface MosaicTrayController : NSObject {
@public
    uintptr_t goHandle;
    NSStatusItem *statusItem;
    NSMenu *menu;
    NSMenuItem *showItem;
    NSMenuItem *pauseItem;
    NSMenuItem *altSpeedItem;
    NSMenuItem *settingsItem;
    NSMenuItem *quitItem;
}
- (instancetype)initWithHandle:(uintptr_t)h;
- (void)installOnMainThread;
- (void)removeOnMainThread;

// Selectors wired to NSMenuItem target/action.
- (void)onShow:(id)sender;
- (void)onPauseAll:(id)sender;
- (void)onAltSpeed:(id)sender;
- (void)onSettings:(id)sender;
- (void)onQuit:(id)sender;
@end

@implementation MosaicTrayController

- (instancetype)initWithHandle:(uintptr_t)h {
    self = [super init];
    if (self) {
        goHandle = h;
        statusItem = nil;
        menu = nil;
    }
    return self;
}

- (void)installOnMainThread {
    // Defensive: Wails should have created NSApp already, but if for some
    // reason it hasn't, force-create the shared instance. Calling
    // sharedApplication is idempotent.
    (void)[NSApplication sharedApplication];

    NSStatusBar *bar = [NSStatusBar systemStatusBar];
    statusItem = [bar statusItemWithLength:NSVariableStatusItemLength];
    // Retain explicitly. ARC is not enabled for this file (matching
    // appleevents_darwin.go's CFLAGS), so we manage references ourselves.
    [statusItem retain];

    menu = [[NSMenu alloc] initWithTitle:@"Mosaic"];
    [menu setAutoenablesItems:NO];

    statusItem.menu = menu;
    if ([statusItem respondsToSelector:@selector(button)]) {
        statusItem.button.toolTip = @"Mosaic";
    }
}

- (void)removeOnMainThread {
    if (statusItem != nil) {
        [[NSStatusBar systemStatusBar] removeStatusItem:statusItem];
        [statusItem release];
        statusItem = nil;
    }
    if (menu != nil) {
        [menu release];
        menu = nil;
    }
    // Menu items were owned by menu (addItem retains); they go away with it.
    showItem = nil;
    pauseItem = nil;
    altSpeedItem = nil;
    settingsItem = nil;
    quitItem = nil;
}

- (NSMenuItem *)addItemWithTitle:(NSString *)title
                          action:(SEL)action
                              tag:(NSInteger)tag {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title
                                                  action:action
                                           keyEquivalent:@""];
    item.target = self;
    item.tag = tag;
    [item setEnabled:YES];
    [menu addItem:item];
    [item release]; // menu retains
    return [menu itemAtIndex:[menu numberOfItems] - 1];
}

- (void)rebuildMenuWithShow:(NSString *)showLabel
                      pause:(NSString *)pauseLabel
                   altSpeed:(NSString *)altSpeedLabel
                   settings:(NSString *)settingsLabel
                       quit:(NSString *)quitLabel {
    if (menu == nil) return;

    [menu removeAllItems];

    showItem = [self addItemWithTitle:showLabel
                               action:@selector(onShow:)
                                  tag:MOSAIC_TRAY_ITEM_SHOW];

    [menu addItem:[NSMenuItem separatorItem]];

    pauseItem = [self addItemWithTitle:pauseLabel
                                action:@selector(onPauseAll:)
                                   tag:MOSAIC_TRAY_ITEM_PAUSE_ALL];

    altSpeedItem = [self addItemWithTitle:altSpeedLabel
                                   action:@selector(onAltSpeed:)
                                      tag:MOSAIC_TRAY_ITEM_ALT_SPEED];

    [menu addItem:[NSMenuItem separatorItem]];

    settingsItem = [self addItemWithTitle:settingsLabel
                                   action:@selector(onSettings:)
                                      tag:MOSAIC_TRAY_ITEM_SETTINGS];

    quitItem = [self addItemWithTitle:quitLabel
                               action:@selector(onQuit:)
                                  tag:MOSAIC_TRAY_ITEM_QUIT];
}

- (void)setIconWithData:(NSData *)data isTemplate:(BOOL)tmpl {
    if (statusItem == nil) return;
    if (data == nil || data.length == 0) return;
    NSImage *image = [[NSImage alloc] initWithData:data];
    if (image == nil) return;
    // 18pt is the canonical menubar height on macOS; scaling here avoids the
    // status item ballooning if the source PNG is larger.
    [image setSize:NSMakeSize(18.0, 18.0)];
    [image setTemplate:tmpl];
    // statusItem.button is the modern (10.10+) API. Wails' minimum target is
    // already past that, so we don't carry a pre-10.10 fallback (and the
    // deprecated -[NSStatusItem setImage:] would fire a warning).
    if ([statusItem respondsToSelector:@selector(button)] && statusItem.button != nil) {
        statusItem.button.image = image;
    }
    [image release];
}

#pragma mark - Menu actions

- (void)onShow:(id)sender {
    goTrayMenuClicked((uintptr_t)goHandle, MOSAIC_TRAY_ITEM_SHOW);
}

- (void)onPauseAll:(id)sender {
    goTrayMenuClicked((uintptr_t)goHandle, MOSAIC_TRAY_ITEM_PAUSE_ALL);
}

- (void)onAltSpeed:(id)sender {
    goTrayMenuClicked((uintptr_t)goHandle, MOSAIC_TRAY_ITEM_ALT_SPEED);
}

- (void)onSettings:(id)sender {
    goTrayMenuClicked((uintptr_t)goHandle, MOSAIC_TRAY_ITEM_SETTINGS);
}

- (void)onQuit:(id)sender {
    goTrayMenuClicked((uintptr_t)goHandle, MOSAIC_TRAY_ITEM_QUIT);
}

@end

#pragma mark - C entry points

static inline NSString *MosaicNSStringOrEmpty(const char *s) {
    if (s == NULL) return @"";
    return [NSString stringWithUTF8String:s];
}

void *MosaicTrayCreate(uintptr_t handleID) {
    MosaicTrayController *controller = [[MosaicTrayController alloc] initWithHandle:handleID];
    if (controller == nil) return NULL;

    // Status item creation must be on the main thread. We block until it's
    // installed so the caller can immediately set the icon / build the menu
    // without racing against the install. dispatch_sync from a non-main
    // thread to the main queue is safe; from the main thread itself it would
    // deadlock, so we special-case it.
    dispatch_block_t install = ^{
        [controller installOnMainThread];
    };
    if ([NSThread isMainThread]) {
        install();
    } else {
        dispatch_sync(dispatch_get_main_queue(), install);
    }

    return (void *)controller;
}

void MosaicTrayBuildMenu(void *controllerPtr,
                         const char *showLabel,
                         const char *pauseLabel,
                         const char *altSpeedLabel,
                         const char *settingsLabel,
                         const char *quitLabel) {
    if (controllerPtr == NULL) return;
    MosaicTrayController *controller = (MosaicTrayController *)controllerPtr;

    NSString *show = MosaicNSStringOrEmpty(showLabel);
    NSString *pause = MosaicNSStringOrEmpty(pauseLabel);
    NSString *alt = MosaicNSStringOrEmpty(altSpeedLabel);
    NSString *settings = MosaicNSStringOrEmpty(settingsLabel);
    NSString *quit = MosaicNSStringOrEmpty(quitLabel);

    // Retain so they survive past the dispatch_async boundary. Released
    // inside the block.
    [show retain];
    [pause retain];
    [alt retain];
    [settings retain];
    [quit retain];

    dispatch_async(dispatch_get_main_queue(), ^{
        [controller rebuildMenuWithShow:show
                                  pause:pause
                               altSpeed:alt
                               settings:settings
                                   quit:quit];
        [show release];
        [pause release];
        [alt release];
        [settings release];
        [quit release];
    });
}

void MosaicTraySetIcon(void *controllerPtr,
                       const void *iconBytes,
                       size_t iconLen,
                       int isTemplate) {
    if (controllerPtr == NULL) return;
    if (iconBytes == NULL || iconLen == 0) return;
    MosaicTrayController *controller = (MosaicTrayController *)controllerPtr;

    NSData *data = [NSData dataWithBytes:iconBytes length:iconLen];
    [data retain];
    BOOL tmpl = isTemplate != 0 ? YES : NO;

    dispatch_async(dispatch_get_main_queue(), ^{
        [controller setIconWithData:data isTemplate:tmpl];
        [data release];
    });
}

void MosaicTrayDestroy(void *controllerPtr) {
    if (controllerPtr == NULL) return;
    MosaicTrayController *controller = (MosaicTrayController *)controllerPtr;

    dispatch_block_t teardown = ^{
        [controller removeOnMainThread];
        [controller release];
    };
    if ([NSThread isMainThread]) {
        teardown();
    } else {
        dispatch_sync(dispatch_get_main_queue(), teardown);
    }
}
