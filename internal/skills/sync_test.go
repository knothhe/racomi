package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func cloneSharedForTest(shared SharedState) SharedState { return canonicalShared(shared) }

func copySharedObjects(t *testing.T, from, to *Store, shared SharedState) {
	t.Helper()
	for _, skill := range shared.Skills {
		destination := filepath.Join(to.Root(), "objects", skill.ContentHash)
		if _, err := os.Stat(destination); err == nil {
			continue
		}
		if err := CopyDir(filepath.Join(from.Root(), "objects", skill.ContentHash), destination); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSharedTwoDeviceMergeAndOfflineDeletion(t *testing.T) {
	a, aBase := newStore(t)
	b, bBase := newStore(t)
	alpha := install(t, a, candidate(t, aBase, "alpha", "alpha"))[0]
	initial := savePreset(t, a, "initial", alpha)
	common := ExportShared(state(t, a))
	copySharedObjects(t, a, b, common)
	if err := b.ApplyShared(common); err != nil {
		t.Fatal(err)
	}
	// A adds a skill/preset while offline. B independently edits the old preset.
	beta := install(t, a, candidate(t, aBase, "beta", "beta"))[0]
	added := savePreset(t, a, "added on A", beta)
	initial.Description = "edited on B"
	if _, err := b.SavePreset(initial); err != nil {
		t.Fatal(err)
	}
	merged, conflicts, err := MergeShared(common, ExportShared(state(t, a)), ExportShared(state(t, b)), nil)
	if err != nil || len(conflicts) != 0 || len(merged.Skills) != 2 || len(merged.Presets) != 2 {
		t.Fatalf("independent edits did not merge: %+v, %+v, %v", merged, conflicts, err)
	}
	copySharedObjects(t, a, b, merged)
	for _, store := range []*Store{a, b} {
		if err = store.ApplyShared(merged); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(ExportShared(state(t, a)), ExportShared(state(t, b))) {
		t.Fatal("devices did not converge")
	}
	// B stays offline with the common state; A deletes a preset. Its absence
	// must win against B's unchanged copy even if B adds unrelated data later.
	if err = a.DeletePreset(added.ID); err != nil {
		t.Fatal(err)
	}
	install(t, b, candidate(t, bBase, "gamma", "gamma"))
	deleted, conflicts, err := MergeShared(merged, ExportShared(state(t, b)), ExportShared(state(t, a)), nil)
	if err != nil || len(conflicts) != 0 || len(deleted.Skills) != 3 || slices.ContainsFunc(deleted.Presets, func(p Preset) bool { return p.ID == added.ID }) {
		t.Fatalf("offline device resurrected deletion: %+v, %+v, %v", deleted, conflicts, err)
	}
}

func TestSharedConflictsPreserveBothVersions(t *testing.T) {
	store, baseDir := newStore(t)
	skill := install(t, store, candidate(t, baseDir, "alpha", "alpha"))[0]
	preset := savePreset(t, store, "original", skill)
	base := ExportShared(state(t, store))
	local, remote := cloneSharedForTest(base), cloneSharedForTest(base)
	local.Presets[0].Name, remote.Presets[0].Name = "local name", "remote name"
	local.Settings.Theme, remote.Settings.Theme = "light", "dark"
	_, conflicts, err := MergeShared(base, local, remote, nil)
	if err != nil || len(conflicts) != 2 {
		t.Fatalf("expected preset and settings conflicts, got %+v, %v", conflicts, err)
	}
	var versions [2]Preset
	for _, conflict := range conflicts {
		if conflict.Key == "preset:"+preset.ID {
			if err = json.Unmarshal(conflict.Local, &versions[0]); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(conflict.Remote, &versions[1]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if versions[0].Name != "local name" || versions[1].Name != "remote name" {
		t.Fatal("conflict did not retain both versions")
	}
	merged, conflicts, err := MergeShared(base, local, remote, map[string]string{"preset:" + preset.ID: "remote", "settings:theme": "local"})
	if err != nil || len(conflicts) != 0 || merged.Presets[0].Name != "remote name" || merged.Settings.Theme != "light" {
		t.Fatalf("resolutions were not applied: %+v, %+v, %v", merged, conflicts, err)
	}
	if _, _, err = MergeShared(base, local, remote, map[string]string{"settings:theme": "newest"}); err == nil {
		t.Fatal("invalid resolution accepted")
	}
}

func TestSharedDeleteVersusNewPresetMemberConflicts(t *testing.T) {
	store, baseDir := newStore(t)
	skill := install(t, store, candidate(t, baseDir, "alpha", "alpha"))[0]
	base := ExportShared(state(t, store))
	local, remote := cloneSharedForTest(base), cloneSharedForTest(base)
	local.Skills = []Skill{}
	remote.Presets = []Preset{{ID: "preset-new", Name: "new reference", SkillIDs: []string{skill.ID}}}
	_, conflicts, err := MergeShared(base, local, remote, nil)
	if err != nil || len(conflicts) != 1 || conflicts[0].Key != "skill:"+skill.ID || string(conflicts[0].Local) != "null" {
		t.Fatalf("cross-record deletion silently discarded membership: %+v, %v", conflicts, err)
	}
	keep, conflicts, err := MergeShared(base, local, remote, map[string]string{"skill:" + skill.ID: "remote"})
	if err != nil || len(conflicts) != 0 || len(keep.Skills) != 1 || !slices.Equal(keep.Presets[0].SkillIDs, []string{skill.ID}) {
		t.Fatalf("keep resolution lost reference: %+v, %+v, %v", keep, conflicts, err)
	}
	remove, conflicts, err := MergeShared(base, local, remote, map[string]string{"skill:" + skill.ID: "local"})
	if err != nil || len(conflicts) != 0 || len(remove.Skills) != 0 || len(remove.Presets[0].SkillIDs) != 0 {
		t.Fatalf("delete resolution did not clean reference: %+v, %+v, %v", remove, conflicts, err)
	}
}

func TestSharedSameSourceIDsAndSkillReferencesConverge(t *testing.T) {
	base := EmptyShared()
	local, remote := cloneSharedForTest(base), cloneSharedForTest(base)
	source := Source{ID: "source-z", Name: "source", Repository: "https://example.test/repo", Ref: "main", Subpath: ".", Mode: SourceModeSelected, Catalog: []SourceSkill{}}
	skill := Skill{ID: "skill-z", Name: "alpha", Source: source.Repository, Ref: source.Ref, SourceID: source.ID, Subpath: "alpha", ContentHash: strings.Repeat("a", 64)}
	skill.InstalledAt, skill.UpdatedAt = "2026-10-01T01:00:00Z", "2026-10-01T01:00:00Z"
	local.Sources, local.Skills = []Source{source}, []Skill{skill}
	local.Presets = []Preset{{ID: "preset-local", Name: "local", SkillIDs: []string{skill.ID}}}
	source.ID, skill.ID, skill.SourceID = "source-a", "skill-a", "source-a"
	skill.InstalledAt, skill.UpdatedAt = "2026-10-02T01:00:00Z", "2026-10-02T01:00:00Z"
	remote.Sources, remote.Skills = []Source{source}, []Skill{skill}
	remote.Presets = []Preset{{ID: "preset-remote", Name: "remote", SkillIDs: []string{skill.ID}}}
	merged, conflicts, err := MergeShared(base, local, remote, nil)
	if err != nil || len(conflicts) != 0 || len(merged.Sources) != 1 || len(merged.Skills) != 1 || len(merged.Presets) != 2 {
		t.Fatalf("same origin duplicated: %+v, %+v, %v", merged, conflicts, err)
	}
	if merged.Sources[0].ID != "source-a" || merged.Skills[0].ID != "skill-a" || merged.Skills[0].SourceID != "source-a" {
		t.Fatal("identity choice is not deterministic")
	}
	for _, preset := range merged.Presets {
		if !slices.Equal(preset.SkillIDs, []string{"skill-a"}) {
			t.Fatal("preset reference was not remapped")
		}
	}
	opposite, conflicts, err := MergeShared(base, remote, local, nil)
	if err != nil || len(conflicts) != 0 || !reflect.DeepEqual(merged, opposite) {
		t.Fatalf("merge depended on device order: %+v, %+v, %v", opposite, conflicts, err)
	}
}

func TestSharedIndependentInstallationsIgnoreOnlyClocks(t *testing.T) {
	base := EmptyShared()
	skill := Skill{ID: "skill-same-origin", Name: "alpha", Source: "https://example.test/repo", Ref: "main", Subpath: "alpha", Revision: "revision-one", ContentHash: strings.Repeat("a", 64), InstalledAt: "2026-10-01T01:00:00Z", UpdatedAt: "2026-10-01T01:00:00Z"}
	for _, test := range []struct {
		name       string
		change     func(*Skill)
		withBase   bool
		conflicted bool
	}{
		{name: "equal deterministic IDs"},
		{name: "different content", change: func(s *Skill) { s.ContentHash = strings.Repeat("b", 64) }, conflicted: true},
		{name: "different revision", change: func(s *Skill) { s.Revision = "revision-two" }, conflicted: true},
		{name: "different metadata", change: func(s *Skill) { s.Description = "edited description" }, conflicted: true},
		{name: "existing baseline clocks", withBase: true, conflicted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			common, local, remote := cloneSharedForTest(base), cloneSharedForTest(base), cloneSharedForTest(base)
			local.Skills, remote.Skills = []Skill{skill}, []Skill{skill}
			remote.Skills[0].InstalledAt, remote.Skills[0].UpdatedAt = "2026-10-02T01:00:00Z", "2026-10-02T01:00:00Z"
			if test.withBase {
				common.Skills = []Skill{skill}
				local.Skills[0].UpdatedAt = "2026-10-03T01:00:00Z"
			}
			if test.change != nil {
				test.change(&remote.Skills[0])
			}
			merged, conflicts, err := MergeShared(common, local, remote, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.conflicted {
				if len(conflicts) != 1 || conflicts[0].Key != "skill:"+skill.ID {
					t.Fatalf("real difference did not conflict: %+v", conflicts)
				}
				var versions [2]Skill
				for i, value := range []json.RawMessage{conflicts[0].Local, conflicts[0].Remote} {
					if err := json.Unmarshal(value, &versions[i]); err != nil {
						t.Fatal(err)
					}
				}
				if versions[0] != local.Skills[0] || versions[1] != remote.Skills[0] {
					t.Fatalf("conflicting records were rewritten: %+v", versions)
				}
				return
			}
			if len(conflicts) != 0 || len(merged.Skills) != 1 || merged.Skills[0].InstalledAt != skill.InstalledAt || merged.Skills[0].UpdatedAt != remote.Skills[0].UpdatedAt {
				t.Fatalf("independent installation clocks caused a conflict: %+v, %+v", merged.Skills, conflicts)
			}
			opposite, conflicts, err := MergeShared(common, remote, local, nil)
			if err != nil || len(conflicts) != 0 || !reflect.DeepEqual(merged, opposite) {
				t.Fatalf("clock normalization depended on device order: %+v, %+v, %v", opposite.Skills, conflicts, err)
			}
		})
	}
}

func TestSharedOnlyLocalDefaultSelectionsRoundTrip(t *testing.T) {
	store, baseDir := newStore(t)
	target, err := store.SaveTarget(Target{Name: "local global directory", Kind: "global", Path: filepath.Join(baseDir, "target")})
	if err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	before.Settings.CustomProjectDirectories = []ProjectDirectory{{ID: "legacy-absolute", Name: "legacy", Path: filepath.Join(baseDir, "legacy")}}
	before.Settings.DefaultProjectDirectories = []string{"legacy-absolute"}
	before.Settings.DefaultGlobalDirectories = []string{target.ID}
	if err := store.writeState(before); err != nil {
		t.Fatal(err)
	}
	shared := ExportShared(before)
	if shared.Settings.DefaultProjectDirectories == nil || len(shared.Settings.DefaultProjectDirectories) != 0 || shared.Settings.DefaultGlobalDirectories == nil || len(shared.Settings.DefaultGlobalDirectories) != 0 {
		t.Fatalf("export selected unchosen portable defaults: %+v", shared.Settings)
	}
	merged, conflicts, err := MergeShared(EmptyShared(), shared, EmptyShared(), nil)
	if err != nil || len(conflicts) != 0 || len(merged.Settings.DefaultProjectDirectories) != 0 || len(merged.Settings.DefaultGlobalDirectories) != 0 {
		t.Fatalf("first sync replaced local-only selections: %+v, %+v, %v", merged.Settings, conflicts, err)
	}
	if err := store.ValidateShared(merged); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyShared(merged); err != nil {
		t.Fatal(err)
	}
	after := state(t, store)
	if !slices.Equal(after.Settings.DefaultProjectDirectories, before.Settings.DefaultProjectDirectories) || !slices.Equal(after.Settings.DefaultGlobalDirectories, before.Settings.DefaultGlobalDirectories) || !reflect.DeepEqual(after.Settings.CustomProjectDirectories, before.Settings.CustomProjectDirectories) || !reflect.DeepEqual(after.Targets, before.Targets) {
		t.Fatalf("round trip enabled unchosen defaults: %+v", after.Settings)
	}
	if !reflect.DeepEqual(ExportShared(after), shared) {
		t.Fatal("local-only defaults changed the shared snapshot after applying it")
	}
	// A device without matching local selections still needs valid defaults.
	fresh, _ := newStore(t)
	if err := fresh.ApplyShared(merged); err != nil {
		t.Fatal(err)
	}
	if got := state(t, fresh).Settings; !slices.Equal(got.DefaultProjectDirectories, []string{"agents"}) || !slices.Equal(got.DefaultGlobalDirectories, []string{"codex-user"}) {
		t.Fatalf("fresh device did not reconstruct required defaults: %+v", got)
	}
}

func TestSharedSourceDeleteVersusNewLinkedSkillConflicts(t *testing.T) {
	base := EmptyShared()
	source := Source{ID: "source-original", Name: "source", Repository: "https://example.test/repo", Ref: "main", Subpath: ".", Mode: SourceModeSelected, Catalog: []SourceSkill{}}
	base.Sources = []Source{source}
	local, remote := cloneSharedForTest(base), cloneSharedForTest(base)
	local.Sources = []Source{}
	remote.Skills = []Skill{{ID: "skill-new", Name: "alpha", Source: source.Repository, Ref: source.Ref, SourceID: source.ID, Subpath: "alpha", ContentHash: strings.Repeat("a", 64)}}
	_, conflicts, err := MergeShared(base, local, remote, nil)
	if err != nil || len(conflicts) != 1 || conflicts[0].Key != "source:"+source.ID {
		t.Fatalf("source deletion silently detached new skill: %+v, %v", conflicts, err)
	}
	keep, conflicts, err := MergeShared(base, local, remote, map[string]string{"source:" + source.ID: "remote"})
	if err != nil || len(conflicts) != 0 || len(keep.Sources) != 1 || keep.Skills[0].SourceID != source.ID {
		t.Fatalf("source keep resolution lost association: %+v, %+v, %v", keep, conflicts, err)
	}
	remove, conflicts, err := MergeShared(base, local, remote, map[string]string{"source:" + source.ID: "local"})
	if err != nil || len(conflicts) != 0 || len(remove.Sources) != 0 || remove.Skills[0].SourceID != "" || remove.Skills[0].ContentHash != remote.Skills[0].ContentHash {
		t.Fatalf("source delete resolution removed skill content: %+v, %+v, %v", remove, conflicts, err)
	}
}

func TestSharedTemplateDeletionVersusDefaultSelectionConflicts(t *testing.T) {
	base := EmptyShared()
	base.Settings.CustomProjectDirectories = []ProjectDirectory{{ID: "custom", Name: "custom", Path: ".custom/skills"}}
	local, remote := cloneSharedForTest(base), cloneSharedForTest(base)
	local.Settings.CustomProjectDirectories = []ProjectDirectory{}
	remote.Settings.DefaultProjectDirectories = []string{"custom"}
	_, conflicts, err := MergeShared(base, local, remote, nil)
	if err != nil || len(conflicts) != 1 || conflicts[0].Key != "settings:customProjectDirectories" {
		t.Fatalf("template deletion did not conflict with new default reference: %+v, %v", conflicts, err)
	}
	keep, conflicts, err := MergeShared(base, local, remote, map[string]string{"settings:customProjectDirectories": "remote"})
	if err != nil || len(conflicts) != 0 || len(keep.Settings.CustomProjectDirectories) != 1 || !slices.Equal(keep.Settings.DefaultProjectDirectories, []string{"custom"}) {
		t.Fatalf("template keep resolution lost selection: %+v, %+v, %v", keep, conflicts, err)
	}
	remove, conflicts, err := MergeShared(base, local, remote, map[string]string{"settings:customProjectDirectories": "local"})
	if err != nil || len(conflicts) != 0 || len(remove.Settings.CustomProjectDirectories) != 0 || len(remove.Settings.DefaultProjectDirectories) != 0 {
		t.Fatalf("template delete resolution retained dangling selection: %+v, %+v, %v", remove, conflicts, err)
	}
}

func TestSharedApplyPreservesMachineBoundariesAndDeployment(t *testing.T) {
	store, baseDir := newStore(t)
	skill := install(t, store, candidate(t, baseDir, "alpha", "alpha"))[0]
	preset := savePreset(t, store, "preset", skill)
	target := saveTarget(t, store, baseDir, preset)
	mustApply(t, store, target)
	before := state(t, store)
	// Seed legacy absolute templates, which SaveSettings intentionally permits
	// only when reading/editing existing settings from previous app versions.
	before.Settings.CustomProjectDirectories = []ProjectDirectory{{ID: "legacy-absolute", Name: "legacy", Path: filepath.Join(baseDir, "legacy")}, {ID: "portable", Name: "portable", Path: ".custom/skills"}}
	before.Settings.DefaultProjectDirectories = []string{"legacy-absolute", "portable"}
	before.Settings.DefaultGlobalDirectories = []string{target.ID}
	before.Sources = []Source{{ID: "source-local", Name: "local source", Repository: filepath.Join(baseDir, "repo"), Ref: "main", Subpath: ".", Mode: SourceModeSelected, LastError: "private local path", Catalog: []SourceSkill{}}}
	before.Skills[0].Source, before.Skills[0].SourceID = before.Sources[0].Repository, before.Sources[0].ID
	if err := store.writeState(before); err != nil {
		t.Fatal(err)
	}
	before = state(t, store)
	shared := ExportShared(before)
	encoded, err := json.Marshal(shared)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), baseDir) || strings.Contains(string(encoded), target.ID) || len(shared.Sources) != 0 || shared.Skills[0].Source != "" || len(shared.Settings.CustomProjectDirectories) != 1 {
		t.Fatalf("machine data leaked into export: %s", encoded)
	}
	shared.Settings.Theme, shared.Settings.Locale, shared.Settings.NavLayout = "dark", "en", "side"
	shared.Presets[0].Name = "synced preset"
	if err = store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	after := state(t, store)
	if !reflect.DeepEqual(after.Targets, before.Targets) || after.Skills[0].Source != before.Skills[0].Source || after.Skills[0].SourceID != before.Skills[0].SourceID || !reflect.DeepEqual(after.Sources, before.Sources) {
		t.Fatalf("local deployment/association changed: %+v", after)
	}
	if !slices.Contains(after.Settings.DefaultGlobalDirectories, target.ID) || !slices.Contains(after.Settings.DefaultProjectDirectories, "legacy-absolute") || len(after.Settings.CustomProjectDirectories) != 2 || after.Settings.Theme != "dark" {
		t.Fatalf("settings did not preserve local paths: %+v", after.Settings)
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "alpha" {
		t.Fatal("sync wrote a deployment")
	}
	// A remotely deleted skill/preset cleans local selection while retaining
	// deployment ownership, allowing the regular explicit apply to remove it.
	shared.Skills, shared.Presets = []Skill{}, []Preset{}
	if err = store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	deleted := state(t, store).Targets[0]
	if len(deleted.EnabledSkillIDs) != 0 || len(deleted.EnabledPresetIDs) != 0 || !deleted.Pending || !reflect.DeepEqual(deleted.Deployments, before.Targets[0].Deployments) {
		t.Fatalf("delete did not preserve pending deployment cleanup: %+v", deleted)
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "alpha" {
		t.Fatal("deletion deployed automatically")
	}
	snapshots, err := store.Snapshots()
	if err != nil || !slices.ContainsFunc(snapshots, func(info SnapshotInfo) bool { return info.Reason == "before applying cloud synchronization" }) {
		t.Fatalf("sync did not create a recoverable snapshot: %+v, %v", snapshots, err)
	}
}

func TestSharedValidationAndAtomicExpectedState(t *testing.T) {
	store, baseDir := newStore(t)
	skill := install(t, store, candidate(t, baseDir, "alpha", "alpha"))[0]
	before := ExportShared(state(t, store))
	incoming := cloneSharedForTest(before)
	incoming.Skills[0].ContentHash = strings.Repeat("a", 64)
	if err := store.ValidateShared(incoming); err == nil {
		t.Fatal("missing content object accepted")
	}
	if err := store.ApplyShared(incoming); err == nil || !reflect.DeepEqual(before, ExportShared(state(t, store))) {
		t.Fatal("failed object validation changed state")
	}
	if err := os.WriteFile(filepath.Join(store.Root(), "objects", skill.ContentHash, "SKILL.md"), []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateShared(before); err == nil {
		t.Fatal("damaged content object accepted")
	}
	// Restore the object to test the independent current-state guard.
	if err := os.WriteFile(filepath.Join(store.Root(), "objects", skill.ContentHash, "SKILL.md"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	savePreset(t, store, "concurrent local edit", skill)
	if err := store.ApplySharedIfCurrent(before, before); err == nil || len(state(t, store).Presets) != 1 {
		t.Fatal("network sync overwrote a concurrent local edit")
	}
}

func TestSharedContentUpdateIsPendingAndKeepsDeviceSelections(t *testing.T) {
	a, aBase := newStore(t)
	b, bBase := newStore(t)
	item := candidate(t, aBase, "alpha", "version one")
	skill := install(t, a, item)[0]
	shared := ExportShared(state(t, a))
	copySharedObjects(t, a, b, shared)
	if err := b.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	target, err := b.SaveTarget(Target{Name: "local deployment", Kind: "global", Path: filepath.Join(bBase, "target"), EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, b, target)
	before := state(t, b).Targets[0]
	if err = os.WriteFile(filepath.Join(item.Path, "SKILL.md"), []byte("version two"), 0644); err != nil {
		t.Fatal(err)
	}
	install(t, a, item)
	updated := ExportShared(state(t, a))
	copySharedObjects(t, a, b, updated)
	if err = b.ApplyShared(updated); err != nil {
		t.Fatal(err)
	}
	after := state(t, b).Targets[0]
	if !after.Pending || !slices.Equal(after.EnabledSkillIDs, []string{skill.ID}) || !reflect.DeepEqual(after.Deployments, before.Deployments) || contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "version one" {
		t.Fatalf("sync changed a deployment or lost device selection: %+v", after)
	}
	plan, err := b.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Update, []string{"alpha"}) {
		t.Fatalf("updated synchronized content is not ready for explicit apply: %+v, %v", plan, err)
	}
}

func TestSharedLocalTemplateCollisionCannotPublishADeletion(t *testing.T) {
	store, baseDir := newStore(t)
	current := state(t, store)
	current.Settings.CustomProjectDirectories = []ProjectDirectory{{ID: "same-id", Name: "legacy", Path: filepath.Join(baseDir, "legacy")}}
	if err := store.writeState(current); err != nil {
		t.Fatal(err)
	}
	incoming := ExportShared(current)
	incoming.Settings.CustomProjectDirectories = []ProjectDirectory{{ID: "same-id", Name: "portable", Path: ".portable/skills"}}
	if err := store.ValidateShared(incoming); err == nil {
		t.Fatal("local override would turn the portable template into a deletion on the next sync")
	}
	if err := store.ApplyShared(incoming); err == nil || state(t, store).Settings.CustomProjectDirectories[0].Path != current.Settings.CustomProjectDirectories[0].Path {
		t.Fatal("template collision replaced a device path")
	}
}

func TestSharedSettingsOptionalPreferencesAndValidation(t *testing.T) {
	store, _ := newStore(t)
	settings := state(t, store).Settings
	if settings.Theme != "" || settings.Locale != "" || settings.NavLayout != "" {
		t.Fatal("new store blocked WebView preference migration")
	}
	settings.Theme, settings.Locale, settings.NavLayout = "dark", "zh-TW", "top"
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings.Theme, settings.Locale, settings.NavLayout = "", "", ""
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if got := state(t, store).Settings; got.Theme != "dark" || got.Locale != "zh-TW" || got.NavLayout != "top" {
		t.Fatal("old client erased synced preferences")
	}
	for _, change := range []func(*Settings){func(s *Settings) { s.Theme = "other" }, func(s *Settings) { s.Locale = "other" }, func(s *Settings) { s.NavLayout = "other" }} {
		invalid := state(t, store).Settings
		change(&invalid)
		if err := store.SaveSettings(invalid); err == nil {
			t.Fatal("invalid UI preference accepted")
		}
	}
}

func TestSharedFreshDeviceAcceptsRemotePreferencesWithoutFalseConflicts(t *testing.T) {
	store, _ := newStore(t)
	local := ExportShared(state(t, store))
	// The WebView preference migration on a new device saves these defaults.
	local.Settings.Theme, local.Settings.Locale, local.Settings.NavLayout = "system", "system", "auto"
	remote := EmptyShared()
	remote.Settings.Theme, remote.Settings.Locale, remote.Settings.NavLayout = "dark", "en", "side"
	merged, conflicts, err := MergeShared(EmptyShared(), local, remote, nil)
	if err != nil || len(conflicts) != 0 || merged.Settings.Theme != "dark" || merged.Settings.Locale != "en" || merged.Settings.NavLayout != "side" {
		t.Fatalf("fresh defaults incorrectly conflicted with remote preferences: %+v, %+v, %v", merged.Settings, conflicts, err)
	}
	if original := state(t, store).Settings; original.Theme != "" || original.Locale != "" || original.NavLayout != "" {
		t.Fatal("canonical shared defaults modified the old local migration state")
	}
}
