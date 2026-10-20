package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func orderProject(t *testing.T, store *Store, base, name string) Target {
	t.Helper()
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(Target{Name: name, Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestProjectOrderIsLocalPersistentAndPreservesGlobalDestinations(t *testing.T) {
	store, base := newStore(t)
	first := orderProject(t, store, base, "first")
	global := saveTarget(t, store, base)
	second := orderProject(t, store, base, "second")
	before := state(t, store)
	for _, ids := range [][]string{{first.ID}, {first.ID, first.ID}, {first.ID, global.ID}, {first.ID, "missing"}, {first.ID, second.ID, first.ID}} {
		if err := store.ReorderProjects(ids); err == nil {
			t.Fatalf("accepted incomplete or invalid project order: %v", ids)
		}
		if !reflect.DeepEqual(before, state(t, store)) {
			t.Fatal("invalid project order changed state")
		}
	}
	if err := store.ReorderProjects([]string{second.ID, first.ID}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	after := state(t, reopened)
	if !reflect.DeepEqual(after.Targets, []Target{before.Targets[2], before.Targets[1], before.Targets[0]}) {
		t.Fatal("reorder changed target metadata or did not retain the global slot")
	}
	shared := ExportShared(after)
	shared.Settings.TextSize = 125
	if err := reopened.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Targets, state(t, reopened).Targets) {
		t.Fatal("cloud library synchronization changed local project order or metadata")
	}
}

func TestTargetDatesAreServerOwnedAndTrackConfigurationOnly(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "content"))[0]
	target := orderProject(t, store, base, "project")
	if _, err := time.Parse(time.RFC3339Nano, target.CreatedAt); err != nil || target.CreatedAt != target.UpdatedAt {
		t.Fatalf("new project dates are missing or invalid: %+v", target)
	}
	created := target.CreatedAt
	target.CreatedAt, target.UpdatedAt = "client created", "client updated"
	unchanged, err := store.SaveTarget(target)
	if err != nil || unchanged.CreatedAt != created || unchanged.UpdatedAt != created {
		t.Fatalf("client dates replaced server dates or unchanged save refreshed them: %+v %v", unchanged, err)
	}
	// Seed an old update date to make advancement deterministic without sleeps.
	s := state(t, store)
	s.Targets[0].UpdatedAt = "2020-01-01T00:00:00Z"
	if err := store.writeState(s); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTargetSelection(target.ID, []string{skill.ID}, nil); err != nil {
		t.Fatal(err)
	}
	selected := state(t, store).Targets[0]
	if selected.CreatedAt != created || selected.UpdatedAt == s.Targets[0].UpdatedAt {
		t.Fatal("selection change did not update the project's modification date")
	}
	if err := store.SetTargetSelection(target.ID, []string{skill.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if state(t, store).Targets[0].UpdatedAt != selected.UpdatedAt {
		t.Fatal("unchanged selection changed the project's modification date")
	}
	if err := store.Apply(target.ID); err != nil {
		t.Fatal(err)
	}
	if state(t, store).Targets[0].UpdatedAt != selected.UpdatedAt {
		t.Fatal("deployment changed the project's configuration modification date")
	}
	updated := state(t, store).Targets[0]
	updated.Name = "renamed"
	if err := store.SaveTargets([]Target{updated}); err != nil {
		t.Fatal(err)
	}
	renamed := state(t, store).Targets[0]
	if renamed.CreatedAt != created || renamed.UpdatedAt == updated.UpdatedAt {
		t.Fatal("batch configuration save did not preserve creation or update modification date")
	}
}

func TestLegacyProjectDatesMigrateOnceUsingSavedIndexTime(t *testing.T) {
	store, base := newStore(t)
	orderProject(t, store, base, "legacy")
	s := state(t, store)
	s.Targets[0].CreatedAt, s.Targets[0].UpdatedAt = "", ""
	if err := store.writeState(s); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(store.Root(), "state.json")
	saved := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(index, saved, saved); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	target := state(t, reopened).Targets[0]
	if target.CreatedAt != saved.Format(time.RFC3339Nano) || target.UpdatedAt != target.CreatedAt {
		t.Fatalf("legacy dates did not use the previous saved time: %+v", target)
	}
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(store.Root()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("legacy date migration was not stable across reopening", err)
	}
}

func TestProjectDatesTrackIndirectSelectionChangesAndIgnoreUnchangedTargets(t *testing.T) {
	for _, action := range []string{"delete source", "restore snapshot", "cloud removal"} {
		t.Run(action, func(t *testing.T) {
			store, base := newStore(t)
			source := selectionSource(t, store, "https://example.test/dated")
			skill := install(t, store, linkedCandidate(t, base, source, "alpha"))[0]
			preset := savePreset(t, store, "selected", skill)
			project := orderProject(t, store, base, "selected")
			orderProject(t, store, base, "unchanged")
			if err := store.SetTargetSelectionWithSources(project.ID, []string{skill.ID}, []string{preset.ID}, []string{source.ID}); err != nil {
				t.Fatal(err)
			}
			mustApply(t, store, project)
			seeded := state(t, store)
			for i := range seeded.Targets {
				seeded.Targets[i].UpdatedAt = "2020-01-01T00:00:00Z"
			}
			if err := store.writeState(seeded); err != nil {
				t.Fatal(err)
			}
			before := state(t, store)
			snapshot, err := store.Snapshot("selected project state")
			if err != nil {
				t.Fatal(err)
			}
			shared := ExportShared(before)
			shared.Skills, shared.Sources, shared.Presets = []Skill{}, []Source{}, []Preset{}
			apply := func() error {
				switch action {
				case "delete source":
					return store.DeleteSource(source.ID)
				case "restore snapshot":
					return store.RestoreSnapshot(snapshot.ID)
				default:
					return store.ApplyShared(shared)
				}
			}
			if err := apply(); err != nil {
				t.Fatal(err)
			}
			after := state(t, store)
			if after.Targets[0].UpdatedAt == before.Targets[0].UpdatedAt || after.Targets[0].CreatedAt != before.Targets[0].CreatedAt {
				t.Fatal("indirect selection change did not update only the modification date")
			}
			if len(after.Targets[0].EnabledSourceIDs) != 0 || (action != "delete source" && len(after.Targets[0].EnabledSkillIDs)+len(after.Targets[0].EnabledPresetIDs) != 0) {
				t.Fatal("operation did not clear the expected project selections")
			}
			if !reflect.DeepEqual(before.Targets[1], after.Targets[1]) {
				t.Fatal("unaffected project metadata changed")
			}
			if !reflect.DeepEqual(before.Targets[0].Deployments, after.Targets[0].Deployments) || contents(t, filepath.Join(project.Path, "alpha", "SKILL.md")) != "alpha" {
				t.Fatal("selection change modified deployment ownership or deployed content")
			}
			if action == "restore snapshot" {
				entries, err := os.ReadDir(filepath.Join(store.Root(), "snapshots"))
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, entry := range entries {
					var saved snapshotFile
					if err := readJSON(filepath.Join(store.Root(), "snapshots", entry.Name()), &saved); err != nil {
						t.Fatal(err)
					}
					if saved.Info.Reason == "before restoring skills" {
						found = true
						if !reflect.DeepEqual(before.Targets, saved.State.Targets) {
							t.Fatal("restore changed its recovery snapshot's previous project selections or dates")
						}
					}
				}
				if !found {
					t.Fatal("restore did not retain a recovery snapshot")
				}
			}
			if action != "delete source" {
				if err := apply(); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(after.Targets, state(t, store).Targets) {
					t.Fatal("repeating an unchanged restore or cloud merge refreshed project dates")
				}
			}
		})
	}
}
