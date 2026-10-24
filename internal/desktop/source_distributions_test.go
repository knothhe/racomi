package desktop

import (
	"context"
	"slices"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestSourceSyncPathsChooseOneVariantAndRetainInstalledPhysicalPath(t *testing.T) {
	source := skillstore.Source{ID: "source", Mode: skillstore.SourceModeAll, Catalog: []skillstore.SourceSkill{
		{Name: "captions", Subpath: ".claude/skills/copy", ContentHash: "claude"},
		{Name: "captions", Subpath: ".agents/skills/captions", ContentHash: "agents"},
	}}
	paths, err := sourceSyncPaths(skillstore.State{}, source, nil)
	if err != nil || !slices.Equal(paths, []string{".agents/skills/captions"}) {
		t.Fatalf("all subscription did not choose one canonical variant: %v %v", paths, err)
	}
	installed := skillstore.Skill{ID: "captions", Name: "captions", SourceID: source.ID, Subpath: ".claude/skills/copy", ContentHash: "old claude"}
	state := skillstore.State{Skills: []skillstore.Skill{installed}}
	paths, err = sourceSyncPaths(state, source, nil)
	if err != nil || !slices.Equal(paths, []string{installed.Subpath}) {
		t.Fatalf("all subscription switched installed build: %v %v", paths, err)
	}
	paths, err = sourceSyncPaths(state, source, []string{".agents/skills/captions"})
	if err != nil || !slices.Equal(paths, []string{".agents/skills/captions"}) {
		t.Fatalf("explicit variant choice ignored: %v %v", paths, err)
	}
	if _, err = sourceSyncPaths(state, source, []string{source.Catalog[0].Subpath, source.Catalog[1].Subpath}); err == nil {
		t.Fatal("accepted two variants of one skill")
	}
	for _, status := range []string{"removed", "skipped"} {
		t.Run(status, func(t *testing.T) {
			copy := source
			copy.Catalog = append([]skillstore.SourceSkill{}, source.Catalog...)
			if status == "removed" {
				copy.Catalog[0].Removed = true
				state.Skills[0].UpstreamRemoved = true
			} else {
				copy.Catalog[0].SkipReason = "cannot inspect selected build"
			}
			paths, err := sourceSyncPaths(state, copy, nil)
			if err != nil || len(paths) != 0 {
				t.Fatalf("%s installed build silently replaced: %v %v", status, paths, err)
			}
			if _, err = sourceSyncPaths(state, copy, []string{installed.Subpath}); err == nil {
				t.Fatalf("explicit unavailable %s build accepted", status)
			}
			paths, err = sourceSyncPaths(state, copy, []string{".agents/skills/captions"})
			if err != nil || !slices.Equal(paths, []string{".agents/skills/captions"}) {
				t.Fatalf("cannot explicitly replace %s build: %v %v", status, paths, err)
			}
		})
	}
}

func TestSourceSyncPathsRefreshMetadataAndHistoricalDuplicates(t *testing.T) {
	source := skillstore.Source{ID: "source", Mode: skillstore.SourceModeAll, Catalog: []skillstore.SourceSkill{
		{Name: "new-name", Subpath: ".agents/skills/captions", ContentHash: "agents"},
		{Name: "new-name", Subpath: ".claude/skills/copy", ContentHash: "claude"},
	}}
	state := skillstore.State{Skills: []skillstore.Skill{{ID: "captions", Name: "old-name", SourceID: source.ID, Subpath: ".claude/skills/copy", ContentHash: "claude"}}}
	paths, err := sourceSyncPaths(state, source, nil)
	if err != nil || !slices.Equal(paths, []string{".claude/skills/copy"}) {
		t.Fatalf("stale installed name split one refreshed distribution family: %v %v", paths, err)
	}
	state.Skills[0].Name = "new-name"
	state.Skills = append(state.Skills, skillstore.Skill{ID: "historical-duplicate", Name: "new-name", SourceID: source.ID, Subpath: ".agents/skills/captions", ContentHash: "agents"})
	paths, err = sourceSyncPaths(state, source, []string{".agents/skills/captions"})
	if err != nil || !slices.Equal(paths, []string{".agents/skills/captions"}) {
		t.Fatalf("unchanged historical duplicates cannot reach consolidation: %v %v", paths, err)
	}

	source.Catalog = append(source.Catalog, skillstore.SourceSkill{Name: "new-name", Subpath: ".agents/skills/second", ContentHash: "second"})
	paths, err = sourceSyncPaths(skillstore.State{}, source, nil)
	if err != nil || len(paths) != 3 {
		t.Fatalf("same-tier ambiguous skills collapsed: %v %v", paths, err)
	}
}

func TestSourceDistributionImportAndUpdateRetainChosenVariantIdentity(t *testing.T) {
	f := newSourceFixture(t)
	f.write(".agents/skills/captions", "captions", "agents build")
	f.write(".claude/skills/copy", "captions", "claude build")
	f.commit("add distribution variants")
	ctx := context.Background()
	preview, err := f.service.Preview(ctx, f.repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err := f.service.Install(preview.ID, []string{".claude/skills/copy"})
	if err != nil {
		t.Fatal(err)
	}
	installed := sourceSkill(t, app, "captions")
	app, err = f.service.SavePreset(skillstore.Preset{Name: "keep references", SkillIDs: []string{installed.ID}})
	if err != nil {
		t.Fatal(err)
	}
	source := app.State.Sources[0]
	source.Mode = skillstore.SourceModeAll
	app, err = f.service.SaveSource(source)
	if err != nil {
		t.Fatal(err)
	}
	app, err = f.service.SyncSource(ctx, source.ID, source.Revision, nil)
	if err != nil || len(app.State.Skills) != 3 || sourceSkill(t, app, "captions").Subpath != ".claude/skills/copy" {
		t.Fatalf("all-source sync changed chosen build or skill count: %+v %v", app.State.Skills, err)
	}
	preview, err = f.service.Preview(ctx, f.repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err = f.service.Install(preview.ID, []string{".agents/skills/captions"})
	if err != nil {
		t.Fatal(err)
	}
	chosen := sourceSkill(t, app, "captions")
	if len(app.State.Skills) != 3 || chosen.ID != installed.ID || chosen.InstalledAt != installed.InstalledAt || chosen.Subpath != ".agents/skills/captions" || !slices.Equal(app.State.Presets[0].SkillIDs, []string{installed.ID}) || len(app.State.Sources[0].Catalog) != 4 {
		t.Fatalf("reimporting a variant lost identity/references/flat catalog: %+v", app.State)
	}
	f.write(".claude/skills/copy", "captions", "unselected claude build changed")
	f.commit("change unselected variant")
	updates, err := f.service.CheckUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, update := range updates {
		if update.SkillID == chosen.ID && (update.Available || update.Removed || update.Error != "") {
			t.Fatalf("update check followed an unselected variant: %+v", update)
		}
	}
	f.write(".agents/skills/captions", "captions", "selected agents build changed")
	f.commit("change selected variant")
	if _, err = f.service.CheckUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	app, err = f.service.UpdateSkills(ctx, []string{chosen.ID})
	if err != nil {
		t.Fatal(err)
	}
	document, err := f.service.ReadSkillDocument(chosen.ID)
	if err != nil || !strings.Contains(document, "selected agents build changed") || sourceSkill(t, app, "captions").Subpath != chosen.Subpath {
		t.Fatalf("update did not retain physical variant: %s %+v %v", document, app.State.Skills, err)
	}
}
