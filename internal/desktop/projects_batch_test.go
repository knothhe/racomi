package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	skillstore "racomi/internal/skills"
)

func batchProject(t *testing.T, service *Service, base, name string, skill skillstore.Skill) skillstore.Target {
	t.Helper()
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err := service.SaveProject(skillstore.Target{Name: name, Path: root, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range app.Projects {
		if project.Name == name {
			return project
		}
	}
	t.Fatal("saved project missing")
	return skillstore.Target{}
}

func TestProjectBatchPreflightsEveryIDBeforeChangingAnyDestination(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	project := batchProject(t, service, base, "project", skill)
	app, err := service.SetGlobalSelections([]string{"codex-user"}, []string{skill.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	globalID := applicationByKey(t, app, "codex-user").Target.ID
	before := applicationIndex(t, service)
	for _, ids := range [][]string{nil, {}, {project.ID, "missing"}, {project.ID, globalID}} {
		if _, err := service.PlanProjects(ids); err == nil {
			t.Fatalf("preview accepted invalid project selection: %v", ids)
		}
		if _, err := service.ApplyProjects(ids); err == nil {
			t.Fatalf("apply accepted invalid project selection: %v", ids)
		}
		if _, err := service.ForceApplyProjects(ids, skillstore.ApplyPlan{}); err == nil {
			t.Fatalf("force apply accepted invalid project selection: %v", ids)
		}
		if !bytes.Equal(before, applicationIndex(t, service)) {
			t.Fatal("invalid project batch changed configuration")
		}
		if _, err := os.Lstat(project.Path); !os.IsNotExist(err) {
			t.Fatalf("invalid project batch touched a selected destination: %v", err)
		}
	}
}

func TestProjectBatchApplyAndReviewedForceAreAtomicAndDeduplicate(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	first := batchProject(t, service, base, "first", skill)
	second := batchProject(t, service, base, "second", skill)
	other := batchProject(t, service, base, "other", skill)
	ids := []string{first.ID, second.ID, first.ID}
	conflict := filepath.Join(second.Path, skill.Name, "local.txt")
	applicationFile(t, conflict, "local edit")
	plan, err := service.PlanProjects(ids)
	if err != nil || len(plan.Directories) != 2 || len(plan.Conflicts) == 0 || !slices.Contains(plan.Overwrite, filepath.Dir(conflict)) {
		t.Fatalf("unexpected project batch preview: %+v %v", plan, err)
	}
	before := applicationIndex(t, service)
	if _, err := service.ApplyProjects(ids); err == nil {
		t.Fatal("protected project batch replaced local files")
	}
	if _, err := os.Lstat(first.Path); !os.IsNotExist(err) {
		t.Fatalf("conflicted batch partially deployed the first project: %v", err)
	}
	applicationFile(t, conflict, "changed after review")
	if _, err := service.ForceApplyProjects(ids, plan); err == nil {
		t.Fatal("force project batch accepted a stale review")
	}
	if !bytes.Equal(before, applicationIndex(t, service)) {
		t.Fatal("rejected project batch changed configuration")
	}
	plan, err = service.PlanProjects(ids)
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.ForceApplyProjects(ids, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range app.Projects {
		if project.ID == other.ID {
			if !project.Pending || len(project.Deployments) != 0 {
				t.Fatal("batch applied an unselected project")
			}
			continue
		}
		if project.Pending || len(project.Deployments) != 1 || project.UpdatedAt == "" {
			t.Fatalf("batch did not deploy a selected project: %+v", project)
		}
		if content, err := os.ReadFile(filepath.Join(project.Path, skill.Name, "SKILL.md")); err != nil || string(content) != "alpha" {
			t.Fatalf("unexpected deployed project skill: %q %v", content, err)
		}
	}
	if _, err = service.ApplyProjects(ids); err != nil {
		t.Fatal("reapplying a successful project batch failed:", err)
	}
	app, err = service.ReorderProjects([]string{other.ID, second.ID, first.ID})
	if err != nil || app.Projects[0].ID != other.ID || app.Projects[2].ID != first.ID {
		t.Fatalf("service did not return reordered projects: %+v %v", app.Projects, err)
	}
}
