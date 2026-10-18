package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func selectionSource(t *testing.T, store *Store, repository string) Source {
	t.Helper()
	source, err := store.SaveSource(Source{Name: repository, Repository: repository, Ref: "main", Subpath: "."})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func linkedCandidate(t *testing.T, base string, source Source, name string) Candidate {
	t.Helper()
	c := candidate(t, base, name, name)
	c.Skill.Source, c.Skill.SourceID = source.Repository, source.ID
	return c
}

func TestSourceSelectionsUnionAndFollowInstalledSkills(t *testing.T) {
	for _, mode := range []string{DeploymentCopy, DeploymentSymlink} {
		t.Run(mode, func(t *testing.T) {
			store, base := newStore(t)
			settings := state(t, store).Settings
			settings.DeploymentMode = mode
			if err := store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			first := selectionSource(t, store, "https://example.test/first")
			second := selectionSource(t, store, "https://example.test/second")
			installed := install(t, store, linkedCandidate(t, base, first, "alpha"), linkedCandidate(t, base, first, "shared"), linkedCandidate(t, base, second, "beta"), candidate(t, base, "direct", "direct"))
			preset := savePreset(t, store, "base", installed[1], installed[2])
			target := saveTarget(t, store, base)
			if err := store.SetTargetSelectionWithSources(target.ID, []string{installed[1].ID, installed[3].ID}, []string{preset.ID}, []string{first.ID, second.ID, first.ID}); err != nil {
				t.Fatal(err)
			}
			selected := state(t, store).Targets[0]
			if !slices.Equal(selected.EnabledSourceIDs, []string{first.ID, second.ID}) {
				t.Fatalf("sources were not deduplicated: %+v", selected)
			}
			selected.EnabledSourceIDs[0] = "mutated"
			if state(t, store).Targets[0].EnabledSourceIDs[0] != first.ID {
				t.Fatal("source selections leaked a mutable slice")
			}
			missing(t, target.Path)
			plan, err := store.Plan(target.ID)
			if err != nil || !slices.Equal(plan.Add, []string{"alpha", "beta", "direct", "shared"}) {
				t.Fatalf("selection union: %+v %v", plan, err)
			}
			mustApply(t, store, target)
			if len(state(t, store).Targets[0].Deployments) != 4 {
				t.Fatal("overlapping selections deployed duplicates")
			}
			if err := store.RemoveSkill(installed[0].ID); err == nil {
				t.Fatal("removed a source-selected skill")
			}
			// A refreshed catalog alone must not include an uninstalled skill.
			future := linkedCandidate(t, base, first, "future")
			refreshedSource(t, store, first, "revision", linkedCandidate(t, base, first, "alpha"), linkedCandidate(t, base, first, "shared"), future)
			if state(t, store).Targets[0].Pending {
				t.Fatal("uninstalled catalog entries changed the target")
			}
			plan, err = store.Plan(target.ID)
			if err != nil || len(plan.Add)+len(plan.Update)+len(plan.Remove) != 0 {
				t.Fatalf("refresh deployed catalog skills: %+v %v", plan, err)
			}
			install(t, store, future)
			missing(t, filepath.Join(target.Path, "future"))
			if !state(t, store).Targets[0].Pending {
				t.Fatal("new installation did not mark source-selected target pending")
			}
			plan, err = store.Plan(target.ID)
			if err != nil || !slices.Equal(plan.Add, []string{"future"}) {
				t.Fatalf("new installation missing: %+v %v", plan, err)
			}
			mustApply(t, store, target)
			if err = store.SetTargetSelectionWithSources(target.ID, []string{installed[1].ID, installed[3].ID}, []string{preset.ID}, nil); err != nil {
				t.Fatal(err)
			}
			plan, err = store.Plan(target.ID)
			if err != nil || !slices.Equal(plan.Remove, []string{"alpha", "future"}) {
				t.Fatalf("clearing sources lost other selection owners: %+v %v", plan, err)
			}
			mustApply(t, store, target)
			if contents(t, filepath.Join(target.Path, "shared", "SKILL.md")) != "shared" {
				t.Fatal("shared skill removed")
			}
		})
	}
}

func TestSourceSelectionValidationAndLegacySaves(t *testing.T) {
	store, base := newStore(t)
	source := selectionSource(t, store, "https://example.test/empty")
	target := saveTarget(t, store, base)
	if err := store.SetTargetSelectionWithSources(target.ID, nil, nil, []string{source.ID}); err != nil {
		t.Fatal(err)
	}
	if state(t, store).Targets[0].Pending {
		t.Fatal("empty source has no deployment changes")
	}
	before := state(t, store)
	snapshots, _ := store.Snapshots()
	if err := store.SetTargetSelectionWithSources(target.ID, nil, nil, []string{"missing"}); err == nil {
		t.Fatal("missing source accepted")
	}
	after, _ := store.Snapshots()
	if !reflect.DeepEqual(before, state(t, store)) || len(after) != len(snapshots) {
		t.Fatal("invalid source selection changed state or snapshots")
	}
	if err := store.SetTargetSelection(target.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTargetPresets(target.ID, nil); err != nil {
		t.Fatal(err)
	}
	legacy := state(t, store).Targets[0]
	legacy.EnabledSourceIDs = nil
	legacy.Name = "renamed"
	saved, err := store.SaveTarget(legacy)
	if err != nil || !slices.Equal(saved.EnabledSourceIDs, []string{source.ID}) {
		t.Fatalf("legacy save erased sources: %+v %v", saved, err)
	}
	// Old state files need no schema migration for the additive field.
	path := filepath.Join(store.Root(), "state.json")
	var doc map[string]any
	if err = json.Unmarshal([]byte(contents(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc["targets"].([]any)[0].(map[string]any), "enabledSourceIds")
	data, _ := json.Marshal(doc)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if value := state(t, reopened).Targets[0].EnabledSourceIDs; value == nil || len(value) != 0 {
		t.Fatalf("old state not normalized: %v", value)
	}
}

func TestSourceDeletionAndRestoreKeepDeploymentOwnership(t *testing.T) {
	store, base := newStore(t)
	source := selectionSource(t, store, "https://example.test/repo")
	installed := install(t, store, linkedCandidate(t, base, source, "alpha"), linkedCandidate(t, base, source, "shared"))
	target := saveTarget(t, store, base)
	if err := store.SetTargetSelectionWithSources(target.ID, []string{installed[1].ID}, nil, []string{source.ID}); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	snapshot, err := store.Snapshot("source selection")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteSource(source.ID); err != nil {
		t.Fatal(err)
	}
	saved := state(t, store)
	if len(saved.Skills) != 2 || len(saved.Targets[0].EnabledSourceIDs) != 0 || len(saved.Targets[0].Deployments) != 2 || !saved.Targets[0].Pending {
		t.Fatalf("delete lost state: %+v", saved)
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "alpha" {
		t.Fatal("deletion changed deployment")
	}
	plan, err := store.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Remove, []string{"alpha"}) {
		t.Fatalf("delete lost direct selection: %+v %v", plan, err)
	}
	if err = store.RestoreSnapshot(snapshot.ID); err != nil {
		t.Fatal(err)
	}
	restored := state(t, store).Targets[0]
	if len(restored.EnabledSourceIDs)+len(restored.EnabledSkillIDs)+len(restored.EnabledPresetIDs) != 0 || len(restored.Deployments) != 2 || !restored.Pending {
		t.Fatalf("restore retained selections or lost ownership: %+v", restored)
	}
	plan, err = store.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Remove, []string{"alpha", "shared"}) {
		t.Fatalf("restored ownership: %+v %v", plan, err)
	}
}

func TestCloudSyncRemapsAndDropsLocalSourceSelections(t *testing.T) {
	store, base := newStore(t)
	source := selectionSource(t, store, "https://example.test/team")
	install(t, store, linkedCandidate(t, base, source, "alpha"))
	target := saveTarget(t, store, base)
	if err := store.SetTargetSelectionWithSources(target.ID, nil, nil, []string{source.ID}); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	shared := ExportShared(state(t, store))
	shared.Sources[0].ID = "source-aligned"
	shared.Skills[0].SourceID = "source-aligned"
	if err := store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	saved := state(t, store).Targets[0]
	if !slices.Equal(saved.EnabledSourceIDs, []string{"source-aligned"}) || saved.Pending {
		t.Fatalf("source identity remapping changed deployment: %+v", saved)
	}
	shared.Sources = nil
	shared.Skills[0].SourceID = ""
	if err := store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	saved = state(t, store).Targets[0]
	if len(saved.EnabledSourceIDs) != 0 || !saved.Pending || len(saved.Deployments) != 1 {
		t.Fatalf("cloud source deletion lost ownership or selection: %+v", saved)
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "alpha" {
		t.Fatal("cloud deletion changed deployed files")
	}
	plan, err := store.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Remove, []string{"alpha"}) {
		t.Fatalf("cloud deletion cannot be applied: %+v %v", plan, err)
	}
}
