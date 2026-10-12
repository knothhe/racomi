package desktop

import (
	"os"
	"path/filepath"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestCodexDirectoryChoicesSaveExactTargetsWithoutDeploying(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	service, err := New(filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if len(app.State.Targets) != 0 || len(app.DirectoryOptions) != 3 {
		t.Fatalf("shortcuts must be available without registering targets: %+v", app)
	}
	expected := map[string]string{
		"codex-user":   filepath.Join(home, ".agents", "skills"),
		"codex-legacy": filepath.Join(home, ".codex", "skills"),
	}
	for _, option := range app.DirectoryOptions {
		if (option.ID == "codex-user" && option.Name != "共享用户目录") || (option.ID == "codex-project" && option.Name != "共享项目目录") {
			t.Fatalf("shared shortcut was mislabeled as a Codex-only directory: %+v", option)
		}
		path := option.Path
		if option.ID == "codex-project" {
			if option.Kind != "project" || option.Path != "" {
				t.Fatalf("project shortcut must require a chosen root: %+v", option)
			}
			path = filepath.Join(base, "repository")
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			expected[option.ID] = filepath.Join(path, ".agents", "skills")
		} else if option.Kind != "global" || option.Path != expected[option.ID] {
			t.Fatalf("incorrect Codex user shortcut: %+v", option)
		}
		app, err = service.SaveTarget(skillstore.Target{Name: option.Name, Kind: option.Kind, Path: path})
		if err != nil {
			t.Fatal(err)
		}
		if len(app.DirectoryOptions) != 3 {
			t.Fatal("mutation response lost directory shortcuts")
		}
		target := app.State.Targets[len(app.State.Targets)-1]
		if target.Path != expected[option.ID] {
			t.Fatalf("saving %s changed the intended directory to %s", option.ID, target.Path)
		}
		if _, err := os.Stat(target.Path); !os.IsNotExist(err) {
			t.Fatalf("saving a target must not create or deploy it: %v", err)
		}
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("enumerating or saving shortcuts changed the user home: %v", err)
	}
}

func TestCodexHomeChangesOnlyTheCompatibilityShortcut(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, override := range []string{filepath.Join(base, "custom codex"), "~/custom-codex"} {
		t.Run(override, func(t *testing.T) {
			t.Setenv("CODEX_HOME", override)
			options, err := codexDirectoryOptions()
			if err != nil {
				t.Fatal(err)
			}
			expectedLegacy := filepath.Join(base, "custom codex", "skills")
			if override == "~/custom-codex" {
				expectedLegacy = filepath.Join(home, "custom-codex", "skills")
			}
			for _, option := range options {
				switch option.ID {
				case "codex-user":
					if option.Path != filepath.Join(home, ".agents", "skills") {
						t.Fatalf("CODEX_HOME redirected the modern user directory: %s", option.Path)
					}
				case "codex-legacy":
					if option.Path != expectedLegacy {
						t.Fatalf("CODEX_HOME was ignored: %s", option.Path)
					}
				case "codex-project":
					if option.Path != "" || option.Kind != "project" {
						t.Fatalf("CODEX_HOME preselected a project: %+v", option)
					}
				}
			}
		})
	}
}
