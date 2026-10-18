package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestSourceSelectionsInProjectsAndGlobalBatches(t *testing.T) {
	service, base, _ := applicationService(t)
	source, err := service.store.SaveSource(skillstore.Source{Name: "Team", Repository: "https://example.test/team", Ref: "main", Subpath: "."})
	if err != nil {
		t.Fatal(err)
	}
	skill := applicationSkill(t, service, base, "alpha")
	// Attach the local fixture to a source without needing a network repository.
	_, err = service.store.Install([]skillstore.Candidate{{Path: filepath.Join(base, "source-alpha"), Skill: skillstore.Skill{ID: skill.ID, Name: skill.Name, Source: source.Repository, SourceID: source.ID, Ref: "main", Subpath: "alpha"}}})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "project")
	if err = os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	app, err := service.SaveProject(skillstore.Target{Name: "Project", Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	project := app.Projects[0]
	app, err = service.SetProjectSelectionWithSources(project.ID, nil, nil, []string{source.ID})
	if err != nil || !slices.Equal(app.Projects[0].EnabledSourceIDs, []string{source.ID}) {
		t.Fatalf("project source selection: %+v %v", app.Projects, err)
	}
	plan, err := service.PlanProject(project.ID)
	if err != nil || !slices.Equal(plan.Add, []string{"alpha"}) {
		t.Fatalf("project plan: %+v %v", plan, err)
	}
	if _, err = service.ApplyProject(project.ID); err != nil {
		t.Fatal(err)
	}
	keys := []string{"codex-user", "claude-user"}
	app, err = service.SetGlobalSelectionsWithSources(keys, nil, nil, []string{source.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		target := applicationByKey(t, app, key).Target
		if target == nil || !slices.Equal(target.EnabledSourceIDs, []string{source.ID}) || !target.Pending {
			t.Fatalf("global source selection: %+v", target)
		}
	}
	before := app.State
	if _, err = service.SetGlobalSelectionsWithSources(keys, nil, nil, []string{"missing"}); err == nil {
		t.Fatal("batch accepted invalid source")
	}
	latest, err := service.GetState()
	if err != nil || !reflect.DeepEqual(before, latest.State) {
		t.Fatal("invalid batch partially saved", err)
	}
	if _, err = service.SetGlobalSelections(keys, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SetGlobalSelection(keys[0], nil, nil); err != nil {
		t.Fatal(err)
	}
	plan, err = service.PlanGlobals(keys)
	if err != nil || len(plan.Directories) != 2 || !slices.Equal(plan.Add, []string{"alpha"}) {
		t.Fatalf("legacy batch erased source selections: %+v %v", plan, err)
	}
	if _, err = service.ApplyGlobals(keys); err != nil {
		t.Fatal(err)
	}
	app, err = service.SetGlobalSelectionWithSources(keys[0], nil, nil, nil)
	if err != nil || len(applicationByKey(t, app, keys[0]).Target.EnabledSourceIDs) != 0 {
		t.Fatal("explicit empty source selection was not cleared", err)
	}
	plan, err = service.PlanGlobal(keys[0])
	if err != nil || !slices.Equal(plan.Remove, []string{"alpha"}) {
		t.Fatalf("global clear plan: %+v %v", plan, err)
	}
	if plan, err = service.PlanGlobal(keys[1]); err != nil || len(plan.Remove) != 0 {
		t.Fatalf("clearing one directory changed another: %+v %v", plan, err)
	}
}
