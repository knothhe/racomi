package desktop

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// All window and timer state is accessed on Wails' main thread. Closing an
// ordinary window quits; lightweight mode keeps the app reachable from its Dock.
func OpenMainWindow(s *Service, app *application.App) {
	var window *application.WebviewWindow
	var release *time.Timer
	var generation uint64
	width, height := 1200, 800
	var open func()
	open = func() {
		generation++
		if release != nil {
			release.Stop()
			release = nil
		}
		if window == nil {
			w := app.Window.NewWithOptions(application.WebviewWindowOptions{
				Name: "main", Title: "racomi", Width: width, Height: height,
				MinWidth: 900, MinHeight: 620,
				BackgroundColour: application.NewRGB(244, 244, 246),
				Mac:              application.MacWindow{TitleBar: application.MacTitleBarHiddenInset}, URL: "/",
			})
			window = w
			w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
				if window != w {
					return
				} // The release timer is closing this window for good.
				e.Cancel()
				if !s.desktop.lightweight.Load() {
					app.Quit()
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
						window = nil
						releaseWebView(w)
						w.Close()
					})
				})
			})
		}
		window.Show()
		window.Focus()
	}
	s.openWindow = func() { application.InvokeAsync(open) }
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) { s.OpenWindow() })
	open()
}

func (s *Service) OpenWindow() {
	if s.openWindow != nil {
		s.openWindow()
	}
}
