package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProjectIconPersistsWithoutChangingDeploymentState(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "alpha"))[0]
	preset := savePreset(t, store, "base", skill)
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(Target{Name: "project", Icon: "compass", Kind: "project", Path: root, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	target = state(t, reopened).Targets[0]
	if target.Icon != "compass" || target.Pending {
		t.Fatalf("project icon was not persisted with the applied state: %+v", target)
	}
	// Switching from a direct skill to an equivalent Preset changes selection
	// metadata, while preserving the icon and existing deployment ownership.
	if err = reopened.SetTargetSelection(target.ID, nil, []string{preset.ID}); err != nil {
		t.Fatal(err)
	}
	target = state(t, reopened).Targets[0]
	if target.Icon != "compass" || target.Pending || len(target.Deployments) != 1 {
		t.Fatalf("saving a selection changed project presentation or deployments: %+v", target)
	}
	target.Name, target.Icon = "renamed", "layers"
	target, err = reopened.SaveTarget(target)
	if err != nil || target.Icon != "layers" || target.Pending || len(target.Deployments) != 1 {
		t.Fatalf("changing project presentation required reapplying skills: %+v %v", target, err)
	}
	snapshot, err := reopened.Snapshot("project icon")
	if err != nil {
		t.Fatal(err)
	}
	var saved snapshotFile
	if err = readJSON(filepath.Join(store.Root(), "snapshots", snapshot.ID+".json"), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.State.Targets[0].Icon != "layers" {
		t.Fatalf("local snapshot lost project icon: %+v", saved.State.Targets[0])
	}
	target.Icon = "book"
	if _, err = reopened.SaveTarget(target); err != nil {
		t.Fatal(err)
	}
	// Restore keeps this machine's project metadata, as it does for paths.
	if err = reopened.RestoreSnapshot(snapshot.ID); err != nil {
		t.Fatal(err)
	}
	target = state(t, reopened).Targets[0]
	if target.Icon != "book" || len(target.EnabledSkillIDs)+len(target.EnabledPresetIDs) != 0 || len(target.Deployments) != 1 {
		t.Fatalf("restore changed the current machine's project icon or ownership: %+v", target)
	}
}

func TestEmptyProjectIconUsesLegacyDefaultAndCanResetCustomIcon(t *testing.T) {
	store, base := newStore(t)
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(Target{Name: "default", Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	assertDefault := func() {
		t.Helper()
		reopened, openErr := Open(store.Root())
		if openErr != nil {
			t.Fatal(openErr)
		}
		if got := state(t, reopened).Targets[0].Icon; got != "" {
			t.Fatalf("legacy/default project gained an icon: %q", got)
		}
		var persisted struct {
			Targets []map[string]json.RawMessage `json:"targets"`
		}
		contents, readErr := os.ReadFile(filepath.Join(store.Root(), "state.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if decodeErr := json.Unmarshal(contents, &persisted); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if _, exists := persisted.Targets[0]["icon"]; exists {
			t.Fatal("default icon should remain optional in persisted project data")
		}
	}
	assertDefault()
	target.Icon = "folder"
	if _, err = store.SaveTarget(target); err != nil {
		t.Fatal(err)
	}
	target.Icon = ""
	if _, err = store.SaveTarget(target); err != nil {
		t.Fatal(err)
	}
	assertDefault()
}

func TestInvalidProjectIconDoesNotChangeStateOrSnapshots(t *testing.T) {
	store, base := newStore(t)
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(Target{Name: "project", Icon: "compass", Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	snapshots, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	for _, icon := range []string{"<svg>", "a b", "../folder", "Folder", "folder\nbook", strings.Repeat("a", 41)} {
		t.Run(icon, func(t *testing.T) {
			target.Icon = icon
			if _, saveErr := store.SaveTarget(target); saveErr == nil {
				t.Fatalf("accepted invalid local icon key %q", icon)
			}
		})
	}
	afterSnapshots, err := store.Snapshots()
	if err != nil || len(afterSnapshots) != len(snapshots) || !reflect.DeepEqual(before, state(t, store)) {
		t.Fatalf("invalid project icon partially changed state or snapshots: %v", err)
	}
}
