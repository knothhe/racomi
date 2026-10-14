package desktop

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
)

func partialSourceEntry(t *testing.T, source skillstore.Source, subpath string) skillstore.SourceSkill {
	t.Helper()
	for _, item := range source.Catalog {
		if item.Subpath == subpath {
			return item
		}
	}
	t.Fatalf("missing catalog entry %q: %+v", subpath, source.Catalog)
	return skillstore.SourceSkill{}
}

func TestSourcePartialRefreshSynchronizesValidNeighborAndPreservesLocalSkill(t *testing.T) {
	f := newSourceFixture(t)
	ctx := context.Background()
	state, err := f.service.SaveSource(skillstore.Source{Name: "Team", Repository: f.repo, Ref: "main", Mode: "all"})
	if err != nil {
		t.Fatal(err)
	}
	id := state.State.Sources[0].ID
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.service.SyncSource(ctx, id, state.State.Sources[0].Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	alphaBefore, betaBefore := sourceSkill(t, state, "alpha"), sourceSkill(t, state, "beta")
	oldSource := state.State.Sources[0]
	if err = os.WriteFile(filepath.Join(f.repo, "skills", "alpha", "SKILL.md"), []byte("Malformed metadata\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.repo, "CLAUDE.md"), []byte("Source instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("CLAUDE.md", filepath.Join(f.repo, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	f.write("skills/beta", "beta", "beta v2")
	f.commit("update healthy beta alongside malformed alpha and unrelated symlink")
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := state.State.Sources[0]
	if refreshed.Revision == oldSource.Revision || refreshed.LastError != "" || len(refreshed.Warnings) != 0 {
		t.Fatalf("partial refresh failed or reported an unrelated alias: %+v", refreshed)
	}
	alphaEntry := partialSourceEntry(t, refreshed, "skills/alpha")
	if alphaEntry.SkipReason == "" || alphaEntry.Removed || alphaEntry.ContentHash != alphaBefore.ContentHash || alphaEntry.Revision != alphaBefore.Revision {
		t.Fatalf("skipped skill lost its reviewed version: %+v", alphaEntry)
	}
	if sourceSkill(t, state, "alpha").UpstreamRemoved || sourceSkill(t, state, "beta").UpstreamRemoved {
		t.Fatal("partial refresh marked an existing upstream skill removed")
	}
	updates, err := f.service.CheckUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, update := range updates {
		seen[update.SkillID] = true
		if update.SkillID == alphaBefore.ID && (update.Available || update.Removed || update.Error == "") {
			t.Fatalf("skipped alpha was offered as an update or removal: %+v", update)
		}
		if update.SkillID == betaBefore.ID && (!update.Available || update.Removed || update.Error != "") {
			t.Fatalf("valid beta update was not available: %+v", update)
		}
	}
	if !seen[alphaBefore.ID] || !seen[betaBefore.ID] {
		t.Fatalf("partial check omitted installed skills: %+v", updates)
	}
	if _, err = f.service.UpdateSkill(ctx, alphaBefore.ID); err == nil {
		t.Fatal("single-skill update accepted skipped metadata")
	}
	state, err = f.service.SyncSource(ctx, id, refreshed.Revision, nil)
	if err != nil {
		t.Fatal("all-source sync was blocked by skipped alpha", err)
	}
	alphaAfter, betaAfter := sourceSkill(t, state, "alpha"), sourceSkill(t, state, "beta")
	if !reflect.DeepEqual(alphaAfter, alphaBefore) {
		t.Fatalf("sync changed retained local alpha: before %+v, after %+v", alphaBefore, alphaAfter)
	}
	if betaAfter.ID != betaBefore.ID || betaAfter.Revision != refreshed.Revision || betaAfter.ContentHash == betaBefore.ContentHash {
		t.Fatalf("valid neighbor did not update in place: %+v", betaAfter)
	}
	for _, check := range []struct {
		id, body string
	}{{alphaBefore.ID, "alpha v1"}, {betaBefore.ID, "beta v2"}} {
		document, readErr := f.service.ReadSkillDocument(check.id)
		if readErr != nil || !strings.Contains(document, check.body) {
			t.Fatalf("local content did not match reviewed sync for %s: %q %v", check.id, document, readErr)
		}
	}
	reopened, err := New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.GetState()
	if err != nil || !reflect.DeepEqual(persisted.State.Sources[0].Warnings, state.State.Sources[0].Warnings) || partialSourceEntry(t, persisted.State.Sources[0], "skills/alpha").SkipReason == "" {
		t.Fatalf("restart lost partial refresh status: %+v %v", persisted.State.Sources, err)
	}
}

func TestSourceEmptyPriorCatalogPreservesInstalledSkippedSkills(t *testing.T) {
	for _, scenario := range []struct {
		problem, ref string
	}{
		{"metadata", "main"},
		{"skill-directory-symlink", "main"},
		{"ancestor-directory-symlink", "main"},
		{"metadata", ""},
		{"skill-directory-symlink", ""},
		{"ancestor-directory-symlink", ""},
	} {
		t.Run(scenario.problem+"/ref="+scenario.ref, func(t *testing.T) {
			f := newSourceFixture(t)
			ctx := context.Background()
			if err := os.MkdirAll(filepath.Join(f.repo, "healthy"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(f.repo, "skills", "beta"), filepath.Join(f.repo, "healthy", "beta")); err != nil {
				t.Fatal(err)
			}
			f.commit("put healthy neighbor outside replaceable ancestor")
			state, err := f.service.SaveSource(skillstore.Source{Name: "Original", Repository: f.repo, Ref: "main", Mode: "all"})
			if err != nil {
				t.Fatal(err)
			}
			id := state.State.Sources[0].ID
			state, err = f.service.RefreshSource(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			state, err = f.service.SyncSource(ctx, id, state.State.Sources[0].Revision, nil)
			if err != nil {
				t.Fatal(err)
			}
			alphaBefore := sourceSkill(t, state, "alpha")
			// Re-registering a source reconnects its installed skills before its
			// first catalog refresh, also covering a migrated/empty catalog.
			if _, err = f.service.DeleteSource(id); err != nil {
				t.Fatal(err)
			}
			state, err = f.service.SaveSource(skillstore.Source{Name: "Reconnected", Repository: f.repo, Ref: scenario.ref, Mode: "all"})
			if err != nil {
				t.Fatal(err)
			}
			id = state.State.Sources[0].ID
			if len(state.State.Sources[0].Catalog) != 0 || state.State.Sources[0].Revision != "" || (scenario.ref != "" && sourceSkill(t, state, "alpha").SourceID != id) {
				t.Fatal("fixture did not create linked local skills with an empty catalog")
			}
			switch scenario.problem {
			case "metadata":
				err = os.WriteFile(filepath.Join(f.repo, "skills", "alpha", "SKILL.md"), []byte("Malformed metadata\n"), 0644)
			case "skill-directory-symlink":
				if err = os.RemoveAll(filepath.Join(f.repo, "skills", "alpha")); err == nil {
					err = os.Symlink("../../healthy/beta", filepath.Join(f.repo, "skills", "alpha"))
				}
			case "ancestor-directory-symlink":
				if err = os.RemoveAll(filepath.Join(f.repo, "skills")); err == nil {
					err = os.Symlink("healthy", filepath.Join(f.repo, "skills"))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			f.write("healthy/beta", "beta", "beta v2")
			f.commit("skip prior installed alpha while updating beta")
			state, err = f.service.RefreshSource(ctx, id)
			if err != nil || state.State.Sources[0].LastError != "" {
				t.Fatalf("empty prior catalog blocked partial refresh: %+v %v", state.State.Sources, err)
			}
			alpha := sourceSkill(t, state, "alpha")
			entry := partialSourceEntry(t, state.State.Sources[0], "skills/alpha")
			if alpha.SourceID != id || alpha.UpstreamRemoved || entry.Removed || entry.SkipReason == "" || entry.ContentHash != alphaBefore.ContentHash || entry.Revision != alphaBefore.Revision {
				t.Fatalf("empty previous catalog treated unreadable local skill as deleted: skill %+v, catalog %+v", alpha, entry)
			}
			state, err = f.service.SyncSource(ctx, id, state.State.Sources[0].Revision, nil)
			if err != nil {
				t.Fatal(err)
			}
			if alpha = sourceSkill(t, state, "alpha"); alpha.ID != alphaBefore.ID || alpha.ContentHash != alphaBefore.ContentHash || alpha.Revision != alphaBefore.Revision || alpha.UpstreamRemoved {
				t.Fatalf("empty-catalog sync changed retained local skill: %+v", alpha)
			}
			document, err := f.service.ReadSkillDocument(alpha.ID)
			if err != nil || !strings.Contains(document, "alpha v1") {
				t.Fatal("empty-catalog sync lost local content", err)
			}
			beta := sourceSkill(t, state, "beta")
			document, err = f.service.ReadSkillDocument(beta.ID)
			if err != nil || !strings.Contains(document, "beta v2") {
				t.Fatal("empty-catalog sync did not update healthy neighbor", err)
			}
		})
	}
}

func TestSourceDeletionOfNeverReadableSkillDoesNotBlockRefresh(t *testing.T) {
	f := newSourceFixture(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(f.repo, "skills", "alpha", "SKILL.md"), []byte("Malformed metadata\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f.commit("add an unreadable skill before first catalog")
	state, err := f.service.SaveSource(skillstore.Source{Name: "Team", Repository: f.repo, Ref: "main", Mode: "all"})
	if err != nil {
		t.Fatal(err)
	}
	id := state.State.Sources[0].ID
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil || state.State.Sources[0].LastError != "" || partialSourceEntry(t, state.State.Sources[0], "skills/alpha").SkipReason == "" {
		t.Fatalf("first-scan unreadable skill blocked refresh: %+v %v", state.State.Sources, err)
	}
	f.git("rm", "-r", "skills/alpha")
	f.commit("remove never-readable skill")
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil || state.State.Sources[0].LastError != "" {
		t.Fatalf("removed unreadable skill made source invalid: %+v %v", state.State.Sources, err)
	}
	for _, item := range state.State.Sources[0].Catalog {
		if item.Subpath == "skills/alpha" && (!item.Removed || item.SkipReason != "") {
			t.Fatalf("deleted unreadable skill still appears skipped: %+v", item)
		}
	}
	state, err = f.service.SyncSource(ctx, id, state.State.Sources[0].Revision, nil)
	if err != nil || len(state.State.Skills) != 1 || sourceSkill(t, state, "beta").UpstreamRemoved {
		t.Fatal("healthy skill remained blocked after unreadable skill deletion", err)
	}
}
