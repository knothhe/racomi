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

func TestCustomProjectPathsResolvePersistAndCloneWithoutDeploying(t *testing.T) {
	store, base := newStore(t)
	root, home := filepath.Join(base, "repository"), filepath.Join(base, "home")
	for _, directory := range []string{root, home} {
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	absolute := filepath.Join(base, "absolute", "skills")
	custom := []string{"  .tool//skills/.  ", "../shared/skills", absolute, "~/custom/skills"}
	target, err := store.SaveTarget(Target{Name: "custom", Kind: "project", Path: root, DirectoryIDs: []string{"agents"}, CustomPaths: custom})
	if err != nil {
		t.Fatal(err)
	}
	expectedCustom := []string{filepath.Join(".tool", "skills"), filepath.Join("..", "shared", "skills"), absolute, filepath.Join(home, "custom", "skills")}
	expectedPaths := []string{filepath.Join(root, ".agents", "skills"), filepath.Join(root, ".tool", "skills"), filepath.Join(base, "shared", "skills"), absolute, filepath.Join(home, "custom", "skills")}
	if !slices.Equal(target.CustomPaths, expectedCustom) || !slices.Equal(target.Paths, expectedPaths) || target.ProjectRoot != root || target.Path != expectedPaths[0] {
		t.Fatalf("custom paths did not resolve against the project root: %+v", target)
	}
	for _, path := range target.Paths {
		missing(t, path)
	}
	custom[0] = "changed-input"
	target.CustomPaths[0] = "changed-returned-target"
	returned := state(t, store)
	if !slices.Equal(returned.Targets[0].CustomPaths, expectedCustom) {
		t.Fatal("saving retained mutable caller arrays")
	}
	copied := cloneState(returned)
	copied.Targets[0].CustomPaths[0] = "changed-clone"
	if !slices.Equal(returned.Targets[0].CustomPaths, expectedCustom) {
		t.Fatal("cloning shared custom path arrays")
	}
	returned.Targets[0].CustomPaths[0] = "changed-read"
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	persisted := state(t, reopened).Targets[0]
	if !slices.Equal(persisted.CustomPaths, expectedCustom) || !slices.Equal(persisted.Paths, expectedPaths) {
		t.Fatalf("custom paths were not persisted: %+v", persisted)
	}
	// Older metadata callers omit the new field; those saves retain custom paths.
	saved, err := reopened.SaveTarget(Target{ID: persisted.ID, Name: "renamed", Kind: "project", Path: root})
	if err != nil || !slices.Equal(saved.CustomPaths, expectedCustom) {
		t.Fatalf("legacy metadata save erased custom directories: %+v %v", saved, err)
	}
}

func TestCustomOnlyProjectAppliesAndRetiresDirectoriesInBothModes(t *testing.T) {
	for _, mode := range []string{DeploymentCopy, DeploymentSymlink} {
		t.Run(mode, func(t *testing.T) {
			store, base := newStore(t)
			skill := install(t, store, candidate(t, base, "alpha", "original"))[0]
			settings := state(t, store).Settings
			settings.DeploymentMode = mode
			if err := store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "repository")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			target, err := store.SaveTarget(Target{Name: "custom only", Kind: "project", Path: root, DirectoryIDs: []string{}, CustomPaths: []string{"private/skills", "../shared/skills"}, EnabledSkillIDs: []string{skill.ID}})
			if err != nil {
				t.Fatal(err)
			}
			if len(target.DirectoryIDs) != 0 || len(target.Paths) != 2 || target.Path != filepath.Join(root, "private", "skills") {
				t.Fatalf("custom-only project gained fixed directories: %+v", target)
			}
			plan, err := store.Plan(target.ID)
			if err != nil || len(plan.Directories) != 2 || !slices.Equal(plan.Add, []string{"alpha"}) {
				t.Fatalf("custom-only project preview failed: %+v %v", plan, err)
			}
			mustApply(t, store, target)
			legacySave, err := store.SaveTarget(Target{ID: target.ID, Name: "renamed custom", Kind: "project", Path: target.Path})
			if err != nil || legacySave.ProjectRoot != root || !slices.Equal(legacySave.CustomPaths, target.CustomPaths) {
				t.Fatalf("legacy primary-path metadata save lost custom project root: %+v %v", legacySave, err)
			}
			for _, path := range target.Paths {
				if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "original" {
					t.Fatal("custom directory did not receive the selected skill")
				}
				if mode == DeploymentSymlink {
					if linked, err := os.Readlink(filepath.Join(path, "alpha")); err != nil || linked != filepath.Join(store.Root(), "objects", skill.ContentHash) {
						t.Fatalf("custom directory did not receive a managed symlink: %s %v", linked, err)
					}
				}
			}
			retired := target.Paths[0]
			if err = os.Mkdir(filepath.Join(retired, "personal"), 0755); err != nil {
				t.Fatal(err)
			}
			target.CustomPaths = []string{"../shared/skills", "new/skills"}
			saved, err := store.SaveTarget(target)
			if err != nil || len(saved.Deployments) != 2 || !saved.Pending {
				t.Fatalf("retiring a custom directory lost ownership: %+v %v", saved, err)
			}
			plan, err = store.Plan(target.ID)
			if err != nil || len(plan.Directories) != 3 || !slices.Equal(plan.Remove, []string{"alpha"}) || !slices.Equal(plan.Add, []string{"alpha"}) {
				t.Fatalf("custom directory replacement was not staged as add/remove: %+v %v", plan, err)
			}
			mustApply(t, store, saved)
			missing(t, filepath.Join(retired, "alpha"))
			if info, err := os.Stat(filepath.Join(retired, "personal")); err != nil || !info.IsDir() {
				t.Fatal("retiring a custom directory removed unmanaged content")
			}
			current := state(t, store).Targets[0]
			if current.Pending || len(current.Deployments) != 2 || len(current.DirectoryIDs) != 0 {
				t.Fatalf("custom-only ownership did not settle: %+v", current)
			}
			for _, path := range saved.Paths {
				if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "original" {
					t.Fatal("a selected custom directory was lost")
				}
			}
		})
	}
}

func TestCustomProjectPathsRejectOverlapDataDirectoryAndSymlinkParents(t *testing.T) {
	store, base := newStore(t)
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, custom := range [][]string{{""}, {"  "}, {".agents/skills"}, {"foo", "foo/bar"}, {"../outside/skills", "../outside/skills/."}, {store.Root()}, {base}, {"/"}, {"linked/skills"}} {
		if _, err := store.SaveTarget(Target{Name: "invalid", Kind: "project", Path: root, DirectoryIDs: []string{"agents"}, CustomPaths: custom}); err == nil {
			t.Fatalf("accepted unsafe custom directories: %v", custom)
		}
	}
	if len(state(t, store).Targets) != 0 {
		t.Fatal("rejected custom directories partially registered a project")
	}
	// Even an otherwise valid custom path cannot take ownership from another target.
	if _, err := store.SaveTarget(Target{Name: "existing", Kind: "global", Path: filepath.Join(base, "shared", "skills")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTarget(Target{Name: "overlapping", Kind: "project", Path: root, DirectoryIDs: []string{}, CustomPaths: []string{"../shared/skills"}}); err == nil {
		t.Fatal("a custom directory overlapped another saved target")
	}
}

func TestRetiredCustomDirectoryKeepsModifiedContentAndBlocksTheWholeApply(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "original"))[0]
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(Target{Name: "custom", Kind: "project", Path: root, DirectoryIDs: []string{}, CustomPaths: []string{"first/skills", "second/skills"}, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	retired := target.Paths[0]
	if err = os.WriteFile(filepath.Join(retired, "alpha", "SKILL.md"), []byte("local edit"), 0644); err != nil {
		t.Fatal(err)
	}
	target.CustomPaths = []string{"second/skills", "third/skills"}
	if _, err = store.SaveTarget(target); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	plan, err := store.Plan(target.ID)
	if err != nil || len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], retired) {
		t.Fatalf("retired custom directory lost conflict protection: %+v %v", plan, err)
	}
	if err = store.Apply(target.ID); err == nil {
		t.Fatal("a locally edited retired directory was removed")
	}
	if !reflect.DeepEqual(before, state(t, store)) || contents(t, filepath.Join(retired, "alpha", "SKILL.md")) != "local edit" {
		t.Fatal("a rejected custom application changed files or ownership")
	}
	missing(t, filepath.Join(root, "third", "skills"))
}

func TestLegacyProjectsWithoutCustomPathsRemainReadable(t *testing.T) {
	store, base := newStore(t)
	target := projectWithDirectories(t, store, base, []string{"agents", "codex"})
	index := filepath.Join(store.Root(), "state.json")
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, entry := range document["targets"].([]any) {
		delete(entry.(map[string]any), "customPaths")
	}
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(index, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	restored := state(t, reopened).Targets[0]
	if restored.CustomPaths == nil || len(restored.CustomPaths) != 0 || !slices.Equal(restored.Paths, target.Paths) || !slices.Equal(restored.DirectoryIDs, target.DirectoryIDs) {
		t.Fatalf("legacy target changed its fixed directories: %+v", restored)
	}
}

func TestCustomOnlyProjectMovesRelativePathsAndRetainsDeployedOwnership(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "original"))[0]
	first, second := filepath.Join(base, "first"), filepath.Join(base, "second")
	for _, root := range []string{first, second} {
		if err := os.Mkdir(root, 0755); err != nil {
			t.Fatal(err)
		}
	}
	target, err := store.SaveTarget(Target{Name: "move", Kind: "project", Path: first, DirectoryIDs: []string{}, CustomPaths: []string{"custom/skills"}, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := store.SaveTarget(Target{ID: target.ID, Name: target.Name, Kind: "project", Path: second})
	if err != nil || moved.Path != filepath.Join(second, "custom", "skills") || !slices.Equal(moved.CustomPaths, []string{"custom/skills"}) || len(moved.DirectoryIDs) != 0 {
		t.Fatalf("relative custom path did not follow a new project root: %+v %v", moved, err)
	}
	mustApply(t, store, moved)
	relocated, err := store.SaveTarget(Target{ID: target.ID, Name: target.Name, Kind: "project", Path: first})
	if err != nil || relocated.Path != filepath.Join(first, "custom", "skills") || !relocated.Pending || len(relocated.Deployments) != 1 || deploymentPath(relocated, relocated.Deployments[0]) != moved.Path {
		t.Fatalf("moving a deployed custom-only project lost its previous directory ownership: %+v %v", relocated, err)
	}
	missing(t, relocated.Path)
}
