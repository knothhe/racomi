package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"racomi/internal/skills"
)

func TestAppearanceSettingsPatchAndLegacySave(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.SaveSettingsPatch(map[string]json.RawMessage{"textSize": json.RawMessage(`125`), "interfaceFont": json.RawMessage(`"serif"`)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State.Settings.TextSize != 125 || saved.State.Settings.InterfaceFont != "serif" {
		t.Fatal(saved.State.Settings)
	}
	legacy := skills.Settings{DeploymentMode: "symlink", DefaultProjectDirectories: []string{"agents"}, DefaultGlobalDirectories: []string{"codex-user"}}
	saved, err = s.SaveSettings(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State.Settings.TextSize != 125 || saved.State.Settings.InterfaceFont != "serif" {
		t.Fatal("older clients erased appearance preferences")
	}
	for key, value := range map[string]string{"textSize": "99", "interfaceFont": `"unknown"`} {
		if _, err = s.SaveSettingsPatch(map[string]json.RawMessage{key: json.RawMessage(value)}, false); err == nil {
			t.Fatalf("accepted invalid %s", key)
		}
	}
	state, err := s.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if state.State.Settings.TextSize != 125 || state.State.Settings.InterfaceFont != "serif" {
		t.Fatal("invalid writes changed saved appearance")
	}
}

func TestLightweightPreferenceIsLocalAndSurvivesRestart(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS setting")
	}
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.SetLightweight(true)
	if err != nil {
		t.Fatal(err)
	}
	if !state.DesktopPreferences.Lightweight {
		t.Fatal("lightweight mode not enabled")
	}
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.desktopPreferences().Lightweight {
		t.Fatal("lightweight preference not persisted")
	}
	index, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err = json.Unmarshal(index, &saved); err != nil {
		t.Fatal(err)
	}
	if _, exists := saved["desktopPreferences"]; exists {
		t.Fatal("desktop preferences leaked into shared state")
	}
	if _, exists := saved["settings"].(map[string]any)["lightweight"]; exists {
		t.Fatal("lightweight preference leaked into shared settings")
	}
	if _, err = s.SetLightweight(false); err != nil {
		t.Fatal(err)
	}
	reopened, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.desktopPreferences().Lightweight {
		t.Fatal("could not disable lightweight mode")
	}
}
