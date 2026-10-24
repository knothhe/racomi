package desktop

import (
	"errors"
	"github.com/wailsapp/wails/v3/pkg/application"
	"racomi/internal/buildinfo"
)

type UpdaterState struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	Automatic bool   `json:"automatic"`
	CanCheck  bool   `json:"canCheck"`
}

func updateEligibility(info buildinfo.Info) string {
	if info.Channel == "dev" {
		return "Development builds do not connect to the release update feed."
	}
	if info.Channel != "stable" {
		return "Preview builds are updated manually."
	}
	if !(info.OS == "darwin" && info.Arch == "arm64") && !(info.OS == "linux" && info.Arch == "amd64") && !(info.OS == "windows" && info.Arch == "amd64") {
		return "Application updates are currently available on macOS Apple Silicon, Linux x86_64 and Windows x64 only."
	}
	if info.Executor != "ci" || info.Dirty {
		return "This source build is updated using make install."
	}
	if buildinfo.FeedURL == "" || buildinfo.PublicKey == "" {
		return "The update feed is not configured."
	}
	return ""
}

func (s *Service) startUpdater() {
	if reason := updateEligibility(buildinfo.Current()); reason == "" {
		nativeStartUpdater(s)
	}
}

func (s *Service) GetUpdaterState() UpdaterState {
	if reason := updateEligibility(buildinfo.Current()); reason != "" {
		return UpdaterState{Reason: reason}
	}
	if s.app == nil {
		return UpdaterState{Reason: "The desktop updater is unavailable."}
	}
	return application.InvokeSyncWithResult(nativeUpdaterState)
}

func (s *Service) CheckApplicationUpdates() error {
	state := s.GetUpdaterState()
	if !state.Available {
		return errors.New(state.Reason)
	}
	if !state.CanCheck {
		return errors.New("An update operation is already in progress.")
	}
	application.InvokeAsync(nativeCheckUpdates)
	return nil
}

func (s *Service) SetAutomaticUpdates(on bool) (UpdaterState, error) {
	state := s.GetUpdaterState()
	if !state.Available {
		return state, errors.New(state.Reason)
	}
	application.InvokeSync(func() { nativeSetAutomaticUpdates(on) })
	return s.GetUpdaterState(), nil
}
