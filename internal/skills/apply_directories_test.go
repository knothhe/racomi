package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func projectWithDirectories(t *testing.T, store *Store, base string, directories []string, skills ...Skill) Target {
	t.Helper()
	root := filepath.Join(base, "repository")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, skill := range skills {
		ids = append(ids, skill.ID)
	}
	target, err := store.SaveTarget(Target{Name: "project", Kind: "project", Path: root, DirectoryIDs: directories, EnabledSkillIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestProjectAppliesAllDirectoriesAndSafelyRetiresDeselectedDirectories(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "original"))[0]
	target := projectWithDirectories(t, store, base, []string{"agents", "claude", "codex", "cursor"}, skill)
	if target.ProjectRoot != filepath.Join(base, "repository") || len(target.Paths) != 4 || target.Path != target.Paths[0] {
		t.Fatalf("incorrect project directories: %+v", target)
	}
	plan, err := store.Plan(target.ID)
	if err != nil || len(plan.Directories) != 4 || !slices.Equal(plan.Add, []string{"alpha"}) {
		t.Fatalf("incorrect multi-directory plan: %+v %v", plan, err)
	}
	for _, path := range target.Paths {
		missing(t, path)
	}
	mustApply(t, store, target)
	for _, path := range target.Paths {
		if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "original" {
			t.Fatal("directory did not receive selected skill")
		}
	}
	if got := state(t, store).Targets[0]; got.Pending || len(got.Deployments) != 4 {
		t.Fatalf("all directories must share committed ownership: %+v", got)
	}
	// Saving directory selections must retain ownership, even when a managed
	// directory is removed from the desired list. Local unrelated content stays.
	personal := filepath.Join(target.Paths[1], "personal")
	if err = os.Mkdir(personal, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(personal, "keep"), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}
	selected, err := store.SaveTarget(Target{ID: target.ID, Name: target.Name, Kind: "project", Path: target.ProjectRoot, DirectoryIDs: []string{"agents", "codex"}, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Deployments) != 4 || !selected.Pending {
		t.Fatalf("saving directories lost ownership: %+v", selected)
	}
	plan, err = store.Plan(target.ID)
	if err != nil || len(plan.Directories) != 4 || !slices.Equal(plan.Remove, []string{"alpha"}) {
		t.Fatalf("retired directories are not included in preview: %+v %v", plan, err)
	}
	mustApply(t, store, selected)
	missing(t, filepath.Join(target.Paths[1], "alpha"))
	missing(t, filepath.Join(target.Paths[3], "alpha"))
	if contents(t, filepath.Join(personal, "keep")) != "mine" {
		t.Fatal("retiring a directory removed unmanaged content")
	}
	for _, path := range selected.Paths {
		if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "original" {
			t.Fatal("retirement changed a selected directory")
		}
	}
	if got := state(t, store).Targets[0]; got.Pending || len(got.Deployments) != 2 {
		t.Fatalf("retired ownership was retained: %+v", got)
	}
}

func TestConflictInOneDirectoryPreventsEveryDirectoryFromChanging(t *testing.T) {
	store, base := newStore(t)
	source := candidate(t, base, "alpha", "original")
	skill := install(t, store, source)[0]
	target := projectWithDirectories(t, store, base, []string{"agents", "claude", "codex"}, skill)
	mustApply(t, store, target)
	if err := os.WriteFile(filepath.Join(source.Path, "SKILL.md"), []byte("updated"), 0644); err != nil {
		t.Fatal(err)
	}
	install(t, store, source)
	if err := os.WriteFile(filepath.Join(target.Paths[2], "alpha", "SKILL.md"), []byte("personal edit"), 0644); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	plan, err := store.Plan(target.ID)
	if err != nil || len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], target.Paths[2]) {
		t.Fatalf("conflict must identify its directory: %+v %v", plan, err)
	}
	if err = store.Apply(target.ID); err == nil {
		t.Fatal("a conflict in one directory must prevent every write")
	}
	if !reflect.DeepEqual(before, state(t, store)) {
		t.Fatal("conflicted application changed ownership")
	}
	for _, path := range target.Paths[:2] {
		if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "original" {
			t.Fatal("another directory changed despite a conflict")
		}
	}
}

func TestSymlinkUpdatesModeConversionsAndRemoval(t *testing.T) {
	store, base := newStore(t)
	source := candidate(t, base, "alpha", "one")
	skill := install(t, store, source)[0]
	settings := state(t, store).Settings
	settings.DeploymentMode = DeploymentSymlink
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	target := projectWithDirectories(t, store, base, []string{"claude", "codex"}, skill)
	mustApply(t, store, target)
	for _, path := range target.Paths {
		destination, err := os.Readlink(filepath.Join(path, "alpha"))
		if err != nil || destination != filepath.Join(store.Root(), "objects", skill.ContentHash) {
			t.Fatalf("skill must link to its immutable object: %s %v", destination, err)
		}
	}
	if err := os.WriteFile(filepath.Join(source.Path, "SKILL.md"), []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	updated := install(t, store, source)[0]
	plan, err := store.Plan(target.ID)
	if err != nil || !slices.Equal(plan.Update, []string{"alpha"}) {
		t.Fatalf("linked skill update not planned: %+v %v", plan, err)
	}
	mustApply(t, store, target)
	for _, path := range target.Paths {
		link, err := os.Readlink(filepath.Join(path, "alpha"))
		if err != nil || link != filepath.Join(store.Root(), "objects", updated.ContentHash) || contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "two" {
			t.Fatalf("link was not updated: %s %v", link, err)
		}
	}
	settings.DeploymentMode = DeploymentCopy
	if err = store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if !state(t, store).Targets[0].Pending {
		t.Fatal("changing deployment mode must mark existing targets pending")
	}
	mustApply(t, store, target)
	for _, path := range target.Paths {
		info, err := os.Lstat(filepath.Join(path, "alpha"))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("symlink did not convert to a regular directory: %v", err)
		}
	}
	settings.DeploymentMode = DeploymentSymlink
	if err = store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	if err = store.SetTargetSelection(target.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, target)
	for _, path := range target.Paths {
		missing(t, filepath.Join(path, "alpha"))
	}
	if contents(t, filepath.Join(store.Root(), "objects", updated.ContentHash, "SKILL.md")) != "two" {
		t.Fatal("removing a deployment deleted the linked store object")
	}
}

func TestChangedManagedSymlinkCannotBeUpdatedRemovedOrAdopted(t *testing.T) {
	for _, replacement := range []string{"foreign-link", "copied-directory", "relative-link"} {
		t.Run(replacement, func(t *testing.T) {
			store, base := newStore(t)
			skill := install(t, store, candidate(t, base, "alpha", "original"))[0]
			settings := state(t, store).Settings
			settings.DeploymentMode = DeploymentSymlink
			if err := store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			target := saveTarget(t, store, base)
			if err := store.SetTargetSelection(target.ID, []string{skill.ID}, nil); err != nil {
				t.Fatal(err)
			}
			mustApply(t, store, target)
			deployed := filepath.Join(target.Path, "alpha")
			if err := os.Remove(deployed); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "personal")
			if err := os.Mkdir(outside, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("mine"), 0644); err != nil {
				t.Fatal(err)
			}
			switch replacement {
			case "foreign-link":
				if err := os.Symlink(outside, deployed); err != nil {
					t.Fatal(err)
				}
			case "copied-directory":
				if err := CopyDir(filepath.Join(store.Root(), "objects", skill.ContentHash), deployed); err != nil {
					t.Fatal(err)
				}
			case "relative-link":
				relative, err := filepath.Rel(target.Path, filepath.Join(store.Root(), "objects", skill.ContentHash))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(relative, deployed); err != nil {
					t.Fatal(err)
				}
			}
			for _, clear := range []bool{false, true} {
				if clear {
					if err := store.SetTargetSelection(target.ID, nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				plan, err := store.Plan(target.ID)
				if err != nil || len(plan.Conflicts) == 0 {
					t.Fatalf("tampered link was accepted: %+v %v", plan, err)
				}
				if err = store.Apply(target.ID); err == nil {
					t.Fatal("tampered link was overwritten or removed")
				}
				if _, err = os.Lstat(deployed); err != nil {
					t.Fatal("tampered deployment was removed")
				}
			}
			if contents(t, filepath.Join(outside, "keep")) != "mine" {
				t.Fatal("a foreign link was followed")
			}
		})
	}
}

func TestSettingsPersistenceDefaultsAndLegacyDeploymentCompatibility(t *testing.T) {
	store, base := newStore(t)
	source := candidate(t, base, "alpha", "original")
	skill := install(t, store, source)[0]
	target := projectWithDirectories(t, store, base, []string{"agents"}, skill)
	mustApply(t, store, target)
	indexPath := filepath.Join(store.Root(), "state.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	delete(document, "settings")
	for _, raw := range document["targets"].([]any) {
		saved := raw.(map[string]any)
		for _, field := range []string{"paths", "directoryIds", "projectRoot"} {
			delete(saved, field)
		}
		for _, deployment := range saved["deployments"].([]any) {
			delete(deployment.(map[string]any), "path")
			delete(deployment.(map[string]any), "mode")
		}
	}
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(indexPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	legacy := state(t, reopened)
	if !reflect.DeepEqual(legacy.Settings, defaultSettings()) || !slices.Equal(legacy.Targets[0].Paths, []string{target.Path}) || legacy.Targets[0].ProjectRoot != target.ProjectRoot || legacy.Targets[0].Pending || legacy.Targets[0].Deployments[0].Mode != DeploymentCopy {
		t.Fatalf("legacy state changed meaning: %+v", legacy)
	}
	plan, err := reopened.Plan(target.ID)
	if err != nil || len(plan.Add)+len(plan.Update)+len(plan.Remove)+len(plan.Conflicts) != 0 {
		t.Fatalf("legacy copy ownership was not recognized: %+v %v", plan, err)
	}
	settings := Settings{DeploymentMode: DeploymentSymlink, DefaultProjectDirectories: []string{"claude", "codex"}, DefaultGlobalDirectories: []string{"claude-user", "codex-legacy"}}
	if err = reopened.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	reopened, err = Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state(t, reopened).Settings, normalizeSettings(settings)) {
		t.Fatal("settings were not persisted")
	}
	newRoot := filepath.Join(base, "other-repository")
	if err = os.Mkdir(newRoot, 0755); err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.SaveTarget(Target{Name: "defaults", Kind: "project", Path: newRoot})
	if err != nil || !slices.Equal(saved.DirectoryIDs, settings.DefaultProjectDirectories) {
		t.Fatalf("new project ignored default directories: %+v %v", saved, err)
	}
	before := state(t, reopened)
	for _, invalid := range []Settings{{DeploymentMode: "hardlink", DefaultProjectDirectories: []string{"agents"}, DefaultGlobalDirectories: []string{"codex-user"}}, {DeploymentMode: DeploymentCopy, DefaultProjectDirectories: []string{"other"}, DefaultGlobalDirectories: []string{"codex-user"}}, {DeploymentMode: DeploymentCopy, DefaultProjectDirectories: []string{}, DefaultGlobalDirectories: []string{"codex-user"}}} {
		if err = reopened.SaveSettings(invalid); err == nil {
			t.Fatal("invalid settings were accepted")
		}
	}
	if !reflect.DeepEqual(before, state(t, reopened)) {
		t.Fatal("invalid settings partially changed the index")
	}
}

func TestMultiDirectoryApplyRollsBackEveryDestinationWhenIndexCannotCommit(t *testing.T) {
	store, base := newStore(t)
	source := candidate(t, base, "alpha", "original")
	original := install(t, store, source)[0]
	target := projectWithDirectories(t, store, base, []string{"agents", "claude", "codex"}, original)
	mustApply(t, store, target)
	if err := os.WriteFile(filepath.Join(source.Path, "SKILL.md"), []byte("updated"), 0644); err != nil {
		t.Fatal(err)
	}
	large := candidate(t, base, "beta", strings.Repeat("x", 8<<20))
	updated := install(t, store, source, large)
	if err := store.SetTargetSelection(target.ID, []string{updated[0].ID, updated[1].ID}, nil); err != nil {
		t.Fatal(err)
	}
	indexPath, savedPath := filepath.Join(store.Root(), "state.json"), filepath.Join(store.Root(), "saved-index.json")
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
			for _, path := range target.Paths {
				entries, err := os.ReadDir(filepath.Dir(path))
				if err != nil {
					observed <- err
					return
				}
				for _, entry := range entries {
					if !strings.HasPrefix(entry.Name(), ".racomi-apply-") {
						continue
					}
					if err = os.Rename(indexPath, savedPath); err != nil {
						observed <- err
						return
					}
					observed <- os.Mkdir(indexPath, 0700)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	applyErr := store.Apply(target.ID)
	if err := <-observed; err != nil {
		t.Fatal("failure injector:", err)
	}
	if err := os.RemoveAll(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(savedPath, indexPath); err != nil {
		t.Fatal(err)
	}
	if applyErr == nil {
		t.Fatal("index failure must fail the whole transaction")
	}
	for _, path := range target.Paths {
		if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "original" {
			t.Fatal("a completed destination was not rolled back")
		}
		missing(t, filepath.Join(path, "beta"))
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".racomi-apply-") {
				t.Fatal("successful rollback left staging data")
			}
		}
	}
	for _, deployment := range state(t, store).Targets[0].Deployments {
		if deployment.ContentHash != original.ContentHash {
			t.Fatal("failed application changed ownership")
		}
	}
}

func TestChangedPlanDuringStagingAbortsBeforeCommittingAnyDirectory(t *testing.T) {
	store, base := newStore(t)
	skills := install(t, store, candidate(t, base, "alpha", "original"), candidate(t, base, "beta", strings.Repeat("x", 16<<20)))
	target := projectWithDirectories(t, store, base, []string{"agents", "claude"}, skills...)
	mustApply(t, store, target)
	// The first destination is initially unchanged; only the second needs repair.
	secondMissing := filepath.Join(target.Paths[1], "beta")
	if err := os.RemoveAll(secondMissing); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	observed := make(chan error, 1)
	stop := make(chan struct{})
	defer close(stop)
	firstMissing := filepath.Join(target.Paths[0], "alpha")
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
			entries, err := os.ReadDir(filepath.Dir(target.Paths[1]))
			if err != nil {
				observed <- err
				return
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".racomi-apply-") {
					observed <- os.RemoveAll(firstMissing)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	applyErr := store.Apply(target.ID)
	if err := <-observed; err != nil {
		t.Fatal("failure injector:", err)
	}
	if applyErr == nil || !strings.Contains(applyErr.Error(), "changed while staging") {
		t.Fatalf("a changed plan must require a new preview: %v", applyErr)
	}
	if !reflect.DeepEqual(before, state(t, store)) {
		t.Fatal("changed plan committed inaccurate ownership")
	}
	missing(t, firstMissing)
	missing(t, secondMissing)
	if contents(t, filepath.Join(target.Paths[1], "alpha", "SKILL.md")) != "original" {
		t.Fatal("another destination changed before the new plan was checked")
	}
}

func TestTargetRejectsOverlappingOrBlankDirectories(t *testing.T) {
	store, base := newStore(t)
	parent := filepath.Join(base, "target")
	for _, paths := range [][]string{{parent, filepath.Join(parent, "nested")}, {filepath.Join(parent, "nested"), parent}, {parent, ""}} {
		if _, err := store.SaveTarget(Target{Name: "unsafe directories", Kind: "global", Paths: paths}); err == nil {
			t.Fatalf("accepted overlapping or blank directories: %v", paths)
		}
	}
	if len(state(t, store).Targets) != 0 {
		t.Fatal("invalid target directories partially changed the index")
	}
	missing(t, parent)
}
