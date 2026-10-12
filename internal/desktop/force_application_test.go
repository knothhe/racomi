package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	skillstore "racomi/internal/skills"
)

func applicationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func applicationIndex(t *testing.T, service *Service) []byte {
	t.Helper()
	index, err := os.ReadFile(filepath.Join(service.store.Root(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func TestForceApplyProjectReplacesConflictWithoutWeakeningProtectedApply(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err := service.SaveProject(skillstore.Target{Name: "Project", Path: root, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	project := app.Projects[0]
	conflict := filepath.Join(project.Path, skill.Name)
	applicationFile(t, filepath.Join(conflict, "local.txt"), "local content")
	neighbor := filepath.Join(project.Path, "unrelated.txt")
	applicationFile(t, neighbor, "keep")
	before := applicationIndex(t, service)
	if _, err = service.ApplyProject(project.ID); err == nil {
		t.Fatal("protected project apply replaced an unowned directory")
	}
	if !bytes.Equal(before, applicationIndex(t, service)) {
		t.Fatal("rejected project application changed the saved configuration")
	}
	plan, err := service.PlanProject(project.ID)
	if err != nil || len(plan.Conflicts) == 0 || len(plan.BlockingConflicts) != 0 || !slices.Contains(plan.Overwrite, conflict) {
		t.Fatalf("project preview did not identify its replaceable path: %+v %v", plan, err)
	}
	applicationFile(t, filepath.Join(conflict, "changed-after-preview.txt"), "new external edit")
	if _, err = service.ForceApplyProject(project.ID, plan); err == nil {
		t.Fatal("force apply accepted an external directory change after its reviewed preview")
	}
	if !bytes.Equal(before, applicationIndex(t, service)) {
		t.Fatal("rejected stale project force apply changed state")
	}
	if content, err := os.ReadFile(filepath.Join(conflict, "changed-after-preview.txt")); err != nil || string(content) != "new external edit" {
		t.Fatalf("stale project force apply changed the external edit: %q %v", content, err)
	}
	plan, err = service.PlanProject(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	app, err = service.ForceApplyProject(project.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Projects[0].Deployments) != 1 || app.Projects[0].Pending {
		t.Fatalf("force apply did not register project deployment ownership: %+v", app.Projects[0])
	}
	if content, err := os.ReadFile(filepath.Join(conflict, "SKILL.md")); err != nil || string(content) != "alpha" {
		t.Fatalf("force apply did not install the selected project skill: %q %v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(conflict, "local.txt")); !os.IsNotExist(err) {
		t.Fatalf("force apply retained the replaced directory's old content: %v", err)
	}
	if content, err := os.ReadFile(neighbor); err != nil || string(content) != "keep" {
		t.Fatalf("force apply changed an unrelated project entry: %q %v", content, err)
	}
}

func TestForceApplyGlobalsKeepsBatchAtomicAndDeduplicatesSharedPaths(t *testing.T) {
	service, base, home := applicationService(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".agents"))
	skill := applicationSkill(t, service, base, "alpha")
	keys := []string{"codex-user", "codex-legacy", "claude-user", "codex-user"}
	app, err := service.SetGlobalSelections(keys, []string{skill.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := applicationByKey(t, app, "codex-user")
	last := applicationByKey(t, app, "claude-user")
	conflict := filepath.Join(last.Path, skill.Name)
	applicationFile(t, conflict, "unowned file")
	neighbor := filepath.Join(last.Path, "unrelated.txt")
	applicationFile(t, neighbor, "keep")
	before := applicationIndex(t, service)
	if _, err = service.ApplyGlobals(keys); err == nil {
		t.Fatal("protected batch apply overwrote an unowned file")
	}
	if !bytes.Equal(before, applicationIndex(t, service)) {
		t.Fatal("rejected global batch changed the saved configuration")
	}
	if _, err := os.Lstat(first.Path); !os.IsNotExist(err) {
		t.Fatalf("a later conflict allowed a partial application to an earlier directory: %v", err)
	}
	if content, err := os.ReadFile(conflict); err != nil || string(content) != "unowned file" {
		t.Fatalf("protected global batch changed the conflicting file: %q %v", content, err)
	}
	plan, err := service.PlanGlobals(keys)
	if err != nil || len(plan.Directories) != 2 || len(plan.BlockingConflicts) != 0 || !slices.Contains(plan.Overwrite, conflict) {
		t.Fatalf("batch preview lost path deduplication or overwrite details: %+v %v", plan, err)
	}
	applicationFile(t, conflict, "external edit after preview")
	if _, err = service.ForceApplyGlobals(keys, plan); err == nil {
		t.Fatal("force batch accepted an external file change after its reviewed preview")
	}
	if !bytes.Equal(before, applicationIndex(t, service)) {
		t.Fatal("rejected stale force batch changed state")
	}
	if _, err := os.Lstat(first.Path); !os.IsNotExist(err) {
		t.Fatalf("a stale force batch partially deployed an earlier directory: %v", err)
	}
	if content, err := os.ReadFile(conflict); err != nil || string(content) != "external edit after preview" {
		t.Fatalf("stale force batch changed the external edit: %q %v", content, err)
	}
	plan, err = service.PlanGlobals(keys)
	if err != nil {
		t.Fatal(err)
	}
	app, err = service.ForceApplyGlobals(keys, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.State.Targets) != 2 {
		t.Fatalf("forcing aliases registered duplicate targets: %+v", app.State.Targets)
	}
	for _, key := range keys {
		application := applicationByKey(t, app, key)
		if application.Target == nil || len(application.Target.Deployments) != 1 || application.Target.Pending {
			t.Fatalf("force apply lost ownership or duplicated a shared deployment: %+v", application)
		}
		if content, err := os.ReadFile(filepath.Join(application.Path, skill.Name, "SKILL.md")); err != nil || string(content) != "alpha" {
			t.Fatalf("force apply omitted a selected global directory: %q %v", content, err)
		}
	}
	if content, err := os.ReadFile(neighbor); err != nil || string(content) != "keep" {
		t.Fatalf("force apply changed an unrelated global entry: %q %v", content, err)
	}
}

func TestForceApplicationScopeErrorsDoNotChangeStateAndEmptySlotsRemainNoOps(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err := service.SaveProject(skillstore.Target{Name: "Project", Path: root, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	project := app.Projects[0]
	app, err = service.SetGlobalSelection("codex-user", []string{skill.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	global := applicationByKey(t, app, "codex-user")
	before := applicationIndex(t, service)
	for _, operation := range []func() (AppState, error){
		func() (AppState, error) { return service.ForceApplyProject(global.Target.ID, skillstore.ApplyPlan{}) },
		func() (AppState, error) { return service.ForceApplyProject("missing", skillstore.ApplyPlan{}) },
		func() (AppState, error) { return service.ForceApplyGlobal(project.ID, skillstore.ApplyPlan{}) },
		func() (AppState, error) { return service.ForceApplyGlobal("missing", skillstore.ApplyPlan{}) },
		func() (AppState, error) {
			return service.ForceApplyGlobals([]string{"codex-user", project.ID}, skillstore.ApplyPlan{})
		},
		func() (AppState, error) {
			return service.ForceApplyGlobals([]string{"codex-user", "missing"}, skillstore.ApplyPlan{})
		},
	} {
		if _, err := operation(); err == nil {
			t.Fatal("a force application accepted an unknown or wrong-scope identifier")
		}
		if !bytes.Equal(before, applicationIndex(t, service)) {
			t.Fatal("rejected force application changed state")
		}
	}
	if _, err := os.Lstat(global.Path); !os.IsNotExist(err) {
		t.Fatalf("invalid force batch partially deployed a valid global directory: %v", err)
	}
	plan, err := service.PlanGlobal("cursor-user")
	if err != nil || plan.Overwrite == nil || plan.BlockingConflicts == nil || len(plan.Overwrite)+len(plan.BlockingConflicts) != 0 {
		t.Fatalf("empty preview must expose non-null force metadata: %+v %v", plan, err)
	}
	emptyBatch, err := service.PlanGlobals(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() (AppState, error){
		func() (AppState, error) { return service.ForceApplyGlobal("cursor-user", plan) },
		func() (AppState, error) { return service.ForceApplyGlobals(nil, emptyBatch) },
	} {
		if _, err := operation(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, applicationIndex(t, service)) {
			t.Fatal("an empty force application changed state")
		}
	}
	// The single-directory endpoint follows the same protected/force boundary.
	applicationFile(t, filepath.Join(global.Path, skill.Name), "local")
	if _, err := service.ApplyGlobal("codex-user"); err == nil {
		t.Fatal("protected single-global apply overwrote an unowned file")
	}
	plan, err = service.PlanGlobal("codex-user")
	if err != nil {
		t.Fatal(err)
	}
	app, err = service.ForceApplyGlobal("codex-user", plan)
	if err != nil || applicationByKey(t, app, "codex-user").Target.Pending {
		t.Fatalf("single-global force apply failed: %+v %v", app, err)
	}
}
