package desktop

import (
	"os"
	"path/filepath"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestProjectIconFlowsThroughMetadataAndSelectionAPIs(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err := service.SaveProject(skillstore.Target{Name: "project", Icon: "compass", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	project := app.Projects[0]
	if project.Icon != "compass" || app.State.Targets[0].Icon != "compass" || project.Pending {
		t.Fatalf("project metadata did not reach application state: %+v", app)
	}
	app, err = service.SetProjectSelection(project.ID, []string{skill.ID}, nil)
	if err != nil || app.Projects[0].Icon != "compass" {
		t.Fatalf("saving skills erased the project icon: %+v %v", app.Projects, err)
	}
	app, err = service.ApplyProject(project.ID)
	if err != nil || app.Projects[0].Icon != "compass" || app.Projects[0].Pending {
		t.Fatalf("applying skills changed project presentation: %+v %v", app.Projects, err)
	}
	project = app.Projects[0]
	project.Name, project.Icon = "renamed", "layers"
	app, err = service.SaveProject(project)
	if err != nil || app.Projects[0].Icon != "layers" || app.Projects[0].Pending {
		t.Fatalf("changing project metadata required applying skills: %+v %v", app.Projects, err)
	}
	project = app.Projects[0]
	project.Icon = ""
	app, err = service.SaveProject(project)
	if err != nil || app.Projects[0].Icon != "" || app.Projects[0].Pending {
		t.Fatalf("project icon could not reset to the default: %+v %v", app.Projects, err)
	}
}
