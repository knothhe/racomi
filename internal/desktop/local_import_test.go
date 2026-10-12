package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestLocalImportInstallsReviewedSelectionWithoutGitSourceOrSourceEdits(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "local")
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: local fixture\n---\n"+body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha", "reviewed")
	write("beta", "not selected")
	service, err := New(filepath.Join(base, "store"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	preview, err := service.PreviewLocal(context.Background(), root)
	if err != nil || !preview.Local || len(preview.Candidates) != 2 {
		t.Fatalf("non-Git local skills were not discovered: %+v %v", preview, err)
	}
	write("alpha", "changed after review")
	app, err := service.Install(preview.ID, []string{"alpha"})
	if err != nil || len(app.State.Skills) != 1 || len(app.State.Sources) != 0 {
		t.Fatalf("local import installed extra skills or registered a Git source: %+v %v", app.State, err)
	}
	skill := app.State.Skills[0]
	if skill.Name != "alpha" || skill.Source != "" || skill.SourceID != "" || skill.Ref != "" || skill.Revision != "" {
		t.Fatalf("local import retained Git provenance: %+v", skill)
	}
	document, err := service.ReadSkillDocument(skill.ID)
	if err != nil || !strings.Contains(document, "reviewed") || strings.Contains(document, "changed") {
		t.Fatalf("local import did not use the reviewed immutable preview: %q %v", document, err)
	}
	body, err := os.ReadFile(filepath.Join(root, "alpha", "SKILL.md"))
	if err != nil || !strings.Contains(string(body), "changed after review") {
		t.Fatalf("install changed the selected local directory: %q %v", body, err)
	}
	if _, err = service.Install(preview.ID, []string{"alpha"}); err == nil {
		t.Fatal("completed local preview could be installed again")
	}
	if _, err = service.PreviewLocal(context.Background(), "relative/path"); err == nil {
		t.Fatal("desktop local preview accepted a relative path")
	}
}

func TestLocalImportsKeepDistinctOriginsAndExistingPresetContent(t *testing.T) {
	base := t.TempDir()
	firstRoot, secondRoot := filepath.Join(base, "first"), filepath.Join(base, "second")
	write := func(root, body string) {
		t.Helper()
		directory := filepath.Join(root, "alpha")
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: alpha\ndescription: local fixture\n---\n"+body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(firstRoot, "first origin")
	write(secondRoot, "second origin")
	service, err := New(filepath.Join(base, "store"))
	if err != nil {
		t.Fatal(err)
	}
	importLocal := func(root, subpath string) AppState {
		t.Helper()
		preview, err := service.PreviewLocal(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		app, err := service.Install(preview.ID, []string{subpath})
		if err != nil {
			t.Fatal(err)
		}
		return app
	}
	app := importLocal(filepath.Join(firstRoot, "alpha"), ".")
	firstID := app.State.Skills[0].ID
	if _, err = service.SavePreset(skillstore.Preset{Name: "Keep first origin", SkillIDs: []string{firstID}}); err != nil {
		t.Fatal(err)
	}
	app = importLocal(filepath.Join(secondRoot, "alpha"), ".")
	if len(app.State.Skills) != 2 || len(app.State.Sources) != 0 {
		t.Fatalf("distinct local origins were merged or registered as Git sources: %+v", app.State)
	}
	if len(app.State.Presets) != 1 || len(app.State.Presets[0].SkillIDs) != 1 || app.State.Presets[0].SkillIDs[0] != firstID {
		t.Fatalf("import changed the existing preset: %+v", app.State.Presets)
	}
	document, err := service.ReadSkillDocument(firstID)
	if err != nil || !strings.Contains(document, "first origin") || strings.Contains(document, "second origin") {
		t.Fatalf("another local origin replaced the referenced skill: %q %v", document, err)
	}
	secondID := ""
	for _, skill := range app.State.Skills {
		if skill.ID != firstID {
			secondID = skill.ID
		}
	}
	document, err = service.ReadSkillDocument(secondID)
	if err != nil || !strings.Contains(document, "second origin") {
		t.Fatalf("second origin was not independently installed: %q %v", document, err)
	}

	// The same origin keeps its identity when selected through its parent, even
	// after a restart. A fresh preview/cache ID must not create another skill.
	write(firstRoot, "updated first origin")
	service, err = New(filepath.Join(base, "store"))
	if err != nil {
		t.Fatal(err)
	}
	app = importLocal(firstRoot, "alpha")
	if len(app.State.Skills) != 2 {
		t.Fatalf("reimporting the same local origin duplicated it: %+v", app.State.Skills)
	}
	document, err = service.ReadSkillDocument(firstID)
	if err != nil || !strings.Contains(document, "updated first origin") {
		t.Fatalf("reimport lost the first origin's identity: %q %v", document, err)
	}
	document, err = service.ReadSkillDocument(secondID)
	if err != nil || !strings.Contains(document, "second origin") {
		t.Fatalf("updating one local origin replaced another: %q %v", document, err)
	}
}
