package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type DesktopPreferences struct {
	Lightweight bool   `json:"lightweight"`
	OpenAtLogin bool   `json:"openAtLogin"`
	Supported   bool   `json:"supported"`
	Platform    string `json:"platform"`
}

// Lightweight mode and open at login are native integrations on these platforms.
func desktopIntegrationSupported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows"
}

type desktopRuntime struct{ lightweight atomic.Bool }

func loadDesktopPreferences(root string) (*desktopRuntime, error) {
	d := &desktopRuntime{}
	b, err := os.ReadFile(filepath.Join(root, "desktop.json"))
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	var saved struct {
		Lightweight bool `json:"lightweight"`
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, err
	}
	d.lightweight.Store(saved.Lightweight)
	return d, nil
}

func (s *Service) desktopPreferences() DesktopPreferences {
	return DesktopPreferences{Lightweight: s.desktop.lightweight.Load(), OpenAtLogin: openAtLoginEnabled(), Supported: desktopIntegrationSupported(), Platform: runtime.GOOS}
}

func (s *Service) SetLightweight(on bool) (AppState, error) {
	if !desktopIntegrationSupported() {
		return AppState{}, errors.New("This setting is available only on macOS, Linux and Windows")
	}
	s.mu.Lock()
	b, err := json.Marshal(struct {
		Lightweight bool `json:"lightweight"`
	}{on})
	if err == nil {
		path := filepath.Join(s.store.Root(), "desktop.json")
		err = os.WriteFile(path+".tmp", b, 0o600)
		if err == nil {
			err = os.Rename(path+".tmp", path)
		}
	}
	if err == nil {
		s.desktop.lightweight.Store(on)
	}
	s.mu.Unlock()
	if err == nil {
		s.syncTray()
	}
	if err != nil {
		return AppState{}, err
	}
	return s.GetState()
}

func (s *Service) SetOpenAtLogin(on bool) (AppState, error) {
	s.mu.Lock()
	err := setOpenAtLogin(on)
	s.mu.Unlock()
	if err != nil {
		return AppState{}, err
	}
	return s.GetState()
}

func (s *Service) SetTextSize(size int) error {
	if !slices.Contains([]int{100, 110, 125, 150}, size) {
		return errors.New("text size must be 100, 110, 125, or 150")
	}
	s.themeMu.Lock()
	app := s.app
	s.themeMu.Unlock()
	if app != nil {
		application.InvokeAsync(func() {
			for _, w := range app.Window.GetAll() {
				if webview, ok := w.(*application.WebviewWindow); ok {
					setPageZoom(webview, float64(size)/100)
				}
			}
		})
	}
	return nil
}
