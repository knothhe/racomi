//go:build windows

// go run -tags production -ldflags="-s=false -w=false" ./internal/desktop/testdata/window-lifecycle-windows
// Uses a temporary profile and never opens the user's racomi instance.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"racomi/internal/desktop"
)

func waitFor(description string, timeout time.Duration, condition func() bool) error {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		if application.InvokeSyncWithResult(condition) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", description)
}

func check(app *application.App, service *desktop.Service) error {
	var first *application.WebviewWindow
	if err := waitFor("initial window", 15*time.Second, func() bool {
		windows := app.Window.GetAll()
		if len(windows) != 1 {
			return false
		}
		first, _ = windows[0].(*application.WebviewWindow)
		return first != nil && first.IsVisible()
	}); err != nil {
		return err
	}
	service.FrontendReady()
	for i := 0; i < 10; i++ {
		application.InvokeSync(first.Close)
		if err := waitFor("window hide", 5*time.Second, func() bool { return !first.IsVisible() }); err != nil {
			return err
		}
		service.OpenWindow()
		if err := waitFor("window reopen", 5*time.Second, func() bool { return len(app.Window.GetAll()) == 1 && first.IsVisible() }); err != nil {
			return err
		}
	}
	fmt.Println("PASS: Windows close/reopen cycles preserve one window")
	application.InvokeSync(first.Close)
	if err := waitFor("one-minute WebView release", 75*time.Second, func() bool { return len(app.Window.GetAll()) == 0 }); err != nil {
		return err
	}
	service.OpenWindow()
	if err := waitFor("reopen after release", 10*time.Second, func() bool {
		windows := app.Window.GetAll()
		return len(windows) == 1 && windows[0] != first && windows[0].IsVisible()
	}); err != nil {
		return err
	}
	fmt.Println("PASS: Windows stays running and reopens after the one-minute release")
	service.FrontendReady()
	_, err := service.SetLightweight(false)
	return err
}

func main() {
	root, err := os.MkdirTemp("", "racomi-windows-lifecycle-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root)
	watchdog := time.AfterFunc(2*time.Minute, func() { fmt.Fprintln(os.Stderr, "FAIL: native lifecycle timeout"); os.Exit(1) })
	defer watchdog.Stop()
	service, err := desktop.New(root)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = service.SetLightweight(true); err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name: "racomi Windows lifecycle verification", ShouldQuit: service.ShouldQuit,
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "<!doctype html><title>racomi isolated verification</title><p>Windows lifecycle regression</p>")
		})},
	})
	app.Event.On("racomi:lifecycle", func(event *application.CustomEvent) {
		if request, ok := event.Data.(desktop.LifecycleEvent); ok {
			_, _ = service.CompleteLifecycle(request.ID, request.Page, true)
		}
	})
	desktop.Attach(service, app)
	desktop.OpenMainWindow(service, app)
	var ready, finished atomic.Bool
	app.OnShutdown(func() {
		os.RemoveAll(root)
		if !ready.Load() {
			fmt.Fprintln(os.Stderr, "FAIL: premature shutdown")
			os.Exit(1)
		}
		finished.Store(true)
		fmt.Println("PASS: ordinary mode quits on Windows window close")
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			if err := check(app, service); err != nil {
				fmt.Fprintln(os.Stderr, "FAIL:", err)
				os.Exit(1)
			}
			ready.Store(true)
			application.InvokeSync(func() { app.Window.GetAll()[0].Close() })
		}()
	})
	if err = app.Run(); err != nil {
		log.Fatal(err)
	}
	if !finished.Load() {
		log.Fatal("FAIL: shutdown checks incomplete")
	}
}
