//go:build darwin && nativeintegration && !server

package desktop

import (
	"fmt"
	"os"
	"testing"
	"time"

	"racomi/internal/desktop/internal/dialogharness"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var dialogTestApp *application.App

// This opt-in suite starts an isolated application, never the user's racomi
// instance. On macOS run outside the sandbox: go test -tags nativeintegration
// ./internal/desktop -run TestNativeDirectoryDialog -count=1 -timeout=45s
func TestMain(m *testing.M) {
	dialogTestApp = application.New(application.Options{
		Name: "racomi native dialog tests",
		Mac:  application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
	})
	dialogTestApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "racomi native dialog tests", Width: 640, Height: 480,
		HTML: "<html><body>Native directory dialog regression test</body></html>",
	})
	dialogTestApp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
		// Wails Quit deliberately exits with code 0, which would hide a failed
		// suite. Exiting here preserves the testing package's result.
		os.Exit(m.Run())
	})
	if err := dialogTestApp.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(1)
}

func TestNativeDirectoryDialog(t *testing.T) {
	service := &Service{app: dialogTestApp}
	for _, scenario := range []string{"visible window", "hidden window", "recreated window"} {
		t.Run(scenario, func(t *testing.T) {
			var oldID uint
			if scenario == "recreated window" {
				application.InvokeSync(func() {
					old, _ := dialogTestApp.Window.GetByName("main")
					oldID = old.ID()
					old.Close()
					dialogTestApp.Window.NewWithOptions(application.WebviewWindowOptions{
						Name: "main", Title: "racomi recreated window", Width: 640, Height: 480,
						HTML: "<html><body>Recreated lightweight window</body></html>",
					})
				})
			}
			if scenario == "hidden window" {
				application.InvokeSync(func() {
					window, _ := dialogTestApp.Window.GetByName("main")
					window.Hide()
				})
			}
			owner := service.dialogWindow()
			deadline := time.Now().Add(5 * time.Second)
			for owner == nil || (scenario == "recreated window" && owner.ID() == oldID) {
				if time.Now().After(deadline) {
					t.Fatal("current main window is unavailable")
				}
				time.Sleep(20 * time.Millisecond)
				owner = service.dialogWindow()
			}
			result := make(chan struct {
				path string
				err  error
			}, 1)
			go func() {
				path, err := service.ChooseDirectory()
				result <- struct {
					path string
					err  error
				}{path, err}
			}()
			defer application.InvokeSync(func() { dialogharness.Cancel(owner.NativeWindow()) })
			deadline = time.Now().Add(5 * time.Second)
			for {
				state := application.InvokeSyncWithResult(func() int { return dialogharness.State(owner.NativeWindow()) })
				if state == 2 {
					break
				}
				if time.Now().After(deadline) {
					description := application.InvokeSyncWithResult(func() string { return dialogharness.Description(owner.NativeWindow()) })
					t.Fatalf("directory panel was not attached and foreground within 5 seconds (state %d): %s", state, description)
				}
				time.Sleep(20 * time.Millisecond)
			}
			// Work queued while the sheet is open must still reach AppKit.
			callback := make(chan struct{})
			application.InvokeAsync(func() { close(callback) })
			select {
			case <-callback:
			case <-time.After(time.Second):
				t.Fatal("main-thread callback stalled while the panel was open")
			}
			application.InvokeSync(func() { dialogharness.Cancel(owner.NativeWindow()) })
			select {
			case got := <-result:
				if got.err != nil || got.path != "" {
					t.Fatalf("cancelled directory result = %q, %v", got.path, got.err)
				}
			case <-time.After(5 * time.Second):
				description := application.InvokeSyncWithResult(func() string { return dialogharness.Description(owner.NativeWindow()) })
				t.Fatalf("directory selection did not return after dismissal: %s", description)
			}
		})
	}
}
