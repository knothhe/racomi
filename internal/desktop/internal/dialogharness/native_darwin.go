//go:build darwin && nativeintegration && !server

// Package dialogharness operates only on the test application's own native
// panel. It requires neither Accessibility permission nor another application.
package dialogharness

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

static NSOpenPanel *racomiTestPanel(void *native) {
 NSWindow *window = (NSWindow *)native;
 NSWindow *sheet = window.attachedSheet;
 return [sheet isKindOfClass:[NSOpenPanel class]] ? (NSOpenPanel *)sheet : nil;
}

static int racomiTestPanelState(void *native) {
 NSOpenPanel *panel = racomiTestPanel(native);
 if (!panel || !panel.visible) return 0;
 return NSApp.active && panel.sheetParent == (NSWindow *)native ? 2 : 1;
}

static char *racomiTestPanelDescription(void *native) {
 NSWindow *window = (NSWindow *)native;
 return strdup([[NSString stringWithFormat:@"owner %@ visible %d, sheet %@, key window %@, first responder %@, app windows %@", window, window.visible, window.attachedSheet, NSApp.keyWindow, window.attachedSheet.firstResponder, NSApp.windows] UTF8String]);
}

static void racomiTestPanelCancel(void *native) { [racomiTestPanel(native) cancel:nil]; }
*/
import "C"

import "unsafe"

// Call these functions on the application's main thread.
func State(native unsafe.Pointer) int { return int(C.racomiTestPanelState(native)) }

func Description(native unsafe.Pointer) string {
	value := C.racomiTestPanelDescription(native)
	defer C.free(unsafe.Pointer(value))
	return C.GoString(value)
}

func Cancel(native unsafe.Pointer) { C.racomiTestPanelCancel(native) }
