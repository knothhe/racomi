package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRemoveSkillsPreflightsSelectionAndCreatesOneSnapshot(t *testing.T) {
	for _, scenario := range []string{"missing", "preset", "target", "success"} {
		t.Run(scenario, func(t *testing.T) {
			store, base := newStore(t)
			skills := install(t, store, candidate(t, base, "alpha", "alpha"), candidate(t, base, "beta", "beta"), candidate(t, base, "gamma", "gamma"))
			ids := []string{skills[0].ID, skills[1].ID, skills[0].ID}
			switch scenario {
			case "missing":
				ids = append(ids, "missing")
			case "preset":
				savePreset(t, store, "used", skills[1])
			case "target":
				target := saveTarget(t, store, base)
				if err := store.SetTargetSelection(target.ID, []string{skills[1].ID}, nil); err != nil {
					t.Fatal(err)
				}
			}
			before := state(t, store)
			index := contents(t, filepath.Join(store.Root(), "state.json"))
			snapshots := snapshotCount(t, store)
			err := store.RemoveSkills(ids)
			if scenario != "success" {
				if err == nil {
					t.Fatal("accepted an invalid batch removal")
				}
				if contents(t, filepath.Join(store.Root(), "state.json")) != index || snapshotCount(t, store) != snapshots {
					t.Fatal("failed removal changed the index or snapshots")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			after := state(t, store)
			if len(after.Skills) != 1 || after.Skills[0].ID != skills[2].ID || !reflect.DeepEqual(after.Sources, before.Sources) || snapshotCount(t, store) != snapshots+1 {
				t.Fatalf("unexpected batch removal result: %+v", after)
			}
			items, err := store.Snapshots()
			if err != nil {
				t.Fatal(err)
			}
			if err = store.RestoreSnapshot(items[0].ID); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(state(t, store).Skills, before.Skills) {
				t.Fatal("batch snapshot did not restore every removed skill")
			}
		})
	}
}

func TestUpdateSkillsAcrossSourcesIsAtomic(t *testing.T) {
	store, base := newStore(t)
	a, b := candidate(t, base, "alpha", "alpha v1"), candidate(t, base, "beta", "beta v1")
	b.Skill.Source = "https://example.test/other"
	installed := install(t, store, a, b)
	target := saveTarget(t, store, base, savePreset(t, store, "both", installed...))
	mustApply(t, store, target)
	prepared := []Candidate{}
	for i, c := range []Candidate{a, b} {
		source, err := store.SaveSource(Source{Name: c.Skill.Name, Repository: c.Skill.Source, Ref: c.Skill.Ref, Mode: SourceModeAll})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(c.Path, "SKILL.md"), []byte(c.Skill.Name+" v2"), 0644); err != nil {
			t.Fatal(err)
		}
		source = refreshedSource(t, store, source, "revision-two", c)
		c = sourceCandidates(source, c)[0]
		c.Skill.ID = installed[i].ID
		prepared = append(prepared, c)
	}
	before := state(t, store)
	index := contents(t, filepath.Join(store.Root(), "state.json"))
	snapshots := snapshotCount(t, store)
	for _, scenario := range []string{"broken-content", "changed-source", "changed-revision", "new-id", "duplicate"} {
		bad := append([]Candidate{}, prepared...)
		switch scenario {
		case "broken-content":
			bad[1].Path = filepath.Join(base, "missing")
		case "changed-source":
			bad[1].Skill.SourceID = bad[0].Skill.SourceID
		case "changed-revision":
			bad[1].Skill.Revision = "stale-revision"
		case "new-id":
			bad[1].Skill.ID = "uninstalled-skill"
		case "duplicate":
			bad = append(bad, bad[0])
		}
		if err := store.UpdateSkills(bad); err == nil {
			t.Fatalf("accepted %s update", scenario)
		}
		if contents(t, filepath.Join(store.Root(), "state.json")) != index || snapshotCount(t, store) != snapshots {
			t.Fatalf("failed %s update changed the index or snapshots", scenario)
		}
	}
	if err := store.UpdateSkills(prepared); err != nil {
		t.Fatal(err)
	}
	after := state(t, store)
	for i, skill := range after.Skills {
		if skill.ID != installed[i].ID || skill.InstalledAt != installed[i].InstalledAt || skill.ContentHash == installed[i].ContentHash || skill.Revision != "revision-two" {
			t.Fatalf("update lost identity or did not update content: %+v", skill)
		}
		if contents(t, filepath.Join(target.Path, skill.Name, "SKILL.md")) != skill.Name+" v1" {
			t.Fatal("batch update changed deployed content")
		}
	}
	if !after.Targets[0].Pending || !reflect.DeepEqual(after.Presets, before.Presets) || snapshotCount(t, store) != snapshots+1 {
		t.Fatal("batch update changed presets, omitted pending state, or created multiple snapshots")
	}
	for _, source := range after.Sources {
		if source.Catalog[0].New {
			t.Fatal("updated source entry stayed new")
		}
	}
}
