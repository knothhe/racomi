package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func distributionCandidate(t *testing.T, base, subpath, name, body string) Candidate {
	t.Helper()
	directory := filepath.Join(base, "variants", filepath.FromSlash(subpath))
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return Candidate{Path: directory, Skill: Skill{Name: name, Source: "https://example.test/distributions", Ref: "main", Subpath: subpath}}
}

func TestDistributionFamiliesRetainScopeAndSameTierAmbiguity(t *testing.T) {
	paths := []string{"skills/.curated/first", ".agents/skills/actual", ".claude/skills/copy"}
	key := DistributionKey("captions", paths[0])
	for _, subpath := range paths[1:] {
		if DistributionKey("captions", subpath) != key {
			t.Fatalf("distribution copy not grouped: %s", subpath)
		}
	}
	for _, subpath := range []string{"fixtures/.agents/skills/actual", ".agents/skills/team/actual", "examples/actual", "other/actual"} {
		if DistributionKey("captions", subpath) == key {
			t.Fatalf("unrelated path grouped: %s", subpath)
		}
	}
	if DistributionTier("skills/.curated/first") == DistributionTier("skills/second") || DistributionPriority(".agents/skills/foo") >= DistributionPriority(".claude/skills/foo") {
		t.Fatal("distribution tiers or canonical preference changed")
	}
	catalog := []SourceSkill{{Name: "captions", Subpath: "skills/first"}, {Name: "captions", Subpath: "skills/second"}, {Name: "captions", Subpath: ".agents/skills/copy"}}
	keys := DistributionCatalogKeys(catalog)
	if keys[catalog[0].Subpath] == keys[catalog[1].Subpath] || keys[catalog[0].Subpath] == keys[catalog[2].Subpath] || keys[catalog[1].Subpath] == keys[catalog[2].Subpath] {
		t.Fatal("same-tier ambiguity silently chose a distribution copy")
	}
}

func TestInstallDistributionVariantPreservesIdentity(t *testing.T) {
	store, base := newStore(t)
	first := distributionCandidate(t, base, ".agents/skills/captions", "captions", "agents build")
	second := distributionCandidate(t, base, ".claude/skills/copy", "captions", "claude build")
	installed := install(t, store, first)[0]
	preset := savePreset(t, store, "keep variant", installed)
	selected := saveTarget(t, store, base, preset)
	mustApply(t, store, selected)
	changed := install(t, store, second)[0]
	state := state(t, store)
	if len(state.Skills) != 1 || changed.ID != installed.ID || changed.InstalledAt != installed.InstalledAt || changed.Subpath != second.Skill.Subpath || changed.ContentHash == installed.ContentHash {
		t.Fatalf("variant created a new skill: %+v", state.Skills)
	}
	if !slices.Equal(state.Presets[0].SkillIDs, []string{installed.ID}) || !state.Targets[0].Pending {
		t.Fatal("variant lost references or did not update target content")
	}
}

func TestInstallRejectsTwoDistributionVariantsEvenWithExplicitIDs(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		store, base := newStore(t)
		first := distributionCandidate(t, base, ".agents/skills/captions", "captions", "agents build")
		second := distributionCandidate(t, base, ".claude/skills/copy", "captions", "claude build")
		if explicit {
			first.Skill.ID, second.Skill.ID = "caller-agents", "caller-claude"
		}
		if _, err := store.Install([]Candidate{first, second}); err == nil || !strings.Contains(err.Error(), "one distribution variant") {
			t.Fatalf("accepted two variants (explicit IDs %v): %v", explicit, err)
		}
		if len(state(t, store).Skills) != 0 {
			t.Fatal("invalid variant selection partially installed")
		}
	}
}

func TestMergeImportPreservesHistoricalDistributionIdentitiesAndUpdates(t *testing.T) {
	store, base := newStore(t)
	first := distributionCandidate(t, base, ".agents/skills/captions", "captions", "agents build")
	second := distributionCandidate(t, base, ".claude/skills/copy", "captions", "claude build")
	source := Source{ID: "backup-source", Name: "Distribution", Repository: first.Skill.Source, Ref: first.Skill.Ref, Subpath: ".", Mode: SourceModeSelected, Revision: strings.Repeat("a", 40)}
	source.Catalog = []SourceSkill{sourceEntry(t, first, source.Revision), sourceEntry(t, second, source.Revision)}
	legacy := sourceCandidates(source, first, second)
	legacy[0].Skill.ID, legacy[1].Skill.ID = "legacy-agents", "legacy-claude"
	preset := Preset{ID: "legacy-preset", Name: "Both historical copies", SkillIDs: []string{legacy[0].Skill.ID, legacy[1].Skill.ID}}
	result, err := store.MergeImport(legacy, []Preset{preset}, []Source{source})
	if err != nil || result != (ImportResult{AddedSkills: 2, AddedPresets: 1, AddedSources: 1}) {
		t.Fatalf("historical external store cannot be imported: %+v %v", result, err)
	}
	before := state(t, store)
	if len(before.Skills) != 2 || !slices.Equal(before.Presets[0].SkillIDs, preset.SkillIDs) {
		t.Fatalf("external stable identities changed: %+v", before)
	}
	for _, candidate := range []Candidate{first, second} {
		if err = os.WriteFile(filepath.Join(candidate.Path, "SKILL.md"), []byte(candidate.Skill.Subpath+" updated build"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	source = refreshedSource(t, store, source, strings.Repeat("b", 40), first, second)
	updated := sourceCandidates(source, first, second)
	updated[0].Skill.ID, updated[1].Skill.ID = legacy[0].Skill.ID, legacy[1].Skill.ID
	if err = store.UpdateSkills(updated); err != nil {
		t.Fatal("historical variants cannot be updated by their exact existing IDs", err)
	}
	after := state(t, store)
	if len(after.Skills) != 2 || !slices.Equal(after.Presets[0].SkillIDs, preset.SkillIDs) || after.Skills[0].ID != before.Skills[0].ID || after.Skills[0].ContentHash == before.Skills[0].ContentHash || after.Skills[1].ContentHash == before.Skills[1].ContentHash {
		t.Fatalf("exact historical updates changed identities or skipped content: %+v", after)
	}
}

func TestSyncDistributionVariantPreservesReferencesAndRejectsTwoVariants(t *testing.T) {
	store, base := newStore(t)
	first := distributionCandidate(t, base, ".agents/skills/captions", "captions", "agents build")
	second := distributionCandidate(t, base, ".claude/skills/copy", "captions", "claude build")
	source, err := store.SaveSource(Source{Name: "Distribution", Repository: first.Skill.Source, Ref: first.Skill.Ref, Subpath: "."})
	if err != nil {
		t.Fatal(err)
	}
	source = refreshedSource(t, store, source, strings.Repeat("a", 40), first, second)
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, first)); err != nil {
		t.Fatal(err)
	}
	installed := state(t, store).Skills[0]
	preset := savePreset(t, store, "keep references", installed)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	before := state(t, store)
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, first, second)); err == nil || !strings.Contains(err.Error(), "one distribution variant") {
		t.Fatal("accepted two variants in one operation", err)
	}
	if !reflect.DeepEqual(state(t, store), before) {
		t.Fatal("rejected variant selection changed the store")
	}
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, second)); err != nil {
		t.Fatal(err)
	}
	after := state(t, store)
	if len(after.Skills) != 1 || after.Skills[0].ID != installed.ID || after.Skills[0].InstalledAt != installed.InstalledAt || after.Skills[0].Subpath != second.Skill.Subpath || after.Presets[0].SkillIDs[0] != installed.ID || !after.Targets[0].Pending {
		t.Fatalf("variant switch changed identity or references: %+v", after)
	}
	for _, entry := range after.Sources[0].Catalog {
		if entry.New {
			t.Fatal("imported family still counted as new")
		}
	}
}

func TestSyncDistributionCoalescesHistoricalCopiesAndRemapsAllReferences(t *testing.T) {
	store, base := newStore(t)
	first := distributionCandidate(t, base, ".agents/skills/captions", "captions", "agents build")
	second := distributionCandidate(t, base, ".claude/skills/copy", "captions", "claude build")
	source, err := store.SaveSource(Source{Name: "Distribution", Repository: first.Skill.Source, Ref: first.Skill.Ref, Subpath: "."})
	if err != nil {
		t.Fatal(err)
	}
	source = refreshedSource(t, store, source, strings.Repeat("a", 40), first, second)
	legacy := sourceCandidates(source, first, second)
	legacy[0].Skill.ID, legacy[1].Skill.ID = "legacy-agents", "legacy-claude"
	// Old releases admitted these records separately. Explicit IDs reproduce
	// that stored state without requiring a schema migration at startup.
	a := install(t, store, legacy[0])[0]
	b := install(t, store, legacy[1])[0]
	preset := savePreset(t, store, "both historical copies", a, b)
	target, err := store.SaveTarget(Target{Name: "direct", Kind: "global", Path: filepath.Join(base, "direct"), EnabledSkillIDs: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	both, err := store.SaveTarget(Target{Name: "both", Kind: "global", Path: filepath.Join(base, "both"), EnabledSkillIDs: []string{a.ID, b.ID}, EnabledPresetIDs: []string{preset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, second)); err != nil {
		t.Fatal(err)
	}
	after := state(t, store)
	if len(after.Skills) != 1 || after.Skills[0].ID != b.ID || !slices.Equal(after.Presets[0].SkillIDs, []string{b.ID}) {
		t.Fatalf("historical variants not coalesced: %+v", after)
	}
	for _, selected := range after.Targets {
		if selected.ID == target.ID && (!selected.Pending || selected.Deployments[0].SkillID != b.ID || selected.Deployments[0].ContentHash != a.ContentHash) {
			t.Fatalf("deployment ownership or pending content lost: %+v", selected)
		}
		if selected.ID == both.ID && !slices.Equal(selected.EnabledSkillIDs, []string{b.ID}) {
			t.Fatalf("direct references not deduplicated: %+v", selected)
		}
	}
	snapshots, err := store.Snapshots()
	if err != nil || len(snapshots) == 0 {
		t.Fatal("no recovery snapshot", err)
	}
	if err = store.RestoreSnapshot(snapshots[0].ID); err != nil || len(state(t, store).Skills) != 2 {
		t.Fatal("historical variants cannot be restored", err)
	}
}

func TestSyncDistributionPreservesOwnershipOfHistoricalDeploymentNames(t *testing.T) {
	store, base := newStore(t)
	first := distributionCandidate(t, base, ".agents/skills/captions", "old-agents", "agents build")
	second := distributionCandidate(t, base, ".claude/skills/copy", "old-claude", "claude build")
	source, err := store.SaveSource(Source{Name: "Distribution", Repository: first.Skill.Source, Ref: first.Skill.Ref, Subpath: "."})
	if err != nil {
		t.Fatal(err)
	}
	source = refreshedSource(t, store, source, strings.Repeat("a", 40), first, second)
	legacy := sourceCandidates(source, first, second)
	legacy[0].Skill.ID, legacy[1].Skill.ID = "legacy-agents", "legacy-claude"
	a := install(t, store, legacy[0])[0]
	b := install(t, store, legacy[1])[0]
	preset := savePreset(t, store, "both old names", a, b)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	first.Skill.Name, second.Skill.Name = "captions", "captions"
	source = refreshedSource(t, store, source, strings.Repeat("b", 40), first, second)
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, second)); err != nil {
		t.Fatal(err)
	}
	after := state(t, store)
	if len(after.Skills) != 1 || len(after.Targets[0].Deployments) != 2 || !after.Targets[0].Pending {
		t.Fatalf("historical deployment ownership lost: %+v", after)
	}
	mustApply(t, store, target)
	after = state(t, store)
	if after.Targets[0].Pending || len(after.Targets[0].Deployments) != 1 || after.Targets[0].Deployments[0].SkillID != b.ID {
		t.Fatalf("cannot apply the consolidated skill: %+v", after.Targets[0])
	}
	for _, old := range []string{"old-agents", "old-claude"} {
		if _, err = os.Lstat(filepath.Join(target.Path, old)); !os.IsNotExist(err) {
			t.Fatalf("old deployment directory orphaned: %s %v", old, err)
		}
	}
}

func TestInstallDistributionKeepsUnrelatedPathsAndRefs(t *testing.T) {
	store, base := newStore(t)
	items := []Candidate{
		distributionCandidate(t, base, "skills/first", "captions", "first distinct skill"),
		distributionCandidate(t, base, "skills/second", "captions", "second distinct skill"),
		distributionCandidate(t, base, ".claude/skills/copy", "captions", "copy cannot resolve ambiguity"),
		distributionCandidate(t, base, "examples/one", "captions", "unrelated example"),
		distributionCandidate(t, base, "fixtures/.agents/skills/captions", "captions", "fixture"),
	}
	if installed := install(t, store, items...); len(installed) != len(items) {
		t.Fatal("distinct paths were merged")
	}
	branch := distributionCandidate(t, base, ".agents/skills/captions", "captions", "other branch")
	branch.Skill.Ref = "release"
	install(t, store, branch)
	other := branch
	other.Skill.Source = "https://example.test/another-repository"
	install(t, store, other)
	if len(state(t, store).Skills) != len(items)+2 {
		t.Fatal("repository or ref identities were merged")
	}
}

func TestSyncDistributionDoesNotMergeRemovedSameTierSkills(t *testing.T) {
	store, base := newStore(t)
	first := distributionCandidate(t, base, "skills/first", "captions", "first distinct skill")
	second := distributionCandidate(t, base, "skills/second", "captions", "second distinct skill")
	third := distributionCandidate(t, base, ".agents/skills/copy", "captions", "cannot resolve historical ambiguity")
	source, err := store.SaveSource(Source{Name: "Distribution", Repository: first.Skill.Source, Ref: first.Skill.Ref, Subpath: "."})
	if err != nil {
		t.Fatal(err)
	}
	source = refreshedSource(t, store, source, strings.Repeat("a", 40), first, second, third)
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, first, second)); err != nil {
		t.Fatal(err)
	}
	source.Catalog[0].Removed, source.Catalog[1].Removed = true, true
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	if err = store.SyncSource(source.ID, source.Revision, sourceCandidates(source, third)); err == nil || !strings.Contains(err.Error(), "multiple installed variants") {
		t.Fatal("removed same-tier records silently coalesced", err)
	}
	if !reflect.DeepEqual(state(t, store), before) {
		t.Fatal("ambiguous historical records or references changed")
	}
}
