package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"racomi/internal/testutil"
)

func testDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	base := testDir(t)
	store, err := Open(filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return store, base
}
func candidate(t *testing.T, base, name, body string) Candidate {
	t.Helper()
	path := filepath.Join(base, "source-"+name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return Candidate{Path: path, Skill: Skill{Name: name, Source: "https://example.test/repo", Ref: "main", Subpath: name}}
}
func install(t *testing.T, store *Store, candidates ...Candidate) []Skill {
	t.Helper()
	skills, err := store.Install(candidates)
	if err != nil {
		t.Fatal(err)
	}
	return skills
}
func savePreset(t *testing.T, store *Store, name string, skills ...Skill) Preset {
	t.Helper()
	members := []string{}
	for _, skill := range skills {
		members = append(members, skill.ID)
	}
	preset, err := store.SavePreset(Preset{Name: name, SkillIDs: members})
	if err != nil {
		t.Fatal(err)
	}
	return preset
}
func saveTarget(t *testing.T, store *Store, base string, presets ...Preset) Target {
	t.Helper()
	enabled := []string{}
	for _, preset := range presets {
		enabled = append(enabled, preset.ID)
	}
	target, err := store.SaveTarget(Target{Name: "test", Kind: "global", Path: filepath.Join(base, "target"), EnabledPresetIDs: enabled})
	if err != nil {
		t.Fatal(err)
	}
	return target
}
func mustApply(t *testing.T, store *Store, target Target) {
	t.Helper()
	if err := store.Apply(target.ID); err != nil {
		t.Fatal(err)
	}
}
func contents(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func state(t *testing.T, store *Store) State {
	t.Helper()
	s, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func missing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected missing %s, got %v", path, err)
	}
}

func TestPresetUnionAndOwnership(t *testing.T) {
	store, base := newStore(t)
	skills := install(t, store, candidate(t, base, "alpha", "alpha"), candidate(t, base, "shared", "shared"), candidate(t, base, "gamma", "gamma"))
	first := savePreset(t, store, "first", skills[0], skills[1])
	second := savePreset(t, store, "second", skills[1], skills[2])
	target := saveTarget(t, store, base, first, second)
	missing(t, target.Path)
	mustApply(t, store, target)
	if err := store.DeleteTarget(target.ID); err == nil {
		t.Fatal("deleting a deployed target must fail")
	}
	if err := os.Mkdir(filepath.Join(target.Path, "personal"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target.Path, "personal", "keep"), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTargetPresets(target.ID, []string{second.ID}); err != nil {
		t.Fatal(err)
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "alpha" {
		t.Fatal("setting desired presets changed files")
	}
	mustApply(t, store, target)
	missing(t, filepath.Join(target.Path, "alpha"))
	if contents(t, filepath.Join(target.Path, "shared", "SKILL.md")) != "shared" {
		t.Fatal("shared skill was removed")
	}
	if err := store.RemoveSkill(skills[1].ID); err == nil {
		t.Fatal("removing preset member should fail")
	}
	if err := store.SetTargetPresets(target.ID, nil); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	missing(t, filepath.Join(target.Path, "shared"))
	if contents(t, filepath.Join(target.Path, "personal", "keep")) != "mine" {
		t.Fatal("unmanaged content was altered")
	}
	if err := store.DeleteTarget(target.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRequiresExplicitApply(t *testing.T) {
	store, base := newStore(t)
	source := candidate(t, base, "alpha", "version one")
	original := install(t, store, source)[0]
	preset := savePreset(t, store, "default", original)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	if err := os.WriteFile(filepath.Join(source.Path, "SKILL.md"), []byte("version two"), 0644); err != nil {
		t.Fatal(err)
	}
	updated := install(t, store, source)[0]
	if original.ID != updated.ID || original.ContentHash == updated.ContentHash || original.InstalledAt != updated.InstalledAt {
		t.Fatal("incorrect update identity or metadata")
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "version one" || !state(t, store).Targets[0].Pending {
		t.Fatal("update was not left pending")
	}
	plan, err := store.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Update, []string{"alpha"}) {
		t.Fatalf("bad plan: %+v, %v", plan, err)
	}
	mustApply(t, store, target)
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "version two" || state(t, store).Targets[0].Pending {
		t.Fatal("manual apply did not deploy update")
	}
	if _, err := os.Stat(filepath.Join(store.Root(), "objects", original.ContentHash)); err != nil {
		t.Fatal("old version was lost", err)
	}
}

func TestConflictsPreventEveryTargetMutation(t *testing.T) {
	store, base := newStore(t)
	a := candidate(t, base, "alpha", "old")
	skill := install(t, store, a)[0]
	preset := savePreset(t, store, "default", skill)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	if err := os.WriteFile(filepath.Join(a.Path, "SKILL.md"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	updated := install(t, store, a, candidate(t, base, "beta", "beta"))
	preset.SkillIDs = []string{updated[0].ID, updated[1].ID}
	if _, err := store.SavePreset(preset); err != nil {
		t.Fatal(err)
	}
	deployed := filepath.Join(target.Path, "alpha", "SKILL.md")
	if err := os.WriteFile(deployed, []byte("my edit"), 0644); err != nil {
		t.Fatal(err)
	}
	before := state(t, store).Targets[0].Deployments
	if err := store.Apply(target.ID); err == nil {
		t.Fatal("edited deployment should conflict")
	}
	missing(t, filepath.Join(target.Path, "beta"))
	if contents(t, deployed) != "my edit" || !slices.Equal(before, state(t, store).Targets[0].Deployments) {
		t.Fatal("conflict had side effects")
	}
	if err := store.SetTargetPresets(target.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(target.ID); err == nil {
		t.Fatal("removing edited deployment should conflict")
	}
	if contents(t, deployed) != "my edit" {
		t.Fatal("local edit was deleted")
	}
	if err := os.WriteFile(deployed, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTargetPresets(target.ID, []string{preset.ID}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(target.Path, "beta"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target.Path, "beta", "unmanaged"), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(target.ID); err == nil {
		t.Fatal("unmanaged same-name directory should conflict")
	}
	if contents(t, deployed) != "old" || contents(t, filepath.Join(target.Path, "beta", "unmanaged")) != "mine" {
		t.Fatal("unmanaged conflict changed content")
	}
}

func TestNameCollisionIsPreflightConflict(t *testing.T) {
	store, base := newStore(t)
	first := candidate(t, base, "alpha", "one")
	second := candidate(t, base, "beta", "two")
	second.Skill.Name = "alpha"
	skills := install(t, store, first, second)
	target := saveTarget(t, store, base, savePreset(t, store, "collision", skills...))
	plan, err := store.Plan(target.ID)
	if err != nil || len(plan.Conflicts) == 0 {
		t.Fatalf("collision wasn't detected: %+v %v", plan, err)
	}
	if err := store.Apply(target.ID); err == nil {
		t.Fatal("colliding apply should fail")
	}
	missing(t, target.Path)
}

func TestPersistenceSnapshotsAndRestore(t *testing.T) {
	store, base := newStore(t)
	source := candidate(t, base, "alpha", "one")
	original := install(t, store, source)[0]
	preset := savePreset(t, store, "original", original)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	snapshot, err := store.Snapshot("known good")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.Path, "SKILL.md"), []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	install(t, store, source)
	if err := store.DeletePreset(preset.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSkill(original.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreSnapshot(snapshot.ID); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	s := state(t, reopened)
	if len(s.Skills) != 1 || s.Skills[0].ContentHash != original.ContentHash || len(s.Presets) != 1 {
		t.Fatal("snapshot did not restore store")
	}
	if len(s.Targets) != 1 || len(s.Targets[0].EnabledPresetIDs) != 0 || len(s.Targets[0].Deployments) != 1 || !s.Targets[0].Pending {
		t.Fatal("restore must keep target ownership but clear desired presets")
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "one" {
		t.Fatal("restore wrote target files")
	}
	snapshots, err := store.Snapshots()
	if err != nil || len(snapshots) < 5 {
		t.Fatalf("automatic snapshots missing: %d %v", len(snapshots), err)
	}
	if err := store.SetTargetPresets(target.ID, []string{preset.ID}); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
}

func TestContentAddressingAndExecutableBits(t *testing.T) {
	store, base := newStore(t)
	first := candidate(t, base, "alpha", "same")
	second := candidate(t, base, "beta", "same")
	skills := install(t, store, first, second)
	if skills[0].ContentHash != skills[1].ContentHash {
		t.Fatal("identical content wasn't deduplicated")
	}
	entries, err := os.ReadDir(filepath.Join(store.Root(), "objects"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one object: %v %v", entries, err)
	}
	if err := os.Chmod(filepath.Join(first.Path, "SKILL.md"), 0755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs do not expose Unix executable permission bits")
	}
	hash, err := HashDir(first.Path)
	if err != nil || hash == skills[0].ContentHash {
		t.Fatal("executable bits did not affect content hash", err)
	}
	dst := filepath.Join(base, "copy")
	if err := CopyDir(first.Path, dst); err != nil {
		t.Fatal(err)
	}
	copied, err := HashDir(dst)
	if err != nil || copied != hash {
		t.Fatal("copy did not preserve executable bits", err)
	}
	if err := CopyDir(first.Path, dst); err == nil {
		t.Fatal("copy must not merge with existing directories")
	}
}

func TestPathAndSymlinkProtection(t *testing.T) {
	store, base := newStore(t)
	for _, path := range []string{store.Root(), filepath.Join(store.Root(), "objects", "escape"), base, "/"} {
		if _, err := store.SaveTarget(Target{Name: "dangerous", Kind: "global", Path: path}); err == nil {
			t.Errorf("accepted dangerous target %s", path)
		}
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if _, err := store.SaveTarget(Target{Name: "case alias", Kind: "global", Path: strings.ToUpper(store.Root())}); err == nil {
			t.Fatal("case variant escaped application data protection")
		}
	}
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "linked")
	if err := testutil.Symlink(t, outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTarget(Target{Name: "linked", Kind: "global", Path: filepath.Join(link, "skills")}); err == nil {
		t.Fatal("accepted symlink parent")
	}
	source := candidate(t, base, "alpha", "alpha")
	if err := testutil.Symlink(t, filepath.Join(outside, "secret"), filepath.Join(source.Path, "unsafe")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Install([]Candidate{source}); err == nil {
		t.Fatal("accepted symlink content")
	}
	if _, err := HashDir(source.Path); err == nil {
		t.Fatal("hash accepted symlink content")
	}
	if err := os.Remove(filepath.Join(source.Path, "unsafe")); err != nil {
		t.Fatal(err)
	}
	skill := install(t, store, source)[0]
	target := saveTarget(t, store, base, savePreset(t, store, "default", skill))
	mustApply(t, store, target)
	if err := os.RemoveAll(filepath.Join(target.Path, "alpha")); err != nil {
		t.Fatal(err)
	}
	if err := testutil.Symlink(t, outside, filepath.Join(target.Path, "alpha")); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(target.ID); err == nil {
		t.Fatal("apply accepted symlink deployment")
	}
	missing(t, filepath.Join(outside, "SKILL.md"))
	if err := os.RemoveAll(target.Path); err != nil {
		t.Fatal(err)
	}
	if err := testutil.Symlink(t, outside, target.Path); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(target.ID); err == nil {
		t.Fatal("apply accepted replaced target symlink")
	}
	missing(t, filepath.Join(outside, "alpha"))
}

func TestProjectTargetsAndNameValidation(t *testing.T) {
	store, base := newStore(t)
	for _, emptyPath := range []string{"", "  "} {
		if _, err := store.SaveTarget(Target{Name: "missing repository", Kind: "project", Path: emptyPath}); err == nil {
			t.Fatalf("empty project path %q silently selected the process working directory", emptyPath)
		}
	}
	if len(state(t, store).Targets) != 0 {
		t.Fatal("rejecting an empty project path saved a target")
	}
	repo := filepath.Join(base, "repository")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(Target{Name: "repository", Kind: "project", Path: repo})
	if err != nil || target.Path != filepath.Join(repo, ".agents", "skills") {
		t.Fatalf("bad project target %+v %v", target, err)
	}
	for _, name := range []string{"../escape", "/absolute", "a/b", "a\\b", ".", "..", "CON", "a."} {
		c := candidate(t, base, "source", "content")
		c.Skill.Name = name
		if _, err := store.Install([]Candidate{c}); err == nil {
			t.Errorf("accepted unsafe name %q", name)
		}
	}
}

func TestDeepCopiesConcurrentHandlesAndCorruptState(t *testing.T) {
	store, base := newStore(t)
	s := state(t, store)
	if s.Skills == nil || s.Presets == nil || s.Targets == nil {
		t.Fatal("state arrays must be non-nil")
	}
	skill := install(t, store, candidate(t, base, "alpha", "a"))[0]
	preset := savePreset(t, store, "first", skill)
	target := saveTarget(t, store, base, preset)
	s = state(t, store)
	s.Presets[0].SkillIDs[0] = "changed"
	s.Targets[0].EnabledPresetIDs[0] = "changed"
	s.Skills[0].Name = "changed"
	if state(t, store).Skills[0].Name != "alpha" {
		t.Fatal("state leaked mutable arrays")
	}
	second, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for _, handle := range []*Store{store, second} {
		group.Add(1)
		go func(h *Store) {
			defer group.Done()
			_, saveErr := h.SavePreset(Preset{Name: "concurrent"})
			errors <- saveErr
		}(handle)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(state(t, store).Presets) != 3 {
		t.Fatal("concurrent update was lost")
	}
	if err := store.SetTargetPresets(target.ID, []string{"missing"}); err == nil {
		t.Fatal("unknown preset was accepted")
	}
	bad := []byte("{broken")
	if err := os.WriteFile(filepath.Join(store.Root(), "state.json"), bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(store.Root()); err == nil {
		t.Fatal("corrupt state was silently recreated")
	}
	if _, err := store.SavePreset(Preset{Name: "overwrite"}); err == nil {
		t.Fatal("corrupt state was overwritten")
	}
	if contents(t, filepath.Join(store.Root(), "state.json")) != string(bad) {
		t.Fatal("corrupt state changed")
	}
}

func TestMissingIndexAndRestoreValidation(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "a"))[0]
	s := state(t, store)
	s.Skills[0].ContentHash = strings.Repeat("0", 64)
	if err := store.Restore(s); err == nil {
		t.Fatal("restore accepted missing object")
	}
	if state(t, store).Skills[0].ContentHash != skill.ContentHash {
		t.Fatal("failed restore changed store")
	}
	s = state(t, store)
	s.Targets = []Target{{ID: "foreign", Name: "foreign", Kind: "global", Path: "/some/other/machine"}}
	if err := store.Restore(s); err != nil {
		t.Fatal(err)
	}
	if len(state(t, store).Targets) != 0 {
		t.Fatal("restore imported foreign target paths")
	}
	if err := os.Remove(filepath.Join(store.Root(), "state.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(store.Root()); err == nil {
		t.Fatal("missing index with existing data was silently recreated")
	}
}

func TestJSONIndexIsCompleteAndPrivate(t *testing.T) {
	store, _ := newStore(t)
	data, err := os.ReadFile(filepath.Join(store.Root(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil || s.Schema != schemaVersion {
		t.Fatal("bad index", err)
	}
	info, err := os.Stat(filepath.Join(store.Root(), "state.json"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("index should be private", err)
	}
}

func TestMacOSSystemAliasAndUserSymlinkRemainDistinct(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system aliases")
	}
	store, base := newStore(t)
	if !strings.HasPrefix(base, "/private/var/") && !strings.HasPrefix(base, "/private/tmp/") {
		t.Skip("test directory has no system alias")
	}
	alias := strings.TrimPrefix(base, "/private")
	source := candidate(t, base, "alpha", "aliased")
	source.Path = strings.TrimPrefix(source.Path, "/private")
	skill := install(t, store, source)[0]
	preset := savePreset(t, store, "default", skill)
	target, err := store.SaveTarget(Target{Name: "aliased target", Kind: "global", Path: filepath.Join(alias, "target"), EnabledPresetIDs: []string{preset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != filepath.Join(base, "target") {
		t.Fatalf("alias wasn't canonicalized: %s", target.Path)
	}
	mustApply(t, store, target)
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "aliased" {
		t.Fatal("aliased application failed")
	}
	link := filepath.Join(base, "custom-link")
	if err := testutil.Symlink(t, target.Path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTarget(Target{Name: "user link", Kind: "global", Path: link}); err == nil {
		t.Fatal("user symlink was mistaken for a system alias")
	}
}

func TestApplyRollsBackWhenIndexCannotCommit(t *testing.T) {
	store, base := newStore(t)
	a := candidate(t, base, "alpha", "old")
	original := install(t, store, a)[0]
	preset := savePreset(t, store, "default", original)
	target := saveTarget(t, store, base, preset)
	mustApply(t, store, target)
	if err := os.WriteFile(filepath.Join(a.Path, "SKILL.md"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	// Give the watcher a deterministic staging interval without introducing a
	// production test hook: staging hashes and copies this ordinary skill.
	large := candidate(t, base, "beta", strings.Repeat("b", 16<<20))
	updated := install(t, store, a, large)
	preset.SkillIDs = []string{updated[0].ID, updated[1].ID}
	if _, err := store.SavePreset(preset); err != nil {
		t.Fatal(err)
	}
	statePath, savedPath := filepath.Join(store.Root(), "state.json"), filepath.Join(store.Root(), "saved-index.json")
	observed := make(chan error, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case <-stop:
				return
			case <-deadline.C:
				observed <- os.ErrDeadlineExceeded
				return
			default:
			}
			entries, err := os.ReadDir(base)
			if err != nil {
				observed <- err
				return
			}
			for _, entry := range entries {
				if !strings.HasPrefix(entry.Name(), ".racomi-apply-") {
					continue
				}
				if err := os.Rename(statePath, savedPath); err != nil {
					observed <- err
					return
				}
				observed <- os.Mkdir(statePath, 0700)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	applyErr := store.Apply(target.ID)
	if err := <-observed; err != nil {
		t.Fatal("failure injector:", err)
	}
	if err := os.RemoveAll(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(savedPath, statePath); err != nil {
		t.Fatal(err)
	}
	if applyErr == nil {
		t.Fatal("application should fail when its ownership index cannot commit")
	}
	if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "old" {
		t.Fatal("old skill wasn't rolled back")
	}
	missing(t, filepath.Join(target.Path, "beta"))
	if state(t, store).Targets[0].Deployments[0].ContentHash != original.ContentHash {
		t.Fatal("failed transaction committed ownership")
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".racomi-apply-") {
			t.Fatal("successful rollback left staging data")
		}
	}
}
