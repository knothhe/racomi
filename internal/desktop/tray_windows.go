package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"racomi/internal/buildinfo"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Windows uses its notification area to reopen a released window. Lightweight mode
// keeps racomi running, a status icon reopens or quits it; launching racomi
// again also reopens the window through the single-instance handoff.
// Both variables are accessed only on Wails' main thread.
var (
	lightweightTray   *application.SystemTray
	lightweightLabels string
)

const trayIconSize = 64

func (s *Service) syncTray() {
	if s.app == nil || !s.lifecycle.started.Load() {
		return
	}
	on := s.desktop.lightweight.Load()
	openLabel := s.nativeText("Open racomi", "打开 racomi", "開啟 racomi")
	quitLabel := s.nativeText("Quit racomi", "退出 racomi", "結束 racomi")
	application.InvokeAsync(func() {
		labels := openLabel + "\x00" + quitLabel
		if on && lightweightTray != nil && lightweightLabels == labels {
			return
		}
		if lightweightTray != nil {
			lightweightTray.Destroy()
			lightweightTray = nil
		}
		if !on {
			return
		}
		menu := application.NewMenu()
		menu.Add(openLabel).OnClick(func(*application.Context) { s.OpenWindow() })
		menu.AddSeparator()
		menu.Add(quitLabel).OnClick(func(*application.Context) { s.app.Quit() })
		tray := s.app.SystemTray.New()
		if icon := trayIcon(s.icon); icon != nil {
			tray.SetIcon(icon)
		}
		tray.SetTooltip(buildinfo.Current().Name)
		tray.SetMenu(menu)
		tray.OnClick(s.OpenWindow)
		lightweightTray, lightweightLabels = tray, labels
	})
}

// Keep the notification icon small; Windows derives its HICON from this PNG.
func trayIcon(source []byte) []byte {
	if len(source) == 0 {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(source))
	if err != nil {
		return source
	}
	bounds := img.Bounds()
	if bounds.Dx() <= trayIconSize && bounds.Dy() <= trayIconSize {
		return source
	}
	out := image.NewNRGBA(image.Rect(0, 0, trayIconSize, trayIconSize))
	for y := 0; y < trayIconSize; y++ {
		y0, y1 := bounds.Min.Y+y*bounds.Dy()/trayIconSize, bounds.Min.Y+(y+1)*bounds.Dy()/trayIconSize
		for x := 0; x < trayIconSize; x++ {
			x0, x1 := bounds.Min.X+x*bounds.Dx()/trayIconSize, bounds.Min.X+(x+1)*bounds.Dx()/trayIconSize
			var r, g, b, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					pr, pg, pb, pa := img.At(sx, sy).RGBA()
					r, g, b, a, n = r+uint64(pr), g+uint64(pg), b+uint64(pb), a+uint64(pa), n+1
				}
			}
			if n == 0 || a == 0 {
				continue
			}
			// Average premultiplied channels, then unpremultiply once.
			out.SetNRGBA(x, y, color.NRGBA{uint8(r * 255 / a), uint8(g * 255 / a), uint8(b * 255 / a), uint8(a / n >> 8)})
		}
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, out); err != nil {
		return source
	}
	return encoded.Bytes()
}
