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

	"racomi/internal/testutil"
)

func writeLocalDeployment(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestForceApplyOverwritesUnmanagedEntriesAndKeepsUnrelatedContent(t *testing.T) {
	for _, mode := range []string{DeploymentCopy, DeploymentSymlink} {
		for _, kind := range []string{"directory", "file", "foreign-link", "case-collision"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				store, base := newStore(t)
				skill := install(t, store, candidate(t, base, "alpha", "selected"))[0]
				settings := state(t, store).Settings
				if mode == DeploymentSymlink {
					testutil.RequireSymlink(t)
				}
				settings.DeploymentMode = mode
				if err := store.SaveSettings(settings); err != nil {
					t.Fatal(err)
				}
				target := saveTarget(t, store, base, savePreset(t, store, "selected", skill))
				outside := filepath.Join(base, "personal")
				writeLocalDeployment(t, outside, "outside")
				writeLocalDeployment(t, filepath.Join(target.Path, "unrelated"), "keep")
				name := "alpha"
				if kind == "case-collision" {
					name = "ALPHA"
				}
				old := filepath.Join(target.Path, name)
				switch kind {
				case "directory", "case-collision":
					writeLocalDeployment(t, old, "unmanaged")
				case "file":
					if err := os.WriteFile(old, []byte("unmanaged file"), 0644); err != nil {
						t.Fatal(err)
					}
				case "foreign-link":
					if err := testutil.Symlink(t, outside, old); err != nil {
						t.Fatal(err)
					}
				}
				before := state(t, store)
				plan, err := store.Plan(target.ID)
				if err != nil || len(plan.Conflicts) == 0 || len(plan.BlockingConflicts) != 0 || !slices.Equal(plan.Overwrite, []string{old}) || !slices.Equal(plan.Update, []string{"alpha"}) {
					t.Fatalf("unmanaged entry was not identified for explicit overwrite: %+v %v", plan, err)
				}
				if err = store.Apply(target.ID); err == nil || !reflect.DeepEqual(before, state(t, store)) {
					t.Fatal("ordinary apply accepted the overwrite or changed ownership")
				}
				// The reviewed plan survives a real API JSON round trip.
				data, err := json.Marshal(plan)
				if err != nil {
					t.Fatal(err)
				}
				var reviewed ApplyPlan
				if err = json.Unmarshal(data, &reviewed); err != nil {
					t.Fatal(err)
				}
				if err = store.ForceApplyReviewed(target.ID, reviewed); err != nil {
					t.Fatal(err)
				}
				if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "selected" || contents(t, filepath.Join(outside, "SKILL.md")) != "outside" || contents(t, filepath.Join(target.Path, "unrelated", "SKILL.md")) != "keep" {
					t.Fatal("force apply changed unrelated content or followed a foreign link")
				}
				entries, err := os.ReadDir(target.Path)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.EqualFold(entry.Name(), "alpha") && entry.Name() != "alpha" {
						t.Fatal("force apply retained the old spelling")
					}
				}
				plan, err = store.Plan(target.ID)
				if err != nil || len(plan.Add)+len(plan.Update)+len(plan.Remove)+len(plan.Conflicts)+len(plan.Overwrite) != 0 || state(t, store).Targets[0].Pending {
					t.Fatalf("force apply did not commit clean ownership: %+v %v", plan, err)
				}
			})
		}
	}
}

func TestForceApplyRepairsOrRemovesChangedManagedContent(t *testing.T) {
	for _, mode := range []string{DeploymentCopy, DeploymentSymlink} {
		for _, action := range []string{"repair", "remove", "case-rename", "replaced-directory", "replaced-file"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				store, base := newStore(t)
				skill := install(t, store, candidate(t, base, "alpha", "selected"))[0]
				settings := state(t, store).Settings
				if mode == DeploymentSymlink {
					testutil.RequireSymlink(t)
				}
				settings.DeploymentMode = mode
				if err := store.SaveSettings(settings); err != nil {
					t.Fatal(err)
				}
				target := projectWithDirectories(t, store, base, []string{"agents", "claude"}, skill)
				mustApply(t, store, target)
				outside := filepath.Join(base, "personal")
				writeLocalDeployment(t, outside, "outside")
				deployed := filepath.Join(target.Paths[0], "alpha")
				if action == "case-rename" {
					if err := os.Rename(deployed, filepath.Join(target.Paths[0], "ALPHA")); err != nil {
						t.Fatal(err)
					}
				} else if action == "replaced-directory" || action == "replaced-file" {
					if err := os.RemoveAll(deployed); err != nil {
						t.Fatal(err)
					}
					if action == "replaced-directory" {
						writeLocalDeployment(t, deployed, "replacement directory")
					} else if err := os.WriteFile(deployed, []byte("replacement file"), 0644); err != nil {
						t.Fatal(err)
					}
				} else if mode == DeploymentSymlink {
					if err := os.Remove(deployed); err != nil {
						t.Fatal(err)
					}
					if err := testutil.Symlink(t, outside, deployed); err != nil {
						t.Fatal(err)
					}
				} else {
					writeLocalDeployment(t, deployed, "local edits")
				}
				if action == "remove" {
					if err := store.SetTargetSelection(target.ID, nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				plan, err := store.Plan(target.ID)
				if err != nil || len(plan.Conflicts) == 0 || len(plan.BlockingConflicts) != 0 || len(plan.Overwrite) != 1 {
					t.Fatalf("changed owned entry was not overwritable: %+v %v", plan, err)
				}
				if err = store.Apply(target.ID); err == nil {
					t.Fatal("ordinary apply accepted changed ownership")
				}
				if err = store.ForceApply(target.ID); err != nil {
					t.Fatal(err)
				}
				for _, path := range target.Paths {
					if action == "remove" {
						missing(t, filepath.Join(path, "alpha"))
					} else if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "selected" {
						t.Fatal("force apply did not repair a changed deployment")
					}
				}
				if contents(t, filepath.Join(outside, "SKILL.md")) != "outside" {
					t.Fatal("force apply followed a replacement symlink")
				}
				if got := state(t, store).Targets[0]; got.Pending || (action == "remove" && len(got.Deployments) != 0) {
					t.Fatalf("force apply retained stale ownership: %+v", got)
				}
			})
		}
	}
}

func TestForceApplyReviewedRejectsChangedContentAndNewOverwritePaths(t *testing.T) {
	for _, change := range []string{"same-path-content", "new-overwrite", "link-destination"} {
		t.Run(change, func(t *testing.T) {
			store, base := newStore(t)
			skills := install(t, store, candidate(t, base, "alpha", "selected"), candidate(t, base, "beta", "selected beta"))
			target := saveTarget(t, store, base, savePreset(t, store, "selected", skills...))
			old := filepath.Join(target.Path, "alpha")
			writeLocalDeployment(t, old, "first review")
			outside := filepath.Join(base, "outside")
			writeLocalDeployment(t, outside, "outside")
			if change == "link-destination" {
				if err := os.RemoveAll(old); err != nil {
					t.Fatal(err)
				}
				if err := testutil.Symlink(t, outside, old); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := store.Plan(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "same-path-content":
				writeLocalDeployment(t, old, "later edits")
			case "new-overwrite":
				writeLocalDeployment(t, filepath.Join(target.Path, "beta"), "unreviewed beta")
			case "link-destination":
				if err := os.Remove(old); err != nil {
					t.Fatal(err)
				}
				if err := testutil.Symlink(t, filepath.Join(base, "different"), old); err != nil {
					t.Fatal(err)
				}
			}
			before := state(t, store)
			snapshots, err := store.Snapshots()
			if err != nil {
				t.Fatal(err)
			}
			if err = store.ForceApplyReviewed(target.ID, plan); err == nil || !strings.Contains(err.Error(), "changed since review") {
				t.Fatalf("stale force review was accepted: %v", err)
			}
			if !reflect.DeepEqual(before, state(t, store)) {
				t.Fatal("stale review changed the index")
			}
			afterSnapshots, err := store.Snapshots()
			if err != nil || len(snapshots) != len(afterSnapshots) {
				t.Fatal("stale review began a transaction")
			}
			fresh, err := store.Plan(target.ID)
			if err != nil || fresh.Directories[0].Fingerprint == plan.Directories[0].Fingerprint {
				t.Fatalf("review fingerprint missed content changes: %+v %v", fresh, err)
			}
			if err = store.ForceApplyReviewed(target.ID, fresh); err != nil {
				t.Fatal("fresh review could not be applied:", err)
			}
		})
	}
}

func TestForceApplyDoesNotReadOrDeleteNestedSymlinkDestinations(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "selected"))[0]
	target := saveTarget(t, store, base, savePreset(t, store, "selected", skill))
	mustApply(t, store, target)
	// An oversized external file would make review fail if links were followed.
	// The force operation must review and remove only the added link itself.
	outside := filepath.Join(base, "large-external-file")
	if err := os.WriteFile(outside, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(outside, maxApplyFingerprintBytes+1); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(target.Path, "alpha", "external")
	if err := testutil.Symlink(t, outside, link); err != nil {
		t.Fatal(err)
	}
	plan, err := store.Plan(target.ID)
	if err != nil || len(plan.Overwrite) != 1 || len(plan.BlockingConflicts) != 0 {
		t.Fatalf("review followed a nested link or failed to classify it: %+v %v", plan, err)
	}
	if err = store.ForceApplyReviewed(target.ID, plan); err != nil {
		t.Fatal(err)
	}
	missing(t, link)
	if info, err := os.Stat(outside); err != nil || info.Size() != maxApplyFingerprintBytes+1 {
		t.Fatalf("force apply touched an external link destination: %v %v", info, err)
	}
}

func TestForceApplyStillBlocksDuplicateNamesDamagedObjectsAndUnsafeTargets(t *testing.T) {
	t.Run("duplicate-names-block-whole-batch", func(t *testing.T) {
		store, base := newStore(t)
		first, second := candidate(t, base, "alpha", "one"), candidate(t, base, "beta", "two")
		second.Skill.Name = "ALPHA"
		skills := install(t, store, first, second)
		target := saveTarget(t, store, base, savePreset(t, store, "duplicate", skills...))
		other, err := store.SaveTarget(Target{Name: "other", Kind: "global", Path: filepath.Join(base, "other"), EnabledSkillIDs: []string{skills[0].ID}})
		if err != nil {
			t.Fatal(err)
		}
		writeLocalDeployment(t, filepath.Join(other.Path, "alpha"), "unmanaged")
		plan, err := store.PlanTargets([]string{other.ID, target.ID})
		if err != nil || len(plan.BlockingConflicts) != 1 || len(plan.Overwrite) != 1 {
			t.Fatalf("blocking conflict was not distinguished: %+v %v", plan, err)
		}
		if err = store.ForceApplyTargetsReviewed([]string{other.ID, target.ID}, plan); err == nil {
			t.Fatal("force apply accepted duplicate deployment names")
		}
		missing(t, target.Path)
		if contents(t, filepath.Join(other.Path, "alpha", "SKILL.md")) != "unmanaged" {
			t.Fatal("a blocked batch changed another target")
		}
	})
	t.Run("damaged-object", func(t *testing.T) {
		store, base := newStore(t)
		skill := install(t, store, candidate(t, base, "alpha", "selected"))[0]
		target := saveTarget(t, store, base, savePreset(t, store, "selected", skill))
		writeLocalDeployment(t, filepath.Join(target.Path, "alpha"), "unmanaged")
		writeLocalDeployment(t, filepath.Join(store.Root(), "objects", skill.ContentHash), "damaged")
		if err := store.ForceApply(target.ID); err == nil || !strings.Contains(err.Error(), "damaged") {
			t.Fatalf("force apply deployed a damaged object: %v", err)
		}
		if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "unmanaged" {
			t.Fatal("object failure changed target content")
		}
	})
	t.Run("symlink-target", func(t *testing.T) {
		store, base := newStore(t)
		skill := install(t, store, candidate(t, base, "alpha", "selected"))[0]
		target := saveTarget(t, store, base, savePreset(t, store, "selected", skill))
		outside := filepath.Join(base, "outside")
		writeLocalDeployment(t, outside, "outside")
		if err := testutil.Symlink(t, outside, target.Path); err != nil {
			t.Fatal(err)
		}
		if err := store.ForceApply(target.ID); err == nil {
			t.Fatal("force apply traversed an unsafe target path")
		}
		if contents(t, filepath.Join(outside, "SKILL.md")) != "outside" {
			t.Fatal("unsafe target failure changed external content")
		}
	})
	t.Run("oversized-unmanaged-entry", func(t *testing.T) {
		store, base := newStore(t)
		skill := install(t, store, candidate(t, base, "alpha", "selected"))[0]
		target := saveTarget(t, store, base, savePreset(t, store, "selected", skill))
		writeLocalDeployment(t, filepath.Join(target.Path, "alpha"), "unmanaged")
		large := filepath.Join(target.Path, "alpha", "large")
		if err := os.WriteFile(large, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(large, maxApplyFingerprintBytes+1); err != nil {
			t.Fatal(err)
		}
		if err := store.ForceApply(target.ID); err == nil || !strings.Contains(err.Error(), "1 GiB") {
			t.Fatalf("force review did not bound unmanaged content: %v", err)
		}
		if contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "unmanaged" {
			t.Fatal("review limit failure changed target content")
		}
	})
}

func TestForceApplyReviewedBindsDesiredVersionIdentityAndDeploymentMode(t *testing.T) {
	for _, change := range []string{"version", "identity", "mode"} {
		t.Run(change, func(t *testing.T) {
			store, base := newStore(t)
			source := candidate(t, base, "alpha", "approved version")
			skill := install(t, store, source)[0]
			target := saveTarget(t, store, base, savePreset(t, store, "selected", skill))
			writeLocalDeployment(t, filepath.Join(target.Path, "alpha"), "unmanaged")
			plan, err := store.Plan(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "version":
				if err = os.WriteFile(filepath.Join(source.Path, "SKILL.md"), []byte("later version"), 0644); err != nil {
					t.Fatal(err)
				}
				install(t, store, source)
			case "identity":
				other := candidate(t, base, "other", "approved version")
				other.Skill.Name = "alpha"
				otherSkill := install(t, store, other)[0]
				if err = store.SetTargetSelection(target.ID, []string{otherSkill.ID}, nil); err != nil {
					t.Fatal(err)
				}
			case "mode":
				settings := state(t, store).Settings
				settings.DeploymentMode = DeploymentSymlink
				if err = store.SaveSettings(settings); err != nil {
					t.Fatal(err)
				}
			}
			fresh, err := store.Plan(target.ID)
			if err != nil || !slices.Equal(plan.Update, fresh.Update) || !slices.Equal(plan.Overwrite, fresh.Overwrite) || fresh.Directories[0].Fingerprint == plan.Directories[0].Fingerprint {
				t.Fatalf("review failed to bind desired deployment details: %+v %v", fresh, err)
			}
			before := state(t, store)
			if err = store.ForceApplyReviewed(target.ID, plan); err == nil || !strings.Contains(err.Error(), "changed since review") {
				t.Fatalf("changed desired deployment was accepted under an old review: %v", err)
			}
			if !reflect.DeepEqual(before, state(t, store)) || contents(t, filepath.Join(target.Path, "alpha", "SKILL.md")) != "unmanaged" {
				t.Fatal("stale deployment review changed files or ownership")
			}
		})
	}
}

func TestForceApplyChangedContentDuringStagingAbortsWithoutOverwriting(t *testing.T) {
	store, base := newStore(t)
	skills := install(t, store, candidate(t, base, "alpha", "selected"), candidate(t, base, "beta", strings.Repeat("b", 16<<20)))
	target := saveTarget(t, store, base, savePreset(t, store, "selected", skills...))
	local := filepath.Join(target.Path, "alpha", "SKILL.md")
	writeLocalDeployment(t, filepath.Dir(local), "before staging")
	before := state(t, store)
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
				if strings.HasPrefix(entry.Name(), ".racomi-apply-") {
					observed <- os.WriteFile(local, []byte("edits while staging"), 0644)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	err := store.ForceApply(target.ID)
	if injectErr := <-observed; injectErr != nil {
		t.Fatal("mutation injector:", injectErr)
	}
	if err == nil || !strings.Contains(err.Error(), "changed while staging") {
		t.Fatalf("force apply swallowed edits while staging: %v", err)
	}
	if contents(t, local) != "edits while staging" || !reflect.DeepEqual(before, state(t, store)) {
		t.Fatal("force apply changed files or ownership after stale staging")
	}
	missing(t, filepath.Join(target.Path, "beta"))
}

func TestForceApplyBatchRollsBackOverwrittenRemovedAndCaseRenamedEntries(t *testing.T) {
	store, base := newStore(t)
	skills := install(t, store, candidate(t, base, "alpha", "selected"), candidate(t, base, "beta", strings.Repeat("b", 16<<20)), candidate(t, base, "gamma", "original gamma"))
	project := projectWithDirectories(t, store, base, []string{"agents", "claude"}, skills[0], skills[1])
	for _, path := range project.Paths {
		writeLocalDeployment(t, filepath.Join(path, "alpha"), "unmanaged original")
	}
	global, err := store.SaveTarget(Target{Name: "global", Kind: "global", Path: filepath.Join(base, "global"), EnabledSkillIDs: []string{skills[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	writeLocalDeployment(t, outside, "outside")
	if err = os.MkdirAll(global.Path, 0755); err != nil {
		t.Fatal(err)
	}
	if err = testutil.Symlink(t, outside, filepath.Join(global.Path, "ALPHA")); err != nil {
		t.Fatal(err)
	}
	retired, err := store.SaveTarget(Target{Name: "retired", Kind: "global", Path: filepath.Join(base, "retired"), EnabledSkillIDs: []string{skills[2].ID}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, retired)
	writeLocalDeployment(t, filepath.Join(retired.Path, "gamma"), "local edits to remove")
	if err = store.SetTargetSelection(retired.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
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
			entries, err := os.ReadDir(filepath.Dir(project.Path))
			if err != nil {
				observed <- err
				return
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".racomi-apply-") {
					if err = os.Rename(indexPath, savedPath); err == nil {
						err = os.Mkdir(indexPath, 0700)
					}
					observed <- err
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	err = store.ForceApplyTargets([]string{project.ID, global.ID, retired.ID})
	if injectErr := <-observed; injectErr != nil {
		t.Fatal("failure injector:", injectErr)
	}
	if restoreErr := os.RemoveAll(indexPath); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if restoreErr := os.Rename(savedPath, indexPath); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil {
		t.Fatal("index failure did not fail force apply")
	}
	for _, path := range project.Paths {
		if contents(t, filepath.Join(path, "alpha", "SKILL.md")) != "unmanaged original" {
			t.Fatal("force rollback lost overwritten unmanaged content")
		}
		missing(t, filepath.Join(path, "beta"))
	}
	entries, err := os.ReadDir(global.Path)
	if err != nil || len(entries) != 1 || entries[0].Name() != "ALPHA" {
		t.Fatalf("rollback did not restore original casing: %+v %v", entries, err)
	}
	if destination, err := os.Readlink(filepath.Join(global.Path, "ALPHA")); err != nil || destination != outside {
		t.Fatalf("rollback did not restore the foreign link itself: %s %v", destination, err)
	}
	if contents(t, filepath.Join(retired.Path, "gamma", "SKILL.md")) != "local edits to remove" || contents(t, filepath.Join(outside, "SKILL.md")) != "outside" || !reflect.DeepEqual(before, state(t, store)) {
		t.Fatal("force rollback lost removed local edits, external content, or ownership")
	}
	for _, path := range append(project.Paths, global.Path, retired.Path) {
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".racomi-apply-") {
				t.Fatal("successful force rollback left staging data")
			}
		}
	}
}
