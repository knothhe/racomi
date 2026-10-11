package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestCustomGlobalDirectoryLifecyclePreservesSelectionsAndCleansDefaults(t *testing.T) {
	service, base, home := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	app, err := service.SavePreset(skillstore.Preset{Name: "custom", SkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	presetID := app.State.Presets[0].ID
	app, err = service.SaveGlobalDirectory(skillstore.Target{Name: "Custom agent", Path: "~/custom-agent/skills"})
	if err != nil || len(app.State.Targets) != 1 || len(app.GlobalApplications) != 5 {
		t.Fatalf("custom global directory was not registered: %+v %v", app, err)
	}
	target := app.State.Targets[0]
	if target.Kind != "global" || target.Path != filepath.Join(home, "custom-agent", "skills") || !slices.Equal(target.Paths, []string{target.Path}) {
		t.Fatalf("custom global path was not resolved: %+v", target)
	}
	custom := applicationByKey(t, app, target.ID)
	if custom.AgentID != "custom" || custom.Target == nil || custom.Key != target.ID {
		t.Fatalf("custom directory is not addressable by its target ID: %+v", custom)
	}
	if _, err = os.Lstat(target.Path); !os.IsNotExist(err) {
		t.Fatalf("registering a custom directory deployed files: %v", err)
	}
	settings := app.State.Settings
	settings.DefaultGlobalDirectories = []string{target.ID}
	app, err = service.SaveSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SetGlobalSelection(target.ID, []string{skill.ID}, []string{presetID}); err != nil {
		t.Fatal(err)
	}
	app, err = service.SaveGlobalDirectory(skillstore.Target{ID: target.ID, Name: "Renamed agent", Path: target.Path})
	if err != nil {
		t.Fatal(err)
	}
	renamed := applicationByKey(t, app, target.ID).Target
	if renamed.Name != "Renamed agent" || !slices.Equal(renamed.EnabledSkillIDs, []string{skill.ID}) || !slices.Equal(renamed.EnabledPresetIDs, []string{presetID}) {
		t.Fatalf("metadata edit lost direct or preset selections: %+v", renamed)
	}
	app, err = service.ApplyGlobals([]string{target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(target.Path, "alpha", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	// Only the path is protected while deployments exist; the name may change.
	app, err = service.SaveGlobalDirectory(skillstore.Target{ID: target.ID, Name: "Deployed agent", Path: target.Path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveGlobalDirectory(skillstore.Target{ID: target.ID, Name: target.Name, Path: filepath.Join(base, "moved", "skills")}); err == nil {
		t.Fatal("a deployed directory moved before its ownership was cleared")
	}
	if _, err = service.DeleteGlobalDirectory(target.ID); err == nil {
		t.Fatal("deleting a deployed directory lost ownership")
	}
	if _, err = service.SetGlobalSelection(target.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApplyGlobal(target.ID); err != nil {
		t.Fatal(err)
	}
	// A copied Target can contain a stale Paths field; Path still controls edits.
	cleared := applicationByKey(t, mustServiceState(t, service), target.ID).Target
	movedPath := filepath.Join(base, "moved", "skills")
	cleared.Path = movedPath
	app, err = service.SaveGlobalDirectory(*cleared)
	if err != nil || applicationByKey(t, app, target.ID).Path != movedPath {
		t.Fatalf("custom path edit ignored its new Path: %+v %v", app, err)
	}
	app, err = service.DeleteGlobalDirectory(target.ID)
	if err != nil || len(app.State.Targets) != 0 || !slices.Equal(app.State.Settings.DefaultGlobalDirectories, []string{"codex-user"}) {
		t.Fatalf("delete did not clear custom defaults atomically: %+v %v", app, err)
	}
	reopened, err := New(service.store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if restored := mustServiceState(t, reopened); !slices.Equal(restored.State.Settings.DefaultGlobalDirectories, []string{"codex-user"}) || len(restored.State.Targets) != 0 {
		t.Fatal("directory deletion and default fallback did not persist together")
	}
}

func mustServiceState(t *testing.T, service *Service) AppState {
	t.Helper()
	app, err := service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestCustomGlobalDirectoryDefaultRemovalKeepsOtherDefaults(t *testing.T) {
	service, base, _ := applicationService(t)
	app, err := service.SaveGlobalDirectory(skillstore.Target{Name: "custom", Path: filepath.Join(base, "custom", "skills")})
	if err != nil {
		t.Fatal(err)
	}
	target := app.State.Targets[0]
	settings := app.State.Settings
	settings.DefaultGlobalDirectories = []string{target.ID, "claude-user"}
	if _, err = service.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	app, err = service.DeleteGlobalDirectory(target.ID)
	if err != nil || !slices.Equal(app.State.Settings.DefaultGlobalDirectories, []string{"claude-user"}) {
		t.Fatalf("delete changed unrelated default directories: %+v %v", app, err)
	}
	settings.DefaultGlobalDirectories = []string{target.ID}
	if _, err = service.SaveSettings(settings); err == nil {
		t.Fatal("settings accepted a deleted custom global ID")
	}
}

func TestCustomGlobalDirectoryAPIsProtectBuiltinsAndProjectTargets(t *testing.T) {
	service, base, home := applicationService(t)
	builtin := filepath.Join(home, ".agents", "skills")
	for _, path := range []string{builtin, filepath.Dir(builtin), filepath.Join(builtin, "nested")} {
		if _, err := service.SaveGlobalDirectory(skillstore.Target{Name: "builtin overlap", Path: path}); err == nil {
			t.Fatalf("custom global directory overlapped a builtin: %s", path)
		}
	}
	if len(mustServiceState(t, service).State.Targets) != 0 {
		t.Fatal("rejected builtin paths partially created targets")
	}
	app, err := service.SetGlobalSelection("codex-user", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	builtinTarget := applicationByKey(t, app, "codex-user").Target
	if _, err = service.SaveGlobalDirectory(skillstore.Target{ID: builtinTarget.ID, Name: "Changed builtin", Path: filepath.Join(base, "other", "skills")}); err == nil {
		t.Fatal("the custom global API edited a builtin target")
	}
	if _, err = service.DeleteGlobalDirectory(builtinTarget.ID); err == nil {
		t.Fatal("the custom global API deleted a builtin target")
	}
	if _, err = service.DeleteGlobalDirectory("codex-user"); err == nil {
		t.Fatal("the custom global API accepted a builtin shortcut key")
	}
	root := filepath.Join(base, "repository")
	if err = os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err = service.SaveProject(skillstore.Target{Name: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	project := app.Projects[0]
	before := app.State
	for _, invalid := range []skillstore.Target{{ID: project.ID, Name: "wrong kind", Path: filepath.Join(base, "outside")}, {ID: "missing", Name: "missing", Path: filepath.Join(base, "outside")}, {Name: "wrong kind", Kind: "project", Path: filepath.Join(base, "outside")}, {Name: "missing path"}, {Name: "relative", Path: "relative/skills"}} {
		if _, err = service.SaveGlobalDirectory(invalid); err == nil {
			t.Fatalf("custom global API accepted an invalid target: %+v", invalid)
		}
	}
	if _, err = service.DeleteGlobalDirectory(project.ID); err == nil {
		t.Fatal("custom global delete accepted a project ID")
	}
	if !reflect.DeepEqual(before, mustServiceState(t, service).State) {
		t.Fatal("rejected custom global operations changed state")
	}
	settings := before.Settings
	settings.DefaultGlobalDirectories = []string{project.ID}
	if _, err = service.SaveSettings(settings); err == nil {
		t.Fatal("project IDs are not global directory defaults")
	}
}

func TestCustomProjectServiceCanApplyFixedAndExternalDirectoriesTogether(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err := service.SaveProject(skillstore.Target{Name: "custom project", Path: root, DirectoryIDs: []string{"claude"}, CustomPaths: []string{"../external/skills"}, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	project := app.Projects[0]
	plan, err := service.PlanProject(project.ID)
	if err != nil || len(plan.Directories) != 2 {
		t.Fatalf("project service ignored custom directories: %+v %v", plan, err)
	}
	app, err = service.ApplyProject(project.ID)
	if err != nil || app.Projects[0].Pending {
		t.Fatalf("mixed project directories did not apply together: %+v %v", app, err)
	}
	for _, path := range project.Paths {
		if _, err = os.Stat(filepath.Join(path, "alpha", "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}
}
