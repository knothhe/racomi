package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sourceEntry(t *testing.T, c Candidate, revision string) SourceSkill {
	t.Helper()
	hash, err := HashDir(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	return SourceSkill{Name: c.Skill.Name, Description: c.Skill.Description, Subpath: c.Skill.Subpath, Revision: revision, ContentHash: hash, New: true}
}

func refreshedSource(t *testing.T, store *Store, source Source, revision string, candidates ...Candidate) Source {
	t.Helper()
	source.Revision, source.Catalog = revision, []SourceSkill{}
	for _, c := range candidates {
		source.Catalog = append(source.Catalog, sourceEntry(t, c, revision))
	}
	if err := store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	for _, saved := range state(t, store).Sources {
		if saved.ID == source.ID {
			return saved
		}
	}
	t.Fatal("refreshed source missing")
	return Source{}
}

func sourceCandidates(source Source, candidates ...Candidate) []Candidate {
	result := append([]Candidate{}, candidates...)
	for i := range result {
		result[i].Skill.SourceID, result[i].Skill.Revision = source.ID, source.Revision
		for _, entry := range source.Catalog {
			if entry.Subpath == result[i].Skill.Subpath {
				result[i].Skill.ContentHash = entry.ContentHash
			}
		}
	}
	return result
}

func TestOversizedSourceCatalogCannotMakeStoreUnreadable(t *testing.T) {
	store, _ := newStore(t)
	source, err := store.SaveSource(Source{Name: "Large", Repository: "owner/repo", Ref: "main", Subpath: ".", Mode: SourceModeSelected})
	if err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	source.Revision = strings.Repeat("a", 40)
	source.Catalog = []SourceSkill{{Name: "large", Description: strings.Repeat("x", maxJSONBytes), Subpath: "large", Revision: source.Revision, ContentHash: strings.Repeat("b", 64)}}
	if err = store.RecordSourceRefresh(source); err == nil {
		t.Fatal("accepted a catalog larger than the readable state limit")
	}
	if after := state(t, store); !reflect.DeepEqual(before, after) {
		t.Fatal("oversized catalog replaced readable store state")
	}
	if _, err = Open(store.Root()); err != nil {
		t.Fatal("oversized refresh made the store unreadable", err)
	}
}

func TestSchemaOneMigrationKeepsContentAndReferences(t *testing.T) {
	store, base := newStore(t)
	first := candidate(t, base, "alpha", "original")
	first.Skill.ID = "custom-original-id"
	installed := install(t, store, first)[0]
	preset := savePreset(t, store, "keep references", installed)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	legacy := state(t, store)
	legacy.Schema, legacy.Sources = 1, nil
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store.Root(), "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	s := state(t, reopened)
	if s.Schema != 2 || len(s.Sources) != 1 || s.Sources[0].Mode != SourceModeSelected || s.Sources[0].Subpath != "." || s.Sources[0].Repository != installed.Source {
		t.Fatalf("unexpected migrated sources: %+v", s.Sources)
	}
	if s.Skills[0].ID != installed.ID || s.Skills[0].ContentHash != installed.ContentHash || s.Skills[0].InstalledAt != installed.InstalledAt || s.Skills[0].SourceID != s.Sources[0].ID || s.Presets[0].SkillIDs[0] != installed.ID || s.Targets[0].Pending {
		t.Fatalf("migration changed identity, references or desired target: %+v", s)
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "original" {
		t.Fatal("migration changed target content")
	}
	var persisted State
	if err = readJSON(filepath.Join(store.Root(), "state.json"), &persisted); err != nil || persisted.Schema != 2 {
		t.Fatal("migration was not persisted", err)
	}
	if err = reopened.Restore(legacy); err != nil {
		t.Fatal("legacy backup did not migrate", err)
	}
	if state(t, reopened).Sources[0].ID != s.Sources[0].ID {
		t.Fatal("source migration identity was not deterministic")
	}
}

func TestSourceRefreshAndDeletionPreserveLocalContent(t *testing.T) {
	store, base := newStore(t)
	c := candidate(t, base, "alpha", "local version")
	installed := install(t, store, c)[0]
	preset := savePreset(t, store, "default", installed)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	source, err := store.SaveSource(Source{Name: "repository", Repository: c.Skill.Source, Ref: c.Skill.Ref})
	if err != nil {
		t.Fatal(err)
	}
	if state(t, store).Skills[0].SourceID != source.ID {
		t.Fatal("existing installed skill was not associated")
	}
	if err = os.WriteFile(filepath.Join(c.Path, "SKILL.md"), []byte("upstream version"), 0644); err != nil {
		t.Fatal(err)
	}
	source = refreshedSource(t, store, source, "new-revision", c)
	s := state(t, store)
	if s.Skills[0].ContentHash != installed.ContentHash || s.Targets[0].Pending || contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "local version" {
		t.Fatal("refresh installed content or changed a target")
	}
	source.Catalog[0].Removed = true
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	s = state(t, store)
	if !s.Skills[0].UpstreamRemoved || s.Skills[0].ContentHash != installed.ContentHash || s.Presets[0].SkillIDs[0] != installed.ID {
		t.Fatal("upstream removal did not preserve local skill")
	}
	source = s.Sources[0]
	if err = store.RecordSourceError(source.ID, "repository unavailable"); err != nil {
		t.Fatal(err)
	}
	s = state(t, store)
	if s.Sources[0].LastError == "" || s.Sources[0].Revision != source.Revision || !reflect.DeepEqual(s.Sources[0].Catalog, source.Catalog) {
		t.Fatal("refresh failure changed cached inventory")
	}
	s.Sources[0].Catalog[0].Name = "changed"
	if state(t, store).Sources[0].Catalog[0].Name != "alpha" {
		t.Fatal("source catalog leaked a mutable slice")
	}
	source.Catalog[0].Removed = false
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	if state(t, store).Skills[0].UpstreamRemoved || state(t, store).Sources[0].LastError != "" {
		t.Fatal("reappearing upstream skill did not clear removal/error flags")
	}
	source.Catalog = nil
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	if !state(t, store).Skills[0].UpstreamRemoved {
		t.Fatal("missing source path was not marked removed")
	}
	if err = store.DeleteSource(source.ID); err != nil {
		t.Fatal(err)
	}
	s = state(t, store)
	if len(s.Sources) != 0 || len(s.Skills) != 1 || s.Skills[0].SourceID != "" || s.Skills[0].UpstreamRemoved || s.Presets[0].SkillIDs[0] != installed.ID || s.Targets[0].Pending {
		t.Fatal("deleting a source removed or changed local content")
	}
}

func TestSourceSyncBatchIsAtomicAndTargetsStayManual(t *testing.T) {
	store, base := newStore(t)
	a := candidate(t, base, "alpha", "old version")
	a.Skill.ID = "original-alpha-id"
	installed := install(t, store, a)[0]
	target := saveTarget(t, store, base, savePreset(t, store, "default", installed))
	mustApply(t, store, target)
	source, err := store.SaveSource(Source{Name: "selected source", Repository: a.Skill.Source, Ref: a.Skill.Ref, Mode: SourceModeSelected})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(a.Path, "SKILL.md"), []byte("new version"), 0644); err != nil {
		t.Fatal(err)
	}
	b := candidate(t, base, "beta", "new skill")
	source = refreshedSource(t, store, source, "revision-two", a, b)
	prepared := sourceCandidates(source, a, b)
	before := state(t, store)
	if err = store.SyncSource(source.ID, "stale-revision", prepared); err == nil {
		t.Fatal("stale synchronization plan accepted")
	}
	wrong := append([]Candidate{}, prepared...)
	wrong[0].Skill.Source = "other/repository"
	if err = store.SyncSource(source.ID, source.Revision, wrong); err == nil {
		t.Fatal("foreign candidate accepted")
	}
	broken := append([]Candidate{}, prepared...)
	broken[1].Path = filepath.Join(base, "missing")
	if err = store.SyncSource(source.ID, source.Revision, broken); err == nil {
		t.Fatal("invalid second candidate accepted")
	}
	if !reflect.DeepEqual(before, state(t, store)) {
		t.Fatal("failed batch partially changed the index")
	}
	prepared[1].Skill.ID = installed.ID
	if err = store.SyncSource(source.ID, source.Revision, prepared); err != nil {
		t.Fatal(err)
	}
	s := state(t, store)
	if len(s.Skills) != 2 || s.Skills[0].ID != installed.ID || s.Skills[0].InstalledAt != installed.InstalledAt || s.Skills[0].ContentHash == installed.ContentHash || s.Skills[1].ID == installed.ID || !s.Targets[0].Pending {
		t.Fatalf("incorrect batch identity or pending state: %+v", s)
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "old version" {
		t.Fatal("sync wrote to target")
	}
	missing(t, filepath.Join(target.Path, "beta"))
	for _, entry := range s.Sources[0].Catalog {
		if entry.New {
			t.Fatal("installed catalog entry stayed new")
		}
	}
	mustApply(t, store, target)
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "new version" {
		t.Fatal("manual apply did not deploy the source update")
	}
}

func TestRelinkSourceSkillKeepsPresetIdentity(t *testing.T) {
	store, base := newStore(t)
	a := candidate(t, base, "alpha", "old path")
	installed := install(t, store, a)[0]
	preset := savePreset(t, store, "default", installed)
	source, err := store.SaveSource(Source{Name: "source", Repository: a.Skill.Source, Ref: a.Skill.Ref})
	if err != nil {
		t.Fatal(err)
	}
	b := candidate(t, base, "renamed", "new path")
	source = refreshedSource(t, store, source, "revision-three", b)
	if !state(t, store).Skills[0].UpstreamRemoved {
		t.Fatal("old path should be marked removed before relink")
	}
	if err = store.RelinkSourceSkill(installed.ID, source.ID, sourceCandidates(source, b)[0]); err != nil {
		t.Fatal(err)
	}
	s := state(t, store)
	if len(s.Skills) != 1 || s.Skills[0].ID != installed.ID || s.Skills[0].InstalledAt != installed.InstalledAt || s.Skills[0].Name != "renamed" || s.Skills[0].Subpath != "renamed" || s.Skills[0].UpstreamRemoved || s.Presets[0].ID != preset.ID || s.Presets[0].SkillIDs[0] != installed.ID {
		t.Fatal("relink changed references or failed to install new path")
	}
}

func TestSourceValidationAndImmutableOrigin(t *testing.T) {
	store, _ := newStore(t)
	source, err := store.SaveSource(Source{Name: "empty repository", Repository: "owner/repo", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	source.Name, source.Mode = "subscribed", SourceModeAll
	updated, err := store.SaveSource(source)
	if err != nil || updated.Mode != SourceModeAll || updated.Name != "subscribed" {
		t.Fatal("name or mode change failed", err)
	}
	for _, field := range []string{"repository", "ref", "scope"} {
		changed := updated
		switch field {
		case "repository":
			changed.Repository = "other/repo"
		case "ref":
			changed.Ref = "other-branch"
		case "scope":
			changed.Subpath = "nested"
		}
		if _, err = store.SaveSource(changed); err == nil {
			t.Errorf("changed existing source %s", field)
		}
	}
	for _, scope := range []string{"../escape", "/absolute", "a/../b", "a\\b"} {
		if _, err = store.SaveSource(Source{Name: "bad", Repository: "owner/other", Subpath: scope}); err == nil {
			t.Errorf("unsafe source scope %q accepted", scope)
		}
	}
	if _, err = store.SaveSource(Source{Name: "duplicate", Repository: source.Repository, Ref: source.Ref}); err == nil {
		t.Fatal("duplicate source origin accepted")
	}
	bad := updated
	bad.Catalog = []SourceSkill{{Name: "escape", Subpath: "../escape", ContentHash: strings.Repeat("a", 64)}}
	if err = store.RecordSourceRefresh(bad); err == nil {
		t.Fatal("unsafe catalog entry accepted")
	}
	if len(state(t, store).Sources[0].Catalog) != 0 {
		t.Fatal("failed catalog refresh altered inventory")
	}
	if _, err = store.SaveSource(Source{Name: "overlap", Repository: source.Repository, Ref: source.Ref, Subpath: "skills/nested"}); err == nil {
		t.Fatal("overlapping source scope accepted")
	}
	if _, err = store.SaveSource(Source{Name: "another branch", Repository: source.Repository, Ref: "other", Subpath: "skills"}); err != nil {
		t.Fatal("different branches should allow overlapping scopes", err)
	}
	for _, scope := range []string{"skills/first", "skills/second"} {
		if _, err = store.SaveSource(Source{Name: scope, Repository: "disjoint/repo", Ref: "main", Subpath: scope}); err != nil {
			t.Fatal("disjoint scopes rejected", err)
		}
	}
}

func TestSourceFirstRefreshBindsDefaultRefWithoutChangingIDs(t *testing.T) {
	store, base := newStore(t)
	c := candidate(t, base, "alpha", "content")
	c.Skill.Ref = ""
	installed := install(t, store, c)[0]
	source, err := store.SaveSource(Source{Name: "default branch", Repository: c.Skill.Source})
	if err != nil {
		t.Fatal(err)
	}
	source.Ref, source.Revision = "main", "revision-one"
	source.Catalog = []SourceSkill{sourceEntry(t, c, source.Revision)}
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	s := state(t, store)
	if s.Sources[0].Ref != "main" || s.Skills[0].Ref != "main" || s.Skills[0].ID != installed.ID || s.Skills[0].InstalledAt != installed.InstalledAt {
		t.Fatal("default branch binding changed installed identity")
	}
	source.Ref = "other"
	if err = store.RecordSourceRefresh(source); err == nil {
		t.Fatal("second refresh changed a bound default branch")
	}
	other, err := store.SaveSource(Source{Name: "unbound source", Repository: "another/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveSource(Source{Name: "bound overlap", Repository: other.Repository, Ref: "main", Subpath: "skills"}); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	other.Ref = "main"
	if err = store.RecordSourceRefresh(other); err == nil {
		t.Fatal("default ref binding accepted overlapping sources")
	}
	if !reflect.DeepEqual(before, state(t, store)) {
		t.Fatal("failed default ref binding changed the source")
	}
}

func TestSourceSyncCannotReplaceUnassociatedSkill(t *testing.T) {
	store, base := newStore(t)
	c := candidate(t, base, "alpha", "original")
	source, err := store.SaveSource(Source{Name: "source", Repository: c.Skill.Source, Ref: c.Skill.Ref})
	if err != nil {
		t.Fatal(err)
	}
	installed := install(t, store, c)[0]
	if state(t, store).Skills[0].SourceID != "" {
		t.Fatal("test requires a skill installed outside the source")
	}
	source = refreshedSource(t, store, source, "revision-one", c)
	before := state(t, store)
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, c)); err == nil {
		t.Fatal("source sync took over an unassociated skill")
	}
	if !reflect.DeepEqual(before, state(t, store)) || state(t, store).Skills[0].ID != installed.ID {
		t.Fatal("failed synchronization changed local ownership")
	}
}

func TestSourceDefaultRefReRegistrationReconnectsRetainedSkills(t *testing.T) {
	store, base := newStore(t)
	c := candidate(t, base, "alpha", "retained local version")
	c.Skill.ID = "original-retained-alpha"
	installed := install(t, store, c)[0]
	preset := savePreset(t, store, "keep original reference", installed)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	source, err := store.SaveSource(Source{Name: "original source", Repository: c.Skill.Source, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteSource(source.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := store.SaveSource(Source{Name: "default branch source", Repository: c.Skill.Source, Mode: SourceModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if state(t, store).Skills[0].SourceID != "" {
		t.Fatal("unknown default branch should not claim an installed skill")
	}
	if err = os.WriteFile(filepath.Join(c.Path, "SKILL.md"), []byte("new upstream version"), 0644); err != nil {
		t.Fatal(err)
	}
	replacement.Ref, replacement.Revision = "main", "new-revision"
	replacement.Catalog = []SourceSkill{sourceEntry(t, c, replacement.Revision)}
	if err = store.RecordSourceRefresh(replacement); err != nil {
		t.Fatal(err)
	}
	s := state(t, store)
	if s.Skills[0].SourceID != replacement.ID || s.Skills[0].ID != installed.ID || s.Skills[0].InstalledAt != installed.InstalledAt || s.Skills[0].ContentHash != installed.ContentHash || s.Presets[0].SkillIDs[0] != installed.ID || s.Targets[0].Pending {
		t.Fatal("default branch registration did not reconnect the retained identity")
	}
	if err = store.SyncSource(replacement.ID, replacement.Revision, sourceCandidates(replacement, c)); err != nil {
		t.Fatal("reconnected skill could not synchronize", err)
	}
	s = state(t, store)
	if len(s.Skills) != 1 || s.Skills[0].ID != installed.ID || s.Skills[0].InstalledAt != installed.InstalledAt || !s.Targets[0].Pending || contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "retained local version" {
		t.Fatal("reconnected synchronization changed identity or applied target files")
	}
}

func TestSchemaOneMigrationRetainsUntrackableOriginSkills(t *testing.T) {
	store, base := newStore(t)
	c := candidate(t, base, "alpha", "local content")
	installed := install(t, store, c)[0]
	legacy := state(t, store)
	legacy.Schema, legacy.Sources = 1, nil
	origins := []struct {
		id, source, ref, subpath, revision string
	}{
		{"valid-root", c.Skill.Source, "main", "", ""},
		{"duplicate-root", c.Skill.Source, "main", ".", ""},
		{"no-source", "", "main", ".", ""},
		{"control-source", "owner/repo\n", "main", ".", ""},
		{"control-ref", c.Skill.Source, "main\x00", ".", ""},
		{"unsafe-path", c.Skill.Source, "main", "../escape", ""},
		{"control-revision", c.Skill.Source, "main", "different", "bad\nrevision"},
	}
	legacy.Skills = []Skill{}
	members := []string{}
	for _, origin := range origins {
		skill := installed
		skill.ID, skill.Source, skill.Ref, skill.Subpath, skill.Revision = origin.id, origin.source, origin.ref, origin.subpath, origin.revision
		legacy.Skills = append(legacy.Skills, skill)
		members = append(members, skill.ID)
	}
	legacy.Presets = []Preset{{ID: "legacy-preset", Name: "all retained local skills", SkillIDs: members}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store.Root(), "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal("previously valid origin metadata prevented migration", err)
	}
	s := state(t, reopened)
	if len(s.Skills) != len(origins) || len(s.Sources) != 1 || len(s.Sources[0].Catalog) != 1 || !reflect.DeepEqual(s.Presets, legacy.Presets) {
		t.Fatalf("migration lost local skills or references: %+v", s)
	}
	for i, skill := range s.Skills {
		if skill.ID != origins[i].id || skill.ContentHash != installed.ContentHash || skill.InstalledAt != installed.InstalledAt || skill.UpstreamRemoved {
			t.Fatalf("migration altered a retained local skill: %+v", skill)
		}
		if i == 0 {
			if skill.SourceID != s.Sources[0].ID || skill.Subpath != "." {
				t.Fatal("valid root origin was not normalized and associated")
			}
		} else if skill.SourceID != "" || skill.Source != origins[i].source || skill.Ref != origins[i].ref || skill.Subpath != origins[i].subpath || skill.Revision != origins[i].revision {
			t.Fatalf("untrackable origin metadata was changed or associated: %+v", skill)
		}
	}
	if err = reopened.Restore(legacy); err != nil {
		t.Fatal("legacy backup with untrackable origins did not migrate", err)
	}
}
