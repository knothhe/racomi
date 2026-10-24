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
	for key, value := range map[string]string{"textSize": "99", "interfaceFont": `"bad\nfont"`} {
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

func TestFontFamilyStyleAndSnapshotRetentionSettingsPatch(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	patch := map[string]json.RawMessage{"interfaceFont": json.RawMessage(`"Avenir Next"`), "interfaceFontStyle": json.RawMessage(`"AvenirNext-Medium"`), "snapshotRetentionDays": json.RawMessage(`14`), "snapshotMaxCount": json.RawMessage(`50`)}
	saved, err := s.SaveSettingsPatch(patch, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.State.Settings; got.InterfaceFont != "Avenir Next" || got.InterfaceFontStyle != "AvenirNext-Medium" || got.SnapshotRetentionDays != 14 || got.SnapshotMaxCount != 50 {
		t.Fatal("settings patch lost font or retention", got)
	}
	legacy := skills.Settings{DeploymentMode: "copy", DefaultProjectDirectories: []string{"agents"}, DefaultGlobalDirectories: []string{"codex-user"}}
	if saved, err = s.SaveSettings(legacy); err != nil || saved.State.Settings.InterfaceFontStyle != "AvenirNext-Medium" || saved.State.Settings.SnapshotRetentionDays != 14 || saved.State.Settings.SnapshotMaxCount != 50 {
		t.Fatal("legacy client erased new settings", err)
	}
	for _, invalid := range []map[string]json.RawMessage{
		{"snapshotRetentionDays": json.RawMessage(`0`)}, {"snapshotMaxCount": json.RawMessage(`0`)},
		{"snapshotRetentionDays": json.RawMessage(`-1`)}, {"snapshotRetentionDays": json.RawMessage(`3651`)},
		{"snapshotMaxCount": json.RawMessage(`-1`)}, {"snapshotMaxCount": json.RawMessage(`10001`)},
		{"interfaceFontStyle": json.RawMessage(`"bad\nstyle"`)},
	} {
		if _, err = s.SaveSettingsPatch(invalid, false); err == nil {
			t.Fatal("accepted invalid preference", invalid)
		}
	}
	if saved, err = s.SaveSettingsPatch(map[string]json.RawMessage{"interfaceFont": json.RawMessage(`"system"`), "interfaceFontStyle": json.RawMessage(`"system"`)}, false); err != nil || saved.State.Settings.InterfaceFontStyle != "system" {
		t.Fatal("could not reset font", err)
	}
}

func TestInstalledInterfaceFontsHaveUsableStyles(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS font catalog")
	}
	fonts, err := listInterfaceFonts()
	if err != nil || len(fonts) == 0 {
		t.Fatal("could not enumerate installed fonts", err)
	}
	seen := map[string]bool{}
	for _, font := range fonts {
		if font.Family == "" || seen[font.Family] || len(font.Styles) == 0 {
			t.Fatal("invalid font family", font)
		}
		seen[font.Family] = true
		for _, style := range font.Styles {
			if style.Name == "" || style.PostscriptName == "" {
				t.Fatal("invalid font style", style)
			}
		}
	}
}

func TestLightweightPreferenceIsLocalAndSurvivesRestart(t *testing.T) {
	if !desktopIntegrationSupported() {
		t.Skip("desktop integration unavailable")
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
