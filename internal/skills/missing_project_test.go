package skills

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"racomi/internal/testutil"
)

func TestDeletedProjectCanRelocateWithoutLosingSelectionsOrExternalOwnership(t *testing.T) {
	for _, mode := range []string{DeploymentCopy, DeploymentSymlink} {
		t.Run(mode, func(t *testing.T) {
			store, base := newStore(t)
			settings := state(t, store).Settings
			if mode == DeploymentSymlink {
				testutil.RequireSymlink(t)
			}
			settings.DeploymentMode = mode
			if err := store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			skill := install(t, store, candidate(t, base, "alpha", "# Alpha"))[0]
			preset, err := store.SavePreset(Preset{Name: "Daily", SkillIDs: []string{skill.ID}})
			if err != nil {
				t.Fatal(err)
			}
			oldRoot, newRoot := filepath.Join(base, "old-project"), filepath.Join(base, "new-project")
			for _, root := range []string{oldRoot, newRoot} {
				if err := os.Mkdir(root, 0755); err != nil {
					t.Fatal(err)
				}
			}
			external := filepath.Join(base, "shared", "skills")
			target, err := store.SaveTarget(Target{Name: "Daily project", Icon: "compass", Kind: "project", Path: oldRoot, DirectoryIDs: []string{"agents"}, CustomPaths: []string{external}, EnabledSkillIDs: []string{skill.ID}, EnabledPresetIDs: []string{preset.ID}})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Apply(target.ID); err != nil {
				t.Fatal(err)
			}
			target = state(t, store).Targets[0]
			if err := os.RemoveAll(oldRoot); err != nil {
				t.Fatal(err)
			}
			// Reading remains usable, and applying must fail without recreating root.
			if _, err := store.State(); err != nil {
				t.Fatal(err)
			}
			for _, apply := range []func(string) error{store.Apply, store.ForceApply} {
				if err := apply(target.ID); err == nil || !strings.Contains(err.Error(), "project root directory does not exist") {
					t.Fatalf("missing project was not rejected: %v", err)
				}
			}
			missing(t, oldRoot)
			invalid := target
			invalid.ProjectRoot = filepath.Join(base, "still-missing")
			if _, err := store.SaveTarget(invalid); err == nil {
				t.Fatal("save accepted a missing replacement")
			}
			if state(t, store).Targets[0].ProjectRoot != oldRoot {
				t.Fatal("invalid save changed the project")
			}
			target.ProjectRoot = newRoot
			saved, err := store.SaveTarget(target)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(saved.EnabledSkillIDs, target.EnabledSkillIDs) || !slices.Equal(saved.EnabledPresetIDs, target.EnabledPresetIDs) || saved.Icon != target.Icon || !saved.Pending {
				t.Fatalf("relocation lost configuration: %+v", saved)
			}
			if len(saved.Deployments) != 1 || deploymentPath(saved, saved.Deployments[0]) != external {
				t.Fatalf("external ownership was lost: %+v", saved.Deployments)
			}
			missing(t, filepath.Join(newRoot, ".agents", "skills"))
			if err := store.Apply(saved.ID); err != nil {
				t.Fatal(err)
			}
			if state(t, store).Targets[0].Pending {
				t.Fatal("relocated project did not finish applying")
			}
			if _, err := os.Lstat(filepath.Join(newRoot, ".agents", "skills", skill.Name)); err != nil {
				t.Fatal(err)
			}
			missing(t, oldRoot)
		})
	}
}

func TestExistingProjectRootCanBeEditedBeforeReviewedApplication(t *testing.T) {
	store, base := newStore(t)
	skill := install(t, store, candidate(t, base, "alpha", "# Alpha"))[0]
	oldRoot, newRoot := filepath.Join(base, "original"), filepath.Join(base, "replacement")
	for _, root := range []string{oldRoot, newRoot} {
		if err := os.Mkdir(root, 0755); err != nil {
			t.Fatal(err)
		}
	}
	target, err := store.SaveTarget(Target{Name: "Project", Kind: "project", Path: oldRoot, EnabledSkillIDs: []string{skill.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(target.ID); err != nil {
		t.Fatal(err)
	}
	target = state(t, store).Targets[0]
	oldSkills := target.Path
	keep := filepath.Join(oldSkills, "user-notes.txt")
	if err := os.WriteFile(keep, []byte("Keep user content"), 0644); err != nil {
		t.Fatal(err)
	}
	target.ProjectRoot = newRoot
	saved, err := store.SaveTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Deployments) != 1 || deploymentPath(saved, saved.Deployments[0]) != oldSkills {
		t.Fatal("saving forgot existing deployment ownership")
	}
	missing(t, saved.Path)
	plan, err := store.Plan(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Directories) != 2 || !slices.Contains(plan.Add, skill.Name) || !slices.Contains(plan.Remove, skill.Name) {
		t.Fatalf("preview omitted migration: %+v", plan)
	}
	if err := store.Apply(target.ID); err != nil {
		t.Fatal(err)
	}
	missing(t, filepath.Join(oldSkills, skill.Name))
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("migration removed unrelated user content", err)
	}
	if _, err := os.Stat(filepath.Join(saved.Path, skill.Name)); err != nil {
		t.Fatal(err)
	}
}
