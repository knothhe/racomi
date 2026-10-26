package desktop

import (
	"racomi/internal/buildinfo"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// All window and timer state is accessed on Wails' main thread. Closing an
// ordinary window quits; lightweight mode keeps the app reachable from its Dock
// on macOS and from its tray icon or launcher on Linux.
func OpenMainWindow(s *Service, app *application.App) {
	var window *application.WebviewWindow
	var release *time.Timer
	var generation uint64
	width, height := 1200, 800
	var open func()
	open = func() {
		s.cancelLifecycle("hide")
		generation++
		if release != nil {
			release.Stop()
			release = nil
		}
		if window == nil {
			s.pageOpened()
			w := app.Window.NewWithOptions(application.WebviewWindowOptions{
				Name: "main", Title: buildinfo.Current().Name, Width: width, Height: height,
				MinWidth: 900, MinHeight: 620,
				BackgroundColour: s.themeBackground(),
				Mac:              application.MacWindow{TitleBar: application.MacTitleBarHiddenInset}, URL: "/",
			})
			window = w
			w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
				// Wails invokes hooks on an event goroutine. Wait for the main
				// thread so cancellation and lifecycle changes finish together.
				application.InvokeSync(func() {
					if window != w {
						return
					} // The release timer is closing this window for good.
					e.Cancel()
					if !s.desktop.lightweight.Load() {
						app.Quit()
						return
					}
					closeGeneration := generation
					s.requestLifecycle("hide", func() {
						if window != w || generation != closeGeneration {
							return
						}
						w.Hide()
						generation++
						closedGeneration := generation
						if release != nil {
							release.Stop()
						}
						release = time.AfterFunc(time.Minute, func() {
							application.InvokeAsync(func() {
								if generation != closedGeneration || window != w || w.IsVisible() {
									return
								}
								if !s.desktop.lightweight.Load() {
									app.Quit()
									return
								}
								width, height = w.Size()
								s.pageReleased()
								window = nil
								releaseWebView(w)
								w.Close()
							})
						})
					})
				})
			})
		}
		window.Show()
		window.Focus()
	}
	s.openWindow = func() { application.InvokeAsync(open) }
	// Wails' default reopen listener calls Show from a separate goroutine.
	// Own this event so it cannot race with our main-thread window lifecycle.
	app.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(e *application.ApplicationEvent) {
		e.Cancel()
		application.InvokeSync(open)
	})
	open()
}

func (s *Service) OpenWindow() {
	if s.openWindow != nil {
		s.openWindow()
	}
}
