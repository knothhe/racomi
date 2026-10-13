//go:build !darwin

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

func releaseWebView(*application.WebviewWindow) {}

func setPageZoom(w *application.WebviewWindow, zoom float64) { w.SetZoom(zoom) }
