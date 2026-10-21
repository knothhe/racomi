package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"racomi/internal/testutil"
)

func importCandidate(t *testing.T, base, name, body, id string) Candidate {
	t.Helper()
	item := candidate(t, base, name, body)
	item.Skill.ID = id
	item.Skill.Subpath = "skills/" + name
	return item
}

func snapshotCount(t *testing.T, store *Store) int {
	t.Helper()
	items, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	return len(items)
}

func TestMergeImportPreservesExistingStoreAndIsIdempotent(t *testing.T) {
	store, base := newStore(t)
	alpha := importCandidate(t, base, "alpha", "alpha", "existing-alpha")
	alpha.Skill.Description, alpha.Skill.Revision = "Alpha description", "old-revision"
	installed := install(t, store, alpha)[0]
	source, err := store.SaveSource(Source{ID: "existing-source", Name: "Existing source", Repository: alpha.Skill.Source, Ref: "main", Subpath: ".", Mode: SourceModeAll})
	if err != nil {
		t.Fatal(err)
	}
	source.Revision, source.RefreshedAt = "current-revision", "2026-01-01T00:00:00Z"
	source.Catalog = []SourceSkill{{Name: installed.Name, Description: installed.Description, Subpath: installed.Subpath, ContentHash: installed.ContentHash, Revision: source.Revision}}
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	preset := savePreset(t, store, "H2", installed)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	before := state(t, store)
	snapshots := snapshotCount(t, store)

	alpha.Skill = before.Skills[0]
	alpha.Skill.ID, alpha.Skill.SourceID = "legacy-alpha", "legacy-source"
	alpha.Skill.Revision = "another-revision-with-the-same-content"
	alpha.Skill.InstalledAt = "2020-01-01T00:00:00Z"
	beta := importCandidate(t, base, "beta", "beta", "legacy-beta")
	beta.Skill.SourceID = "legacy-source"
	beta.Skill.InstalledAt, beta.Skill.UpdatedAt = "2021-01-02T03:04:05.123Z", "2022-02-03T04:05:06Z"
	incomingSource := Source{ID: "legacy-source", Name: "Legacy source", Repository: source.Repository, Ref: "main", Subpath: "skills", Mode: SourceModeSelected}
	presets := []Preset{
		{ID: "legacy-h2", Name: "H2", Description: "Legacy description", SkillIDs: []string{alpha.Skill.ID}},
		{ID: "legacy-extra", Name: "Imported", SkillIDs: []string{beta.Skill.ID, alpha.Skill.ID}},
	}
	want := ImportResult{AddedSkills: 1, SkippedSkills: 1, AddedPresets: 1, SkippedPresets: 1, SkippedSources: 1}
	got, err := store.MergeImport([]Candidate{alpha, beta}, presets, []Source{incomingSource})
	if err != nil || got != want {
		t.Fatalf("unexpected import: %+v, %v", got, err)
	}
	after := state(t, store)
	if !reflect.DeepEqual(after.Targets, before.Targets) || !reflect.DeepEqual(after.Sources, before.Sources) || !reflect.DeepEqual(after.Skills[0], before.Skills[0]) || !reflect.DeepEqual(after.Presets[0], before.Presets[0]) {
		t.Fatal("import changed existing content, source configuration, preset, or target")
	}
	if after.Skills[1].SourceID != source.ID || after.Skills[1].InstalledAt != beta.Skill.InstalledAt || after.Skills[1].UpdatedAt != beta.Skill.UpdatedAt {
		t.Fatalf("import lost source mapping or original timestamps: %+v", after.Skills[1])
	}
	if !reflect.DeepEqual(after.Presets[1].SkillIDs, []string{beta.Skill.ID, installed.ID}) {
		t.Fatalf("preset members were not remapped: %+v", after.Presets[1])
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "alpha" {
		t.Fatal("import altered a deployed skill")
	}
	missing(t, filepath.Join(target.Path, "beta"))
	if snapshotCount(t, store) != snapshots+1 {
		t.Fatal("import must create exactly one recovery snapshot")
	}
	items, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	var recovery snapshotFile
	if err = readJSON(filepath.Join(store.root, "snapshots", items[0].ID+".json"), &recovery); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovery.State, before) {
		t.Fatal("recovery snapshot does not contain the complete state before import")
	}
	indexBefore := contents(t, filepath.Join(store.root, "state.json"))
	want = ImportResult{SkippedSkills: 2, SkippedPresets: 2, SkippedSources: 1}
	got, err = store.MergeImport([]Candidate{alpha, beta}, presets, []Source{incomingSource})
	if err != nil || got != want {
		t.Fatalf("repeated import was not idempotent: %+v, %v", got, err)
	}
	if contents(t, filepath.Join(store.root, "state.json")) != indexBefore || snapshotCount(t, store) != snapshots+1 {
		t.Fatal("a repeated import wrote an index or created another snapshot")
	}
}

func TestMergeImportFailuresLeaveIndexAndSnapshotsUntouched(t *testing.T) {
	type input struct {
		candidates []Candidate
		presets    []Preset
		sources    []Source
	}
	tests := []struct {
		name   string
		change func(*testing.T, *input, Candidate, Source, Preset)
	}{
		{"changed-content-same-id", func(t *testing.T, in *input, old Candidate, _ Source, _ Preset) {
			old.Path = in.candidates[0].Path
			in.candidates = append(in.candidates, old)
		}},
		{"changed-content-same-origin", func(t *testing.T, in *input, old Candidate, _ Source, _ Preset) {
			old.Skill.ID, old.Path = "legacy-alpha", in.candidates[0].Path
			in.candidates = append(in.candidates, old)
		}},
		{"changed-metadata", func(t *testing.T, in *input, old Candidate, _ Source, _ Preset) {
			old.Skill.Name = "renamed"
			in.candidates = append(in.candidates, old)
		}},
		{"duplicate-skill-id", func(t *testing.T, in *input, _ Candidate, _ Source, _ Preset) {
			in.candidates = append(in.candidates, in.candidates[0])
		}},
		{"skipped-candidate-hash-mismatch", func(t *testing.T, in *input, old Candidate, _ Source, _ Preset) {
			old.Skill.ContentHash = "incorrect"
			in.candidates = append(in.candidates, old)
		}},
		{"skipped-candidate-missing-skill-file", func(t *testing.T, in *input, old Candidate, _ Source, _ Preset) {
			old.Path = testDir(t)
			in.candidates = append(in.candidates, old)
		}},
		{"skipped-candidate-symlink", func(t *testing.T, in *input, old Candidate, _ Source, _ Preset) {
			if err := testutil.Symlink(t, in.candidates[0].Path, filepath.Join(old.Path, "linked")); err != nil {
				t.Skip(err)
			}
			in.candidates = append(in.candidates, old)
		}},
		{"missing-source-reference", func(t *testing.T, in *input, _ Candidate, _ Source, _ Preset) {
			in.candidates[0].Skill.SourceID = "missing-source"
		}},
		{"duplicate-source-id", func(t *testing.T, in *input, _ Candidate, source Source, _ Preset) {
			in.sources = []Source{source, source}
		}},
		{"source-id-conflict", func(t *testing.T, in *input, _ Candidate, source Source, _ Preset) {
			source.Repository = "https://example.test/other"
			in.sources = []Source{source}
		}},
		{"source-scope-overlap", func(t *testing.T, in *input, _ Candidate, source Source, _ Preset) {
			source.ID, source.Subpath = "legacy-source", "."
			in.sources = []Source{source}
		}},
		{"skipped-source-invalid-catalog", func(t *testing.T, in *input, _ Candidate, source Source, _ Preset) {
			source.ID = "legacy-source"
			source.Catalog = []SourceSkill{{Name: "alpha", Subpath: "skills/alpha", ContentHash: "invalid"}}
			in.sources = []Source{source}
		}},
		{"preset-id-conflict", func(t *testing.T, in *input, _ Candidate, _ Source, preset Preset) {
			preset.SkillIDs = []string{in.candidates[0].Skill.ID}
			in.presets = []Preset{preset}
		}},
		{"missing-preset-member", func(t *testing.T, in *input, _ Candidate, _ Source, _ Preset) {
			in.presets[0].SkillIDs = append(in.presets[0].SkillIDs, "missing-skill")
		}},
		{"duplicate-preset-id", func(t *testing.T, in *input, _ Candidate, _ Source, _ Preset) {
			in.presets = append(in.presets, in.presets[0])
		}},
		{"duplicate-preset-member", func(t *testing.T, in *input, _ Candidate, _ Source, _ Preset) {
			in.presets[0].SkillIDs = append(in.presets[0].SkillIDs, in.presets[0].SkillIDs[0])
		}},
		{"invalid-time", func(t *testing.T, in *input, _ Candidate, _ Source, _ Preset) {
			in.candidates[0].Skill.InstalledAt = "yesterday"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, base := newStore(t)
			old := importCandidate(t, base, "alpha", "alpha", "existing-alpha")
			old.Skill = install(t, store, old)[0]
			source, err := store.SaveSource(Source{ID: "existing-source", Name: "Existing source", Repository: old.Skill.Source, Ref: "main", Subpath: "skills", Mode: SourceModeSelected})
			if err != nil {
				t.Fatal(err)
			}
			old.Skill = state(t, store).Skills[0]
			preset := savePreset(t, store, "Existing", old.Skill)
			beta := importCandidate(t, base, "beta", "beta", "legacy-beta")
			beta.Skill.SourceID = source.ID
			in := input{candidates: []Candidate{beta}, presets: []Preset{{ID: "legacy-preset", Name: "Legacy", SkillIDs: []string{beta.Skill.ID}}}}
			test.change(t, &in, old, source, preset)
			indexBefore := contents(t, filepath.Join(store.root, "state.json"))
			snapshots := snapshotCount(t, store)
			got, err := store.MergeImport(in.candidates, in.presets, in.sources)
			if err == nil || got != (ImportResult{}) {
				t.Fatalf("expected a cancelled import, got %+v, %v", got, err)
			}
			if contents(t, filepath.Join(store.root, "state.json")) != indexBefore || snapshotCount(t, store) != snapshots {
				t.Fatal("failed import partially changed the index or snapshots")
			}
			if contents(t, filepath.Join(store.root, "objects", old.Skill.ContentHash, "SKILL.md")) != "alpha" {
				t.Fatal("failed import replaced existing content")
			}
		})
	}
}

func TestMergeImportReusesLocalContentAndRemapsPresetSets(t *testing.T) {
	store, base := newStore(t)
	local := importCandidate(t, base, "local", "local", "existing-local")
	local.Skill.Source, local.Skill.Ref, local.Skill.Subpath = "", "", ""
	installed := install(t, store, local)[0]
	preset := savePreset(t, store, "Local", installed)
	local.Skill = installed
	local.Skill.ID = "legacy-local"
	alias := local
	alias.Skill.ID = "legacy-local-alias"
	before := state(t, store)
	snapshots := snapshotCount(t, store)
	got, err := store.MergeImport([]Candidate{local, alias}, []Preset{{ID: "legacy-preset", Name: preset.Name, SkillIDs: []string{local.Skill.ID, alias.Skill.ID}}}, nil)
	if err != nil || got != (ImportResult{SkippedSkills: 2, SkippedPresets: 1}) {
		t.Fatalf("local aliases were not reused: %+v, %v", got, err)
	}
	if !reflect.DeepEqual(state(t, store), before) || snapshotCount(t, store) != snapshots {
		t.Fatal("reusing local content altered the store")
	}
	local.Skill.Description = "different metadata"
	if _, err = store.MergeImport([]Candidate{local}, nil, nil); err == nil {
		t.Fatal("a changed description must not be silently discarded")
	}
}

func TestMergeImportReusesNewBatchAliases(t *testing.T) {
	store, base := newStore(t)
	alpha := importCandidate(t, base, "alpha", "alpha", "legacy-alpha")
	alias := alpha
	alias.Skill.ID = "legacy-alias"
	got, err := store.MergeImport([]Candidate{alpha, alias}, []Preset{{ID: "legacy-preset", Name: "Legacy", SkillIDs: []string{alpha.Skill.ID, alias.Skill.ID}}}, nil)
	if err != nil || got != (ImportResult{AddedSkills: 1, SkippedSkills: 1, AddedPresets: 1}) {
		t.Fatalf("batch alias merge failed: %+v, %v", got, err)
	}
	after := state(t, store)
	if len(after.Skills) != 1 || !reflect.DeepEqual(after.Presets[0].SkillIDs, []string{alpha.Skill.ID}) || snapshotCount(t, store) != 1 {
		t.Fatalf("unexpected merged batch: %+v", after)
	}
}

func TestMergeImportChecksReusedObjects(t *testing.T) {
	store, base := newStore(t)
	alpha := importCandidate(t, base, "alpha", "alpha", "legacy-alpha")
	alpha.Skill = install(t, store, alpha)[0]
	if err := os.WriteFile(filepath.Join(store.root, "objects", alpha.Skill.ContentHash, "SKILL.md"), []byte("damaged"), 0644); err != nil {
		t.Fatal(err)
	}
	indexBefore := contents(t, filepath.Join(store.root, "state.json"))
	snapshots := snapshotCount(t, store)
	if _, err := store.MergeImport([]Candidate{alpha}, nil, nil); err == nil {
		t.Fatal("a damaged reused content object must fail import")
	}
	if contents(t, filepath.Join(store.root, "state.json")) != indexBefore || snapshotCount(t, store) != snapshots {
		t.Fatal("damaged-object import changed the index or snapshots")
	}
}

func TestMergeImportSerializesConcurrentImports(t *testing.T) {
	store, base := newStore(t)
	alpha := importCandidate(t, base, "alpha", "alpha", "legacy-alpha")
	alpha.Skill.SourceID = "legacy-source"
	source := Source{ID: "legacy-source", Name: "Source", Repository: alpha.Skill.Source, Ref: alpha.Skill.Ref, Subpath: ".", Mode: SourceModeSelected}
	preset := Preset{ID: "legacy-preset", Name: "Legacy", SkillIDs: []string{alpha.Skill.ID}}
	var workers sync.WaitGroup
	results := make(chan ImportResult, 8)
	errors := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			result, err := store.MergeImport([]Candidate{alpha}, []Preset{preset}, []Source{source})
			results <- result
			errors <- err
		})
	}
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	added, skipped := 0, 0
	for result := range results {
		added += result.AddedSkills + result.AddedPresets + result.AddedSources
		skipped += result.SkippedSkills + result.SkippedPresets + result.SkippedSources
	}
	if added != 3 || skipped != 21 || snapshotCount(t, store) != 1 {
		t.Fatalf("concurrent import did not commit once: added=%d skipped=%d", added, skipped)
	}
	data, err := json.Marshal(ImportResult{})
	if err != nil || string(data) != `{"addedSkills":0,"skippedSkills":0,"addedPresets":0,"skippedPresets":0,"addedSources":0,"skippedSources":0}` {
		t.Fatalf("unexpected import result JSON: %s, %v", data, err)
	}
}
