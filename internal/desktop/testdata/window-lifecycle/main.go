//go:build darwin

// Native regression check: GORACE=halt_on_error=1 go run -race -tags production ./internal/desktop/testdata/window-lifecycle
// Run in a macOS desktop session. Uses temporary data and takes about two minutes.
package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Exercise the same AppKit delegate callback as clicking the Dock icon.
static void reopenFromDock(void) {
 [NSApp.delegate applicationShouldHandleReopen:NSApp hasVisibleWindows:NO];
}

static BOOL windowIsShown(void *native) {
 return [(NSWindow *)native isVisible];
}
*/
import "C"

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"racomi/internal/desktop"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func waitFor(description string, timeout time.Duration, condition func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if application.InvokeSyncWithResult(condition) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", description)
}

// Called on the main thread. Wails' IsVisible tests occlusion, so a window
// covered by another app can look closed even though it is still ordered in.
func shown(window *application.WebviewWindow) bool {
	return bool(C.windowIsShown(window.NativeWindow()))
}

func check(app *application.App, service *desktop.Service) error {
	var first *application.WebviewWindow
	if err := waitFor("initial window", 10*time.Second, func() bool {
		windows := app.Window.GetAll()
		if len(windows) != 1 {
			return false
		}
		first, _ = windows[0].(*application.WebviewWindow)
		return first != nil && shown(first)
	}); err != nil {
		return err
	}
	service.FrontendReady() // This harness has no editable frontend.
	for i := 0; i < 100; i++ {
		first.Close()
		if err := waitFor("closed window to hide", 5*time.Second, func() bool { return !shown(first) }); err != nil {
			return err
		}
		service.OpenWindow()
		if err := waitFor("service reopen", 5*time.Second, func() bool {
			windows := app.Window.GetAll()
			return len(windows) == 1 && windows[0] == first && shown(first)
		}); err != nil {
			return fmt.Errorf("cycle %d: %w", i+1, err)
		}
	}
	fmt.Println("PASS: 100 close/immediate service reopen cycles reused the window")
	// Let every canceled release deadline pass while this window stays open.
	time.Sleep(65 * time.Second)
	if !application.InvokeSyncWithResult(func() bool {
		windows := app.Window.GetAll()
		return len(windows) == 1 && windows[0] == first && shown(first)
	}) {
		return fmt.Errorf("a canceled release removed or hid the reopened window")
	}
	fmt.Println("PASS: reopened window survived all canceled release deadlines")
	first.Close()
	if err := waitFor("lightweight window release", 70*time.Second, func() bool { return len(app.Window.GetAll()) == 0 }); err != nil {
		return err
	}
	application.InvokeSync(func() { C.reopenFromDock() })
	var reopened *application.WebviewWindow
	if err := waitFor("Dock reopen after release", 5*time.Second, func() bool {
		windows := app.Window.GetAll()
		if len(windows) != 1 {
			return false
		}
		reopened, _ = windows[0].(*application.WebviewWindow)
		return reopened != nil && reopened != first && shown(reopened)
	}); err != nil {
		return err
	}
	fmt.Println("PASS: Dock reopen created a new window after the one-minute release")
	service.FrontendReady()
	if _, err := service.SetLightweight(false); err != nil {
		return err
	}
	return nil
}

func main() {
	root, err := os.MkdirTemp("", "racomi-window-lifecycle-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root)
	watchdog := time.AfterFunc(3*time.Minute, func() {
		os.RemoveAll(root)
		fmt.Fprintln(os.Stderr, "FAIL: native lifecycle check exceeded three minutes")
		os.Exit(1)
	})
	defer watchdog.Stop()
	service, err := desktop.New(root)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = service.SetLightweight(true); err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name: "racomi window lifecycle check",
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<!doctype html><title>Native lifecycle check</title><p>Isolated window regression</p>")
		})},
		ShouldQuit: service.ShouldQuit,
		Mac:        application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
	})
	app.Event.On("racomi:lifecycle", func(event *application.CustomEvent) {
		request, ok := event.Data.(desktop.LifecycleEvent)
		if ok {
			_, _ = service.CompleteLifecycle(request.ID, request.Page, true)
		}
	})
	desktop.Attach(service, app)
	desktop.OpenMainWindow(service, app)
	var readyToQuit, shutdownVerified atomic.Bool
	app.OnShutdown(func() {
		os.RemoveAll(root)
		// AppKit's terminate exits the process without returning from Run.
		if !readyToQuit.Load() {
			fmt.Fprintln(os.Stderr, "FAIL: application exited before the lifecycle checks completed")
			os.Exit(1)
		}
		shutdownVerified.Store(true)
		fmt.Println("PASS: ordinary mode exited on window close")
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			if err := check(app, service); err != nil {
				os.RemoveAll(root)
				fmt.Fprintln(os.Stderr, "FAIL:", err)
				os.Exit(1)
			}
			// Ordinary mode must actually quit when the window is closed.
			readyToQuit.Store(true)
			application.InvokeSync(func() { app.Window.GetAll()[0].Close() })
		}()
	})
	if err = app.Run(); err != nil {
		log.Fatal(err)
	}
	if !shutdownVerified.Load() {
		log.Fatal("FAIL: application returned without completing shutdown checks")
	}
}
