package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPresetIconPersistsAndRestoresWithoutChangingDeployedSkills(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "alpha"))[0]
	preset, err := store.SavePreset(Preset{Name: "base", Icon: "  compass  ", SkillIDs: []string{skill.ID}})
	if err != nil || preset.Icon != "compass" {
		t.Fatalf("preset icon was not normalized and saved: %+v %v", preset, err)
	}
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	deployed := state(t, store).Targets[0]

	preset.Icon = "rocket"
	if _, err = store.SavePreset(preset); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	current := state(t, reopened)
	if current.Presets[0].Icon != "rocket" || !reflect.DeepEqual(current.Targets[0], deployed) {
		t.Fatalf("icon edit lost metadata or changed deployment state: %+v", current)
	}
	snapshot, err := reopened.Snapshot("preset icon")
	if err != nil {
		t.Fatal(err)
	}
	preset.Icon = "archive"
	if _, err = reopened.SavePreset(preset); err != nil {
		t.Fatal(err)
	}
	if err = reopened.RestoreSnapshot(snapshot.ID); err != nil {
		t.Fatal(err)
	}
	current = state(t, reopened)
	if current.Presets[0].Icon != "rocket" || len(current.Targets[0].EnabledPresetIDs) != 0 || !reflect.DeepEqual(current.Targets[0].Deployments, deployed.Deployments) {
		t.Fatalf("snapshot did not restore the portable preset icon while keeping deployments: %+v", current)
	}
	if got := contents(t, filepath.Join(deployed.Path, skill.Name, "SKILL.md")); got != "alpha" {
		t.Fatalf("preset presentation changed deployed content: %q", got)
	}
}

func TestMissingPresetIconKeepsLegacyDefaultAndSupportsReset(t *testing.T) {
	store, _ := newStore(t)
	preset, err := store.SavePreset(Preset{Name: "legacy", SkillIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	assertDefault := func() {
		t.Helper()
		reopened, openErr := Open(store.Root())
		if openErr != nil {
			t.Fatal(openErr)
		}
		if actual := state(t, reopened).Presets[0].Icon; actual != "" {
			t.Fatalf("missing icon should keep the default layers choice: %q", actual)
		}
		var persisted struct {
			Presets []map[string]json.RawMessage `json:"presets"`
		}
		if err := json.Unmarshal([]byte(contents(t, filepath.Join(store.Root(), "state.json"))), &persisted); err != nil {
			t.Fatal(err)
		}
		if _, exists := persisted.Presets[0]["icon"]; exists {
			t.Fatal("default icon should remain omitted, compatible with old preset data")
		}
	}
	assertDefault()
	for _, icon := range []string{"layers", "folder", "future-local-icon", ""} {
		preset.Icon = icon
		if _, err = store.SavePreset(preset); err != nil {
			t.Fatalf("safe local icon %q was rejected: %v", icon, err)
		}
	}
	assertDefault()
}

func TestInvalidPresetIconsDoNotChangeStateOrSnapshots(t *testing.T) {
	store, _ := newStore(t)
	preset, err := store.SavePreset(Preset{Name: "base", Icon: "compass"})
	if err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	snapshots, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	for _, icon := range []string{"<svg>", "a b", "../layers", "Layers", "layers\nbook", strings.Repeat("a", 41)} {
		t.Run(icon, func(t *testing.T) {
			invalid := preset
			invalid.Icon = icon
			if _, err := store.SavePreset(invalid); err == nil {
				t.Fatalf("accepted invalid local preset icon %q", icon)
			}
			// Duplicate import matching must validate presentation too, rather
			// than silently skipping a malformed preset with the same members.
			if _, err := store.MergeImport(nil, []Preset{invalid}, nil); err == nil {
				t.Fatalf("duplicate merge accepted invalid preset icon %q", icon)
			}
			imported := cloneState(before)
			imported.Presets[0].Icon = icon
			if err := store.Restore(imported); err == nil {
				t.Fatalf("restore accepted invalid preset icon %q", icon)
			}
		})
	}
	afterSnapshots, err := store.Snapshots()
	if err != nil || !reflect.DeepEqual(before, state(t, store)) || len(afterSnapshots) != len(snapshots) {
		t.Fatalf("invalid preset icon changed state or snapshots: %v", err)
	}
}

func TestMergeImportPreservesPresetIconsAndExistingPresentation(t *testing.T) {
	store, _ := newStore(t)
	custom := Preset{ID: "imported-custom", Name: "custom", Icon: " git-branch ", SkillIDs: []string{}}
	legacy := Preset{ID: "imported-legacy", Name: "legacy", SkillIDs: []string{}}
	result, err := store.MergeImport(nil, []Preset{custom, legacy}, nil)
	if err != nil || result.AddedPresets != 2 {
		t.Fatalf("could not import custom and legacy preset icons: %+v %v", result, err)
	}
	current := state(t, store)
	if current.Presets[0].Icon != "git-branch" || current.Presets[1].Icon != "" {
		t.Fatalf("import lost local icons or invented a legacy icon: %+v", current.Presets)
	}
	custom.Icon = "palette"
	result, err = store.MergeImport(nil, []Preset{custom}, nil)
	if err != nil || result.SkippedPresets != 1 || state(t, store).Presets[0].Icon != "git-branch" {
		t.Fatalf("duplicate import overwrote existing preset presentation: %+v %v", result, err)
	}
	// Invalid metadata on disk must also fail during a restart, not just saves.
	current.Presets[0].Icon = "<svg>"
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store.Root(), "state.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(store.Root()); err == nil {
		t.Fatal("restart accepted invalid persisted preset icon")
	}
}
