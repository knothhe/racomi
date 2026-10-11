package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestDirectSkillsAndPresetsShareDeploymentOwnership(t *testing.T) {
	store, base := newStore(t)
	skills := install(t, store, candidate(t, base, "alpha", "alpha"), candidate(t, base, "shared", "shared"), candidate(t, base, "direct", "direct"))
	preset := savePreset(t, store, "base", skills[0], skills[1])
	target := saveTarget(t, store, base)
	if err := store.SetTargetSelection(target.ID, []string{skills[1].ID, skills[2].ID, skills[2].ID}, []string{preset.ID}); err != nil {
		t.Fatal(err)
	}
	plan, err := store.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Add, []string{"alpha", "direct", "shared"}) {
		t.Fatalf("direct skills and Presets were not unioned: %+v %v", plan, err)
	}
	missing(t, target.Path)
	mustApply(t, store, target)
	if err = store.RemoveSkill(skills[2].ID); err == nil {
		t.Fatal("a directly selected skill must not be deleted from the store")
	}
	// The legacy Preset-only API must leave direct selections intact.
	if err = store.SetTargetPresets(target.ID, nil); err != nil {
		t.Fatal(err)
	}
	plan, err = store.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Remove, []string{"alpha"}) {
		t.Fatalf("clearing a Preset removed directly selected skills: %+v %v", plan, err)
	}
	mustApply(t, store, target)
	if contents(t, filepath.Join(target.Path, "shared", "SKILL.md")) != "shared" {
		t.Fatal("shared skill was removed while still selected directly")
	}
	before := state(t, store)
	snapshots, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetTargetSelection(target.ID, []string{skills[0].ID}, []string{"missing"}); err == nil {
		t.Fatal("invalid Preset selection was accepted")
	}
	if err = store.SetTargetSelection(target.ID, []string{"missing"}, []string{preset.ID}); err == nil {
		t.Fatal("invalid direct skill selection was accepted")
	}
	afterSnapshots, err := store.Snapshots()
	if err != nil || len(afterSnapshots) != len(snapshots) || !reflect.DeepEqual(before, state(t, store)) {
		t.Fatalf("invalid selection partially changed state or snapshots: %v", err)
	}
	// Updating metadata through an older caller must not erase the new field.
	legacy := before.Targets[0]
	legacy.EnabledSkillIDs = nil
	legacy.Name = "renamed"
	saved, err := store.SaveTarget(legacy)
	if err != nil || !slices.Equal(saved.EnabledSkillIDs, before.Targets[0].EnabledSkillIDs) {
		t.Fatalf("legacy metadata save erased direct selections: %+v %v", saved, err)
	}
	if err = store.SetTargetSelection(target.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.RemoveSkill(skills[2].ID); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	missing(t, filepath.Join(target.Path, "direct"))
}

func TestRestoreClearsDirectSelectionsAndPreservesDeployments(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "direct", "original"))[0]
	target, err := store.SaveTarget(Target{Name: "direct", Kind: "global", Path: filepath.Join(base, "target"), EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	snapshot, err := store.Snapshot("direct selection")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetTargetSelection(target.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.RemoveSkill(skill.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.RestoreSnapshot(snapshot.ID); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	restored := state(t, reopened)
	if len(restored.Skills) != 1 || len(restored.Targets) != 1 || len(restored.Targets[0].EnabledSkillIDs) != 0 || len(restored.Targets[0].EnabledPresetIDs) != 0 || len(restored.Targets[0].Deployments) != 1 || !restored.Targets[0].Pending {
		t.Fatalf("restore lost ownership or retained desired selections: %+v", restored)
	}
	if contents(t, filepath.Join(target.Path, "direct", "SKILL.md")) != "original" {
		t.Fatal("restore modified a deployed skill")
	}
	plan, err := reopened.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Remove, []string{"direct"}) {
		t.Fatalf("restored ownership cannot be safely cleared: %+v %v", plan, err)
	}
}

func TestExistingStateWithoutDirectSelectionsRemainsReadable(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "alpha"))[0]
	preset := savePreset(t, store, "base", skill)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	path := filepath.Join(store.Root(), "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, raw := range document["targets"].([]any) {
		delete(raw.(map[string]any), "enabledSkillIds")
	}
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	existing := state(t, reopened).Targets[0]
	if existing.ID != target.ID || existing.EnabledSkillIDs == nil || len(existing.EnabledSkillIDs) != 0 || !slices.Equal(existing.EnabledPresetIDs, []string{preset.ID}) || len(existing.Deployments) != 1 || existing.Pending {
		t.Fatalf("existing saved target was not preserved: %+v", existing)
	}
}
