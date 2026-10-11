package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestProjectDirectoryTemplatesNormalizePersistAndClone(t *testing.T) {
	store, _ := newStore(t)
	settings := state(t, store).Settings
	settings.CustomProjectDirectories = []ProjectDirectory{
		{ID: "project-dir-relative", Name: "  团队技能  ", Path: "  ../shared//skills/.  "},
		{ID: "project-dir-local", Name: "Local", Path: "./custom//skills"},
	}
	settings.DefaultProjectDirectories = []string{"codex", "project-dir-relative"}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	expected := []ProjectDirectory{
		{ID: "project-dir-relative", Name: "团队技能", Path: filepath.Join("..", "shared", "skills")},
		{ID: "project-dir-local", Name: "Local", Path: filepath.Join("custom", "skills")},
	}
	if !reflect.DeepEqual(state(t, store).Settings.CustomProjectDirectories, expected) {
		t.Fatal("project templates were not normalized")
	}
	settings.CustomProjectDirectories[0].Path = "changed-input"
	settings.DefaultProjectDirectories[0] = "cursor"
	read := state(t, store)
	cloned := cloneState(read)
	cloned.Settings.CustomProjectDirectories[0].Name = "changed-clone"
	cloned.Settings.DefaultProjectDirectories[0] = "claude"
	if !reflect.DeepEqual(read.Settings.CustomProjectDirectories, expected) || read.Settings.DefaultProjectDirectories[0] != "codex" {
		t.Fatal("state cloning shared template or default directory arrays")
	}
	read.Settings.CustomProjectDirectories[0].Path = "changed-read"
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state(t, reopened).Settings.CustomProjectDirectories, expected) {
		t.Fatal("template persistence retained mutable caller/read arrays")
	}
}

func TestProjectDirectoryTemplatesValidateWithoutPartialChanges(t *testing.T) {
	store, _ := newStore(t)
	valid := ProjectDirectory{ID: "project-dir-team", Name: "Team", Path: "team/skills"}
	for _, test := range []struct {
		name        string
		directories []ProjectDirectory
	}{
		{"empty ID", []ProjectDirectory{{Name: "Team", Path: "team/skills"}}},
		{"invalid ID", []ProjectDirectory{{ID: "project dir", Name: "Team", Path: "team/skills"}}},
		{"built-in ID", []ProjectDirectory{{ID: "agents", Name: "Team", Path: "team/skills"}}},
		{"duplicate ID", []ProjectDirectory{valid, {ID: valid.ID, Name: "Other", Path: "other/skills"}}},
		{"empty name", []ProjectDirectory{{ID: valid.ID, Name: "  ", Path: valid.Path}}},
		{"long name", []ProjectDirectory{{ID: valid.ID, Name: strings.Repeat("团", 129), Path: valid.Path}}},
		{"empty path", []ProjectDirectory{{ID: valid.ID, Name: valid.Name, Path: "  "}}},
		{"long path", []ProjectDirectory{{ID: valid.ID, Name: valid.Name, Path: strings.Repeat("a", 4097)}}},
		{"null path", []ProjectDirectory{{ID: valid.ID, Name: valid.Name, Path: "team\x00/skills"}}},
		{"duplicate path", []ProjectDirectory{valid, {ID: "project-dir-other", Name: "Other", Path: "./team//skills/."}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := state(t, store)
			settings := before.Settings
			settings.CustomProjectDirectories = test.directories
			if err := store.SaveSettings(settings); err == nil {
				t.Fatal("invalid project template was accepted")
			}
			if !reflect.DeepEqual(before, state(t, store)) {
				t.Fatal("invalid project templates partially changed state")
			}
		})
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		settings := state(t, store).Settings
		settings.CustomProjectDirectories = []ProjectDirectory{valid, {ID: "project-dir-case", Name: "Case", Path: "TEAM/skills"}}
		if err := store.SaveSettings(settings); err == nil {
			t.Fatal("case-insensitive duplicate template paths were accepted")
		}
	}
	// Nested templates can be offered separately. Actual selection still rejects
	// overlapping deployment directories through the existing target protection.
	settings := state(t, store).Settings
	settings.CustomProjectDirectories = []ProjectDirectory{valid, {ID: "project-dir-child", Name: "Child", Path: "team/skills/child"}}
	settings.DefaultProjectDirectories = []string{valid.ID, "project-dir-child"}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal("settings rejected independently selectable nested templates:", err)
	}
	root := t.TempDir()
	if _, err := store.SaveTarget(Target{Name: "nested", Kind: "project", Path: root}); err == nil {
		t.Fatal("nested template selection bypassed target overlap protection")
	}
}

func TestProjectTemplateDefaultsExpandSnapshotsAndRespectExplicitSelection(t *testing.T) {
	store, base := newStore(t)
	settings := state(t, store).Settings
	settings.CustomProjectDirectories = []ProjectDirectory{{ID: "project-dir-team", Name: "Team", Path: "../shared/skills"}}
	settings.DefaultProjectDirectories = []string{"project-dir-team", "codex"}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(Target{Name: "defaults", Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(target.DirectoryIDs, []string{"codex"}) || !slices.Equal(target.CustomPaths, []string{"../shared/skills"}) || !slices.Equal(target.Paths, []string{filepath.Join(root, ".codex", "skills"), filepath.Join(base, "shared", "skills")}) {
		t.Fatalf("default template did not expand into a project path snapshot: %+v", target)
	}
	settings.DefaultProjectDirectories = []string{"project-dir-team"}
	settings.CustomProjectDirectories[0].Path = "only/skills"
	if err = store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	secondRoot := filepath.Join(base, "second")
	if err = os.Mkdir(secondRoot, 0755); err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveTarget(Target{Name: "custom default", Kind: "project", Path: secondRoot})
	if err != nil || len(second.DirectoryIDs) != 0 || !slices.Equal(second.CustomPaths, []string{"only/skills"}) || !slices.Equal(second.Paths, []string{filepath.Join(secondRoot, "only", "skills")}) {
		t.Fatalf("custom-only defaults were not usable: %+v %v", second, err)
	}
	updated, err := store.SaveTarget(Target{ID: target.ID, Name: "metadata update", Kind: "project", Path: root})
	if err != nil || !slices.Equal(updated.Paths, target.Paths) || !slices.Equal(updated.CustomPaths, target.CustomPaths) {
		t.Fatalf("omitted directory fields applied new defaults to an existing target: %+v %v", updated, err)
	}
	updated, err = store.SaveTarget(Target{ID: target.ID, Name: "custom-only selection", Kind: "project", Path: root, DirectoryIDs: []string{}})
	if err != nil || len(updated.DirectoryIDs) != 0 || !slices.Equal(updated.CustomPaths, target.CustomPaths) {
		t.Fatalf("explicit empty fixed selection did not preserve custom-only semantics: %+v %v", updated, err)
	}
	if _, err = store.SaveTarget(Target{Name: "empty selection", Kind: "project", Path: root, DirectoryIDs: []string{}, CustomPaths: []string{}}); err == nil {
		t.Fatal("explicit empty selection unexpectedly expanded defaults")
	}
}

func TestProjectTemplateEditsAndDeletionLeaveExistingDeploymentsUntouched(t *testing.T) {
	for _, mode := range []string{DeploymentCopy, DeploymentSymlink} {
		t.Run(mode, func(t *testing.T) {
			store, base := newStore(t)
			skill := install(t, store, candidate(t, base, "alpha", "original"))[0]
			settings := state(t, store).Settings
			settings.DeploymentMode = mode
			settings.CustomProjectDirectories = []ProjectDirectory{{ID: "project-dir-team", Name: "Team", Path: "team/skills"}}
			settings.DefaultProjectDirectories = []string{"project-dir-team"}
			if err := store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "repository")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			target, err := store.SaveTarget(Target{Name: "template snapshot", Kind: "project", Path: root, EnabledSkillIDs: []string{skill.ID}})
			if err != nil {
				t.Fatal(err)
			}
			mustApply(t, store, target)
			before := state(t, store)
			settings.CustomProjectDirectories[0].Path = "changed/skills"
			settings.CustomProjectDirectories[0].Name = "Changed"
			if err = store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.Targets, state(t, store).Targets) {
				t.Fatal("editing a template changed an existing project or its ownership")
			}
			settings.CustomProjectDirectories = []ProjectDirectory{}
			if err = store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			after := state(t, store)
			if len(after.Settings.CustomProjectDirectories) != 0 || !slices.Equal(after.Settings.DefaultProjectDirectories, []string{"agents"}) || !reflect.DeepEqual(before.Targets, after.Targets) {
				t.Fatalf("deleting a template did not preserve project snapshots and fall back defaults: %+v", after)
			}
			if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "original" {
				t.Fatal("settings edited a deployed skill")
			}
			plan, err := store.Plan(target.ID)
			if err != nil || len(plan.Add)+len(plan.Update)+len(plan.Remove)+len(plan.Conflicts) != 0 {
				t.Fatalf("template deletion changed a project's desired deployment: %+v %v", plan, err)
			}
		})
	}
}

func TestProjectTemplatesPreserveOmittedFieldsAndOnlyPruneDeletedDefaults(t *testing.T) {
	store, _ := newStore(t)
	settings := state(t, store).Settings
	settings.CustomProjectDirectories = []ProjectDirectory{{ID: "project-dir-team", Name: "Team", Path: "team/skills"}}
	settings.DefaultProjectDirectories = []string{"project-dir-team", "codex"}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings.CustomProjectDirectories = nil
	settings.DeploymentMode = DeploymentSymlink
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	if len(before.Settings.CustomProjectDirectories) != 1 {
		t.Fatal("legacy settings caller removed omitted templates")
	}
	invalid := before.Settings
	invalid.CustomProjectDirectories = []ProjectDirectory{}
	invalid.DefaultProjectDirectories = []string{"project-dir-team", "project-dir-unknown"}
	if err := store.SaveSettings(invalid); err == nil || !strings.Contains(err.Error(), "unknown project directory") {
		t.Fatalf("removing a known template silently accepted an unknown default: %v", err)
	}
	if !reflect.DeepEqual(before, state(t, store)) {
		t.Fatal("a rejected template deletion partially changed state")
	}
	settings = before.Settings
	settings.CustomProjectDirectories = []ProjectDirectory{}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(state(t, store).Settings.DefaultProjectDirectories, []string{"codex"}) {
		t.Fatal("deletion removed unrelated fixed defaults")
	}
	// States written before templates existed keep their other settings intact.
	index := filepath.Join(store.Root(), "state.json")
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	delete(document["settings"].(map[string]any), "customProjectDirectories")
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(index, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	legacy := state(t, reopened).Settings
	if legacy.CustomProjectDirectories == nil || len(legacy.CustomProjectDirectories) != 0 || legacy.DeploymentMode != DeploymentSymlink || !slices.Equal(legacy.DefaultProjectDirectories, []string{"codex"}) {
		t.Fatalf("legacy settings changed meaning: %+v", legacy)
	}
}

func TestProjectTemplatesRejectNewAndChangedAbsolutePaths(t *testing.T) {
	store, base := newStore(t)
	settings := state(t, store).Settings
	settings.CustomProjectDirectories = []ProjectDirectory{{ID: "project-dir-team", Name: "Team", Path: "team/skills"}}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(base, "absolute", "skills"), "/", "  /absolute/skills  ",
		`\rooted\skills`, `\\server\share\skills`, "//server/share/skills",
		"~", "~/skills", `~\skills`, "C:/skills", `c:\skills`, "Z:skills",
		"./~/skills", "team/../~/skills", "./C:/skills", "team/../C:skills",
	} {
		t.Run(path, func(t *testing.T) {
			for _, create := range []bool{true, false} {
				before := state(t, store)
				changed := cloneState(before).Settings
				if create {
					changed.CustomProjectDirectories = append(changed.CustomProjectDirectories, ProjectDirectory{ID: "project-dir-new", Name: "New", Path: path})
				} else {
					changed.CustomProjectDirectories[0].Path = path
				}
				if err := store.SaveSettings(changed); err == nil || err.Error() != "项目目录模板必须使用相对项目根目录的路径" {
					t.Fatalf("accepted absolute template path (create=%v): %q %v", create, path, err)
				}
				if !reflect.DeepEqual(before, state(t, store)) {
					t.Fatal("rejected template path partially changed settings")
				}
			}
		})
	}
}

func TestLegacyAbsoluteProjectTemplatesAndProjectSnapshotsRemainUsable(t *testing.T) {
	for _, mode := range []string{DeploymentCopy, DeploymentSymlink} {
		t.Run(mode, func(t *testing.T) {
			store, base := newStore(t)
			skill := install(t, store, candidate(t, base, "alpha", "original"))[0]
			absolute := filepath.Join(base, "legacy", "skills")
			legacy := state(t, store)
			legacy.Settings.DeploymentMode = mode
			legacy.Settings.CustomProjectDirectories = []ProjectDirectory{{ID: "project-dir-legacy", Name: "Legacy", Path: absolute}}
			legacy.Settings.DefaultProjectDirectories = []string{"project-dir-legacy"}
			// Simulate a state written before SaveSettings required relative templates.
			if err := store.writeState(legacy); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(store.Root())
			if err != nil {
				t.Fatal("legacy absolute templates became unreadable:", err)
			}
			root := filepath.Join(base, "repository")
			if err = os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			target, err := reopened.SaveTarget(Target{Name: "legacy default", Kind: "project", Path: root, EnabledSkillIDs: []string{skill.ID}})
			if err != nil || !slices.Equal(target.CustomPaths, []string{absolute}) {
				t.Fatalf("legacy default no longer expands as an absolute path snapshot: %+v %v", target, err)
			}
			mustApply(t, reopened, target)
			before := state(t, reopened)
			settings := cloneState(before).Settings
			settings.CustomProjectDirectories[0].Name = "Renamed legacy template"
			settings.CustomProjectDirectories[0].Path = "  " + absolute + "  "
			settings.DefaultGlobalDirectories = []string{"claude-user"}
			if err = reopened.SaveSettings(settings); err != nil {
				t.Fatal("unchanged legacy absolute path prevented metadata/preferences save:", err)
			}
			if !reflect.DeepEqual(before.Targets, state(t, reopened).Targets) {
				t.Fatal("saving a legacy template changed existing projects")
			}
			settings = state(t, reopened).Settings
			settings.CustomProjectDirectories = nil
			if err = reopened.SaveSettings(settings); err != nil {
				t.Fatal("an older caller omitting templates could not save settings:", err)
			}
			for _, path := range []string{filepath.Join(base, "new", "skills"), absolute + string(filepath.Separator) + "."} {
				changed := state(t, reopened).Settings
				changed.CustomProjectDirectories[0].Path = path
				if err = reopened.SaveSettings(changed); err == nil {
					t.Fatal("legacy template compatibility allowed a changed absolute path:", path)
				}
			}
			settings = state(t, reopened).Settings
			settings.CustomProjectDirectories[0].Path = "../relative/skills"
			if err = reopened.SaveSettings(settings); err != nil {
				t.Fatal("legacy absolute template could not change to a relative path:", err)
			}
			saved, err := reopened.SaveTarget(Target{ID: target.ID, Name: "existing absolute snapshot", Kind: "project", Path: root})
			if err != nil || !slices.Equal(saved.CustomPaths, []string{absolute}) || !reflect.DeepEqual(before.Targets[0].Deployments, saved.Deployments) {
				t.Fatalf("template constraint changed an existing absolute project snapshot: %+v %v", saved, err)
			}
			mustApply(t, reopened, saved)
			if contents(t, filepath.Join(absolute, "alpha", "SKILL.md")) != "original" {
				t.Fatal("template edits changed a deployed absolute project skill")
			}
			// The constraint only applies to settings templates. Explicit absolute
			// project paths still pass through the existing ownership/safety checks.
			explicit := filepath.Join(base, "explicit", "skills")
			other, err := reopened.SaveTarget(Target{Name: "absolute custom path", Kind: "project", Path: root, DirectoryIDs: []string{}, CustomPaths: []string{explicit}, EnabledSkillIDs: []string{skill.ID}})
			if err != nil {
				t.Fatal("absolute project customPaths were restricted:", err)
			}
			mustApply(t, reopened, other)
			if contents(t, filepath.Join(explicit, "alpha", "SKILL.md")) != "original" {
				t.Fatal("absolute project custom path no longer deploys")
			}
		})
	}
}

func TestLegacyNonRelativeTemplateFormatsRemainReadableAndMetadataEditable(t *testing.T) {
	for _, path := range []string{"~/skills", `~\skills`, "C:/skills", `C:\skills`, `\\server\share\skills`} {
		t.Run(path, func(t *testing.T) {
			store, base := newStore(t)
			t.Setenv("HOME", filepath.Join(base, "home"))
			t.Setenv("USERPROFILE", filepath.Join(base, "home"))
			legacy := state(t, store)
			legacy.Settings.CustomProjectDirectories = []ProjectDirectory{{ID: "project-dir-legacy", Name: "Legacy", Path: path}}
			if err := store.writeState(legacy); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(store.Root())
			if err != nil {
				t.Fatal(err)
			}
			settings := state(t, reopened).Settings
			settings.CustomProjectDirectories[0].Name = "Renamed"
			if err = reopened.SaveSettings(settings); err != nil {
				t.Fatalf("legacy template format %q prevented a metadata edit: %v", path, err)
			}
		})
	}
}
