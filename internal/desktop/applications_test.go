package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	skillstore "racomi/internal/skills"
)

func applicationService(t *testing.T) (*Service, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", filepath.Join(base, "codex-home"))
	service, err := New(filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return service, base, home
}

func applicationSkill(t *testing.T, service *Service, base, name string) skillstore.Skill {
	t.Helper()
	path := filepath.Join(base, "source-"+name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(name), 0644); err != nil {
		t.Fatal(err)
	}
	skills, err := service.store.Install([]skillstore.Candidate{{Path: path, Skill: skillstore.Skill{Name: name}}})
	if err != nil {
		t.Fatal(err)
	}
	return skills[0]
}

func applicationByKey(t *testing.T, app AppState, key string) GlobalApplication {
	t.Helper()
	for _, application := range app.GlobalApplications {
		if application.Key == key {
			return application
		}
	}
	t.Fatalf("missing global application %s", key)
	return GlobalApplication{}
}

func TestGlobalApplicationBrowseAndEmptyPreviewDoNotWrite(t *testing.T) {
	service, base, home := applicationService(t)
	index := filepath.Join(service.store.Root(), "state.json")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if len(app.GlobalApplications) != 4 || len(app.Projects) != 0 || len(app.State.Targets) != 0 || len(app.DirectoryOptions) != 3 {
		t.Fatalf("unexpected initial application scopes: %+v", app)
	}
	expected := map[string]string{
		"codex-user":   filepath.Join(home, ".agents", "skills"),
		"codex-legacy": filepath.Join(base, "codex-home", "skills"),
		"claude-user":  filepath.Join(home, ".claude", "skills"),
		"cursor-user":  filepath.Join(home, ".cursor", "skills"),
	}
	for _, application := range app.GlobalApplications {
		if application.Target != nil || application.Path != expected[application.Key] || application.AgentID == "" || application.AgentName == "" {
			t.Fatalf("incorrect empty Agent directory: %+v", application)
		}
		plan, err := service.PlanGlobal(application.Key)
		if err != nil || plan.TargetID != application.Key || len(plan.Add)+len(plan.Update)+len(plan.Remove)+len(plan.Conflicts) != 0 {
			t.Fatalf("empty directory preview changed desired state: %+v %v", plan, err)
		}
		app, err = service.ApplyGlobal(application.Key)
		if err != nil || len(app.State.Targets) != 0 || len(app.Snapshots) != 0 {
			t.Fatalf("empty application should be a no-op: %+v %v", app, err)
		}
	}
	for _, path := range []string{home, filepath.Join(base, "codex-home")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("browsing global applications created %s: %v", path, err)
		}
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("browsing or previewing wrote the index: %v", err)
	}
}

func TestGlobalAndProjectApplicationsRemainIndependent(t *testing.T) {
	service, base, _ := applicationService(t)
	alpha := applicationSkill(t, service, base, "alpha")
	beta := applicationSkill(t, service, base, "beta")
	app, err := service.SavePreset(skillstore.Preset{Name: "基础", SkillIDs: []string{alpha.ID}})
	if err != nil {
		t.Fatal(err)
	}
	presetID := app.State.Presets[0].ID
	app, err = service.SetGlobalSelection("codex-user", []string{alpha.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	global := applicationByKey(t, app, "codex-user")
	if global.Target == nil || len(global.Target.Deployments) != 0 {
		t.Fatalf("global selection was not saved without deployment: %+v", global)
	}
	if _, err = os.Stat(global.Path); !os.IsNotExist(err) {
		t.Fatalf("saving global selection created its directory: %v", err)
	}
	if _, err = service.ApplyGlobal("codex-user"); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "project")
	if err = os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveProject(skillstore.Target{Name: "missing"}); err == nil {
		t.Fatal("empty project root was accepted")
	}
	app, err = service.SaveProject(skillstore.Target{Name: "project", Kind: "global", Path: root})
	if err != nil || len(app.Projects) != 1 {
		t.Fatalf("project was not registered: %+v %v", app, err)
	}
	project := app.Projects[0]
	if project.Kind != "project" || project.Path != filepath.Join(root, ".agents", "skills") {
		t.Fatalf("project root was not resolved correctly: %+v", project)
	}
	if _, err = service.SetProjectSelection(project.ID, []string{alpha.ID, beta.ID}, []string{presetID}); err != nil {
		t.Fatal(err)
	}
	plan, err := service.PlanProject(project.ID)
	if err != nil || !slices.Equal(plan.Add, []string{"alpha", "beta"}) {
		t.Fatalf("project direct skills and Preset were not combined: %+v %v", plan, err)
	}
	if _, err = os.Stat(project.Path); !os.IsNotExist(err) {
		t.Fatalf("project selection or preview created the skills directory: %v", err)
	}
	if _, err = service.ApplyProject(project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SetGlobalSelection("codex-user", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApplyGlobal("codex-user"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(project.Path, "alpha", "SKILL.md")); err != nil {
		t.Fatalf("clearing global skills changed the project: %v", err)
	}
	if _, err = service.SetGlobalSelection("codex-user", []string{alpha.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApplyGlobal("codex-user"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SetProjectSelection(project.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApplyProject(project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(global.Path, "alpha", "SKILL.md")); err != nil {
		t.Fatalf("clearing project skills changed the global scope: %v", err)
	}
	if _, err = service.DeleteProject(global.Target.ID); err == nil {
		t.Fatal("project deletion accepted a global ID")
	}
	if _, err = service.SaveProject(skillstore.Target{ID: global.Target.ID, Name: "changed", Path: root}); err == nil {
		t.Fatal("project save accepted a global ID")
	}
	if _, err = service.SetProjectSelection(global.Target.ID, nil, nil); err == nil {
		t.Fatal("project selection accepted a global ID")
	}
	if _, err = service.PlanProject(global.Target.ID); err == nil {
		t.Fatal("project preview accepted a global ID")
	}
	if _, err = service.ApplyProject(global.Target.ID); err == nil {
		t.Fatal("project application accepted a global ID")
	}
	if _, err = service.SetGlobalSelection(project.ID, nil, nil); err == nil {
		t.Fatal("global selection accepted a project ID")
	}
	if _, err = service.PlanGlobal(project.ID); err == nil {
		t.Fatal("global preview accepted a project ID")
	}
	if _, err = service.ApplyGlobal(project.ID); err == nil {
		t.Fatal("global application accepted a project ID")
	}
}

func TestLegacyGlobalTargetsKeepIdentityAndOwnership(t *testing.T) {
	service, base, home := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	app, err := service.SavePreset(skillstore.Preset{Name: "legacy", SkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	presetID := app.State.Presets[0].ID
	app, err = service.SaveTarget(skillstore.Target{Name: "old user name", Kind: "global", Path: filepath.Join(home, ".agents", "skills"), EnabledPresetIDs: []string{presetID}})
	if err != nil {
		t.Fatal(err)
	}
	legacy := app.State.Targets[0]
	if _, err = service.ApplyTarget(legacy.ID); err != nil {
		t.Fatal(err)
	}
	app, err = service.SaveTarget(skillstore.Target{Name: "custom", Kind: "global", Path: filepath.Join(base, "custom-skills"), EnabledPresetIDs: []string{presetID}})
	if err != nil {
		t.Fatal(err)
	}
	custom := app.State.Targets[1]
	app, err = service.ApplyTarget(custom.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.GlobalApplications) != 5 {
		t.Fatalf("historical custom global directory disappeared: %+v", app.GlobalApplications)
	}
	for _, key := range []string{"codex-user", custom.ID} {
		application := applicationByKey(t, app, key)
		if application.Target == nil || len(application.Target.Deployments) != 1 {
			t.Fatalf("old deployment ownership was lost for %s: %+v", key, application)
		}
	}
	app, err = service.SetGlobalSelection("codex-user", []string{skill.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	global := applicationByKey(t, app, "codex-user")
	if len(app.State.Targets) != 2 || global.Target.ID != legacy.ID || global.Target.Name != legacy.Name || len(global.Target.Deployments) != 1 {
		t.Fatalf("new global flow replaced an existing record: %+v", app.State.Targets)
	}
	plan, err := service.PlanGlobal("codex-user")
	if err != nil || len(plan.Add)+len(plan.Update)+len(plan.Remove)+len(plan.Conflicts) != 0 {
		t.Fatalf("existing deployed skills were not recognized: %+v %v", plan, err)
	}
	if _, err = service.SetGlobalSelection(custom.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	plan, err = service.PlanGlobal(custom.ID)
	if err != nil || !slices.Equal(plan.Remove, []string{"alpha"}) {
		t.Fatalf("custom deployment lost removal ownership: %+v %v", plan, err)
	}
	if _, err = service.ApplyGlobal(custom.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(custom.Path, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("custom deployed skill was not removed: %v", err)
	}
	if _, err = os.Stat(filepath.Join(legacy.Path, "alpha", "SKILL.md")); err != nil {
		t.Fatalf("clearing the custom scope changed the fixed global scope: %v", err)
	}
}

func TestBatchGlobalSelectionAndApplicationAreAtomic(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	app, err := service.SavePreset(skillstore.Preset{Name: "batch", SkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	preset := app.State.Presets[0]
	keys := []string{"codex-user", "claude-user", "cursor-user"}
	// Validate all keys and references before registering even the first slot.
	for _, selection := range []struct{ keys, skills, presets []string }{
		{[]string{"codex-user", "unknown"}, []string{skill.ID}, nil},
		{keys, []string{skill.ID}, []string{"missing"}},
		{keys, []string{"missing"}, []string{preset.ID}},
	} {
		if _, err = service.SetGlobalSelections(selection.keys, selection.skills, selection.presets); err == nil {
			t.Fatal("invalid batch selection was accepted")
		}
		current, readErr := service.GetState()
		if readErr != nil || len(current.State.Targets) != 0 {
			t.Fatalf("invalid batch partially registered directories: %+v %v", current, readErr)
		}
	}
	app, err = service.SetGlobalSelections(keys, nil, []string{preset.ID})
	if err != nil || len(app.State.Targets) != 3 {
		t.Fatalf("batch registration failed: %+v %v", app, err)
	}
	for _, key := range keys {
		missingPath := applicationByKey(t, app, key).Path
		if _, err = os.Lstat(missingPath); !os.IsNotExist(err) {
			t.Fatalf("saving a batch deployed files: %v", err)
		}
	}
	plan, err := service.PlanGlobals(keys)
	if err != nil || len(plan.Directories) != 3 || !slices.Equal(plan.Add, []string{"alpha"}) {
		t.Fatalf("batch plan did not cover every directory: %+v %v", plan, err)
	}
	// A conflict in the last slot must prevent writing any selected slot.
	last := applicationByKey(t, app, keys[2])
	if err = os.MkdirAll(filepath.Join(last.Path, "alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(last.Path, "alpha", "keep"), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApplyGlobals(keys); err == nil {
		t.Fatal("global batch with a conflict was applied")
	}
	for _, key := range keys[:2] {
		if _, err = os.Lstat(applicationByKey(t, app, key).Path); !os.IsNotExist(err) {
			t.Fatal("another global directory changed despite the conflict")
		}
	}
	if err = os.RemoveAll(filepath.Join(last.Path, "alpha")); err != nil {
		t.Fatal(err)
	}
	app, err = service.ApplyGlobals(keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		application := applicationByKey(t, app, key)
		if application.Target == nil || application.Target.Pending || len(application.Target.Deployments) != 1 {
			t.Fatalf("batch did not commit ownership: %+v", application)
		}
		if data, err := os.ReadFile(filepath.Join(application.Path, "alpha", "SKILL.md")); err != nil || string(data) != "alpha" {
			t.Fatalf("batch did not deploy skill: %v", err)
		}
	}
	if _, err = service.SetGlobalSelections(keys, nil, nil); err != nil {
		t.Fatal(err)
	}
	app, err = service.ApplyGlobals(keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if _, err = os.Lstat(filepath.Join(applicationByKey(t, app, key).Path, "alpha")); !os.IsNotExist(err) {
			t.Fatalf("batch clear did not remove every deployment: %v", err)
		}
	}
}

func TestGlobalBatchDeduplicatesCodexHomeAlias(t *testing.T) {
	service, base, home := applicationService(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".agents"))
	skill := applicationSkill(t, service, base, "alpha")
	keys := []string{"codex-user", "codex-legacy", "codex-user"}
	app, err := service.SetGlobalSelections(keys, []string{skill.ID}, nil)
	if err != nil || len(app.State.Targets) != 1 {
		t.Fatalf("same directory shortcuts registered multiple targets: %+v %v", app, err)
	}
	modern, legacy := applicationByKey(t, app, "codex-user"), applicationByKey(t, app, "codex-legacy")
	if modern.Target == nil || legacy.Target == nil || modern.Target.ID != legacy.Target.ID {
		t.Fatalf("aliases lost shared ownership: %+v %+v", modern, legacy)
	}
	plan, err := service.PlanGlobals(keys)
	if err != nil || len(plan.Directories) != 1 {
		t.Fatalf("aliases planned the same directory more than once: %+v %v", plan, err)
	}
	app, err = service.ApplyGlobals(keys)
	if err != nil || len(app.State.Targets[0].Deployments) != 1 {
		t.Fatalf("aliases did not apply once: %+v %v", app, err)
	}
}
