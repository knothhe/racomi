package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"racomi/internal/skills"
)

const darkOmarchy = "background = \"#112233\"\nforeground = '#eeeeee'\naccent = \"#88AAFF\" # comment\n"

func TestParseOmarchyTheme(t *testing.T) {
	for _, raw := range []string{"", "background = '#112233'", "background = '#gggggg'\nforeground = '#ffffff'\naccent = '#123456'", "[other]\n" + darkOmarchy} {
		if parseOmarchyTheme(raw) != nil {
			t.Fatalf("accepted invalid palette: %q", raw)
		}
	}
	for _, tc := range []struct {
		raw  string
		dark bool
	}{
		{darkOmarchy, true},
		{strings.ReplaceAll(darkOmarchy, "#112233", "#eeeeee"), false},
		{darkOmarchy + "mode = 'light' # explicit\n", false},
		{darkOmarchy + "theme_type = 'light'\n", false},
		{darkOmarchy + "mode = 'dark'\ntheme_type = 'light'\n", true},
	} {
		got := parseOmarchyTheme(tc.raw)
		if got == nil || got.Dark != tc.dark || got.Colors["accent"] != "#88aaff" {
			t.Fatalf("%q: %+v", tc.raw, got)
		}
	}
}

func isolatedOmarchy(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Omarchy is Linux only")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "omarchy", "current", "theme", "colors.toml"), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "omarchy", "current", "theme", "colors.toml")
}

func writeOmarchy(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestOmarchyPathsRefreshAndFallback(t *testing.T) {
	state, config := isolatedOmarchy(t)
	service := &Service{}
	if service.GetOmarchyTheme() != nil {
		t.Fatal("unexpected palette")
	}
	writeOmarchy(t, config, darkOmarchy)
	if got := service.GetOmarchyTheme(); got == nil || got.Colors["accent"] != "#88aaff" {
		t.Fatal("legacy palette not read", got)
	}
	writeOmarchy(t, state, strings.ReplaceAll(darkOmarchy, "#88AAFF", "#abcdef"))
	if got := service.GetOmarchyTheme(); got == nil || got.Colors["accent"] != "#abcdef" {
		t.Fatal("state palette not preferred", got)
	}
	writeOmarchy(t, state, "accent = 'invalid'")
	if service.GetOmarchyTheme() != nil {
		t.Fatal("invalid active theme must not use stale legacy colors")
	}
	writeOmarchy(t, state, strings.Repeat(" ", 65537))
	if service.GetOmarchyTheme() != nil {
		t.Fatal("oversized palette accepted")
	}
	if err := os.Remove(state); err != nil {
		t.Fatal(err)
	}
	if service.GetOmarchyTheme() == nil {
		t.Fatal("missing state should fall back to legacy path")
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if service.GetOmarchyTheme() != nil {
		t.Fatal("deleted palette retained")
	}
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	if service.GetOmarchyTheme() != nil {
		t.Fatal("directory treated as palette")
	}
}

func TestOmarchyThemeSymlinkSwitchAndDefaultStateHome(t *testing.T) {
	_, config := isolatedOmarchy(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	first, second := filepath.Join(t.TempDir(), "first"), filepath.Join(t.TempDir(), "second")
	writeOmarchy(t, filepath.Join(first, "colors.toml"), darkOmarchy)
	writeOmarchy(t, filepath.Join(second, "colors.toml"), strings.ReplaceAll(darkOmarchy, "#112233", "#eeeeee"))
	link := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	writeOmarchy(t, config, "invalid")
	if got := readOmarchyTheme(); got == nil || !got.Dark {
		t.Fatal("default state path not loaded", got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	if got := readOmarchyTheme(); got == nil || got.Dark {
		t.Fatal("symlink switch not detected", got)
	}
}

func TestOmarchyNativeBackgroundAndSavedStyle(t *testing.T) {
	state, _ := isolatedOmarchy(t)
	writeOmarchy(t, state, darkOmarchy)
	service := &Service{theme: "system"}
	if got := service.themeBackground(); got != application.NewRGB(17, 34, 51) {
		t.Fatal("automatic palette", got)
	}
	if err := service.SetTheme("light", "omarchy"); err != nil {
		t.Fatal(err)
	}
	if got := service.themeBackground(); got != application.NewRGB(244, 244, 246) {
		t.Fatal("explicit appearance", got)
	}
	if err := service.SetTheme("dark", "default"); err != nil {
		t.Fatal(err)
	}
	if got := service.themeBackground(); got != application.NewRGB(26, 26, 30) {
		t.Fatal("default style", got)
	}
	if err := service.SetTheme("dark", "unknown"); err == nil {
		t.Fatal("invalid style accepted")
	}
}

func TestThemeStylePersistenceMigrationAndLegacySave(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	// Persist the preference on every platform, so cloud sync preserves it even
	// where the Omarchy palette is unavailable.
	for _, style := range []string{"omarchy", "default"} {
		saved, err := s.SaveSettingsPatch(map[string]json.RawMessage{"themeStyle": json.RawMessage(`"` + style + `"`), "theme": json.RawMessage(`"dark"`)}, false)
		if err != nil || saved.State.Settings.ThemeStyle != style {
			t.Fatal("save", saved.State.Settings, err)
		}
		saved, err = s.SaveSettingsPatch(map[string]json.RawMessage{"themeStyle": json.RawMessage(`"omarchy"`)}, true)
		if err != nil || saved.State.Settings.ThemeStyle != style {
			t.Fatal("migration overwrote choice", err)
		}
		legacy := skills.Settings{DeploymentMode: "copy", DefaultProjectDirectories: []string{"agents"}, DefaultGlobalDirectories: []string{"codex-user"}}
		saved, err = s.SaveSettings(legacy)
		if err != nil || saved.State.Settings.ThemeStyle != style {
			t.Fatal("legacy save erased choice", err)
		}
		reopened, err := New(root)
		if err != nil || reopened.themeStyle != style || reopened.theme != "dark" {
			t.Fatal("startup did not restore appearance", err)
		}
	}
	if _, err := s.SaveSettingsPatch(map[string]json.RawMessage{"themeStyle": json.RawMessage(`"unknown"`)}, false); err == nil {
		t.Fatal("invalid style saved")
	}
	if runtime.GOOS != "linux" && s.GetOmarchyTheme() != nil {
		t.Fatal("palette exposed outside Linux")
	}
}
