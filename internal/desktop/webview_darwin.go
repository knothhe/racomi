package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

static WKWebView *racomiFindPage(NSView *view) {
 if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
 for (NSView *child in view.subviews) {
  WKWebView *page = racomiFindPage(child);
  if (page) return page;
 }
 return nil;
}
// Wails' macOS SetZoom uses pinch magnification, which leaves the layout at
// its old width. Page zoom reflows the interface at the selected text size.
static void racomiSetPageZoom(void *native, double zoom) {
 WKWebView *page = racomiFindPage(((NSWindow *)native).contentView);
 if (!page) return;
 page.magnification = 1;
 page.pageZoom = zoom;
}
// Wails beta.24 keeps WKWebView alive after Close. Detach its retained
// delegates and handlers before destroying a lightweight window.
static void racomiReleasePage(void *native) {
 NSWindow *window = (NSWindow *)native;
 WKWebView *page = racomiFindPage(window.contentView);
 if (!page) return;
 if ([window respondsToSelector:@selector(setWebView:)]) [window performSelector:@selector(setWebView:) withObject:nil];
 [page stopLoading];
 page.navigationDelegate = nil;
 page.UIDelegate = nil;
 [page.configuration.userContentController removeAllScriptMessageHandlers];
 [page removeFromSuperview];
}
*/
import "C"
import "github.com/wailsapp/wails/v3/pkg/application"

func releaseWebView(w *application.WebviewWindow) {
	if native := w.NativeWindow(); native != nil {
		C.racomiReleasePage(native)
	}
}

func setPageZoom(w *application.WebviewWindow, zoom float64) {
	if native := w.NativeWindow(); native != nil {
		C.racomiSetPageZoom(native, C.double(zoom))
	}
}
