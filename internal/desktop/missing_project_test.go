package desktop

import (
	"os"
	"path/filepath"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestMissingProjectStateRemainsReadableAndCanBeReassociated(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err := service.SaveProject(skillstore.Target{Name: "Project", Path: root, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	project := app.Projects[0]
	if _, err := service.ApplyProject(project.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	app, err = service.GetState()
	if err != nil || len(app.Projects) != 1 || app.ProjectDirectoryErrors[project.ID] == "" {
		t.Fatalf("missing project blocked the workspace or was not reported: %+v %v", app, err)
	}
	if _, err := service.PlanProject(project.ID); err == nil {
		t.Fatal("preview accepted a missing project directory")
	}
	if _, err := service.ApplyProject(project.ID); err == nil {
		t.Fatal("apply accepted a missing project directory")
	}
	newRoot := filepath.Join(base, "relocated")
	if err := os.Mkdir(newRoot, 0755); err != nil {
		t.Fatal(err)
	}
	// Use the frontend's save shape: Path carries the typed project root.
	app, err = service.SaveProject(skillstore.Target{ID: project.ID, Name: project.Name, Path: newRoot, DirectoryIDs: project.DirectoryIDs, EnabledSkillIDs: []string{skill.ID}})
	if err != nil || app.ProjectDirectoryErrors[project.ID] != "" || app.Projects[0].ProjectRoot != newRoot || !app.Projects[0].Pending {
		t.Fatalf("reassociation failed: %+v %v", app, err)
	}
	if _, err := os.Stat(filepath.Join(newRoot, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("save deployed skills: %v", err)
	}
}
