package desktop

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestCheckedPresetRejectsStaleEditAndRemoteDeletion(t *testing.T) {
	service, base, _ := applicationService(t)
	skill := applicationSkill(t, service, base, "alpha")
	app, err := service.SavePresetChecked(skillstore.Preset{Name: "original", SkillIDs: []string{skill.ID}}, nil)
	if err != nil || len(app.State.Presets) != 1 {
		t.Fatalf("new preset failed: %+v, %v", app.State.Presets, err)
	}
	expected := app.State.Presets[0]
	draft := expected
	draft.Description = "unsaved local description"
	shared := skillstore.ExportShared(app.State)
	shared.Presets[0].Name = "remote name"
	if err = service.store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SavePresetChecked(draft, &expected); err == nil || !strings.Contains(err.Error(), "reopen the editor") {
		t.Fatalf("stale modal overwrote the remote preset: %v", err)
	}
	latest, err := service.GetState()
	if err != nil || latest.State.Presets[0].Name != "remote name" || latest.State.Presets[0].Description != "" {
		t.Fatalf("stale edit changed state: %+v, %v", latest.State.Presets, err)
	}
	shared.Presets = []skillstore.Preset{}
	if err = service.store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SavePresetChecked(draft, &expected); err == nil {
		t.Fatal("stale modal resurrected a remotely deleted preset")
	}
	latest, err = service.GetState()
	if err != nil || len(latest.State.Presets) != 0 {
		t.Fatal("deleted preset was recreated")
	}
}

func TestCheckedPresetAcceptsSameMembershipOrderAndRejectsMissingSkill(t *testing.T) {
	service, base, _ := applicationService(t)
	alpha := applicationSkill(t, service, base, "alpha")
	beta := applicationSkill(t, service, base, "beta")
	app, err := service.SavePresetChecked(skillstore.Preset{Name: "original", SkillIDs: []string{beta.ID, alpha.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := app.State.Presets[0]
	expected.SkillIDs = append([]string{}, expected.SkillIDs...)
	slices.Reverse(expected.SkillIDs)
	draft := expected
	draft.Name = "renamed"
	app, err = service.SavePresetChecked(draft, &expected)
	if err != nil || app.State.Presets[0].Name != "renamed" {
		t.Fatalf("membership order produced a false stale-edit error: %+v, %v", app.State.Presets, err)
	}
	// A new editor may still contain a skill deleted by background sync. Passing
	// the original membership must fail instead of quietly removing that choice.
	shared := skillstore.ExportShared(app.State)
	shared.Skills, shared.Presets = []skillstore.Skill{}, []skillstore.Preset{}
	if err = service.store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SavePresetChecked(skillstore.Preset{Name: "new stale selection", SkillIDs: []string{alpha.ID}}, nil); err == nil || !strings.Contains(err.Error(), "reopen the editor") {
		t.Fatalf("missing preset member was accepted: %v", err)
	}
}

func TestCheckedSourceRejectsStaleEditAndRemoteDeletion(t *testing.T) {
	service, _, _ := applicationService(t)
	app, err := service.SaveSourceChecked(skillstore.Source{Name: "original", Repository: "example/skills", Ref: "main", Subpath: ".", Mode: skillstore.SourceModeSelected}, nil)
	if err != nil || len(app.State.Sources) != 1 || app.State.Sources[0].Repository != "https://github.com/example/skills.git" {
		t.Fatalf("new source was not normalized: %+v, %v", app.State.Sources, err)
	}
	expected := app.State.Sources[0]
	draft := expected
	draft.Name = "unsaved local name"
	shared := skillstore.ExportShared(app.State)
	shared.Sources[0].Mode = skillstore.SourceModeAll
	if err = service.store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveSourceChecked(draft, &expected); err == nil || !strings.Contains(err.Error(), "reopen the editor") {
		t.Fatalf("stale modal overwrote the remote source mode: %v", err)
	}
	latest, err := service.GetState()
	if err != nil || latest.State.Sources[0].Mode != skillstore.SourceModeAll || latest.State.Sources[0].Name != "original" {
		t.Fatalf("stale source edit changed state: %+v, %v", latest.State.Sources, err)
	}
	shared.Sources = []skillstore.Source{}
	if err = service.store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveSourceChecked(draft, &expected); err == nil {
		t.Fatal("stale modal resurrected a remotely deleted source")
	}
	latest, err = service.GetState()
	if err != nil || len(latest.State.Sources) != 0 {
		t.Fatal("deleted source was recreated")
	}
}

func TestCheckedSourceAllowsCatalogRefreshAndPreservesLatestMetadata(t *testing.T) {
	service, _, _ := applicationService(t)
	app, err := service.SaveSourceChecked(skillstore.Source{Name: "original", Repository: "https://example.test/skills.git", Ref: "main", Subpath: ".", Mode: skillstore.SourceModeSelected}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := app.State.Sources[0]
	refreshed := expected
	refreshed.Revision, refreshed.RefreshedAt = "new-revision", "2026-10-12T01:00:00Z"
	refreshed.Warnings = []string{"local refresh warning"}
	refreshed.Catalog = []skillstore.SourceSkill{{Name: "alpha", Subpath: "alpha", Revision: refreshed.Revision, ContentHash: strings.Repeat("a", 64)}}
	if err = service.store.RecordSourceRefresh(refreshed); err != nil {
		t.Fatal(err)
	}
	if err = service.store.RecordSourceError(refreshed.ID, "local refresh error"); err != nil {
		t.Fatal(err)
	}
	draft := expected
	draft.Name = "local rename"
	app, err = service.SaveSourceChecked(draft, &expected)
	if err != nil || app.State.Sources[0].Name != "local rename" {
		t.Fatalf("catalog update incorrectly rejected editor: %+v, %v", app.State.Sources, err)
	}
	got := app.State.Sources[0]
	if got.Revision != refreshed.Revision || got.RefreshedAt != refreshed.RefreshedAt || got.LastError != "local refresh error" || !reflect.DeepEqual(got.Catalog, refreshed.Catalog) || !slices.Equal(got.Warnings, refreshed.Warnings) {
		t.Fatalf("saving editor overwrote latest refresh metadata: %+v", got)
	}
}

func TestCheckedEditsRequireMatchingBaselines(t *testing.T) {
	service, _, _ := applicationService(t)
	preset := skillstore.Preset{ID: "preset-explicit", Name: "preset"}
	source := skillstore.Source{ID: "source-explicit", Name: "source", Repository: "https://example.test/skills.git", Subpath: ".", Mode: skillstore.SourceModeSelected}
	if _, err := service.SavePresetChecked(preset, nil); err == nil {
		t.Fatal("explicit preset ID without a baseline accepted")
	}
	if _, err := service.SaveSourceChecked(source, nil); err == nil {
		t.Fatal("explicit source ID without a baseline accepted")
	}
	if _, err := service.SavePresetChecked(skillstore.Preset{Name: "new"}, &preset); err == nil {
		t.Fatal("new preset accepted an existing-record baseline")
	}
	if _, err := service.SaveSourceChecked(skillstore.Source{Name: "new", Repository: "example/skills"}, &source); err == nil {
		t.Fatal("new source accepted an existing-record baseline")
	}
}
