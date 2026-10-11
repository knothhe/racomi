package sources

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
)

func catalogSource(repo, scope string) skillstore.Source {
	return skillstore.Source{ID: "source-test", Name: "test repository", Repository: repo, Ref: "main", Subpath: scope, Mode: "selected", Catalog: []skillstore.SourceSkill{}}
}

func TestSourceCatalogRecordsEmptyRepositoryAndMissingScope(t *testing.T) {
	for _, scope := range []string{".", "skills"} {
		t.Run(scope, func(t *testing.T) {
			repo := testRepo(t)
			client := New(t.TempDir())
			source, previewID, err := client.RefreshSource(context.Background(), catalogSource(repo, scope))
			if err != nil {
				t.Fatal(err)
			}
			if len(source.Catalog) == 0 || source.Revision == "" || source.RefreshedAt == "" {
				t.Fatal("initial refresh did not discover skills")
			}
			for _, item := range source.Catalog {
				if item.Removed || !item.New || len(item.ContentHash) != 64 {
					t.Fatalf("invalid discovered entry: %+v", item)
				}
			}
			if err = client.Release(previewID); err != nil {
				t.Fatal(err)
			}
			if scope == "." {
				testGit(t, repo, "rm", "-r", "skills", "nested")
			} else {
				testGit(t, repo, "rm", "-r", "skills")
			}
			if err = os.WriteFile(filepath.Join(repo, "README.md"), []byte("No skills in this scope.\n"), 0644); err != nil {
				t.Fatal(err)
			}
			testGit(t, repo, "add", ".")
			testGit(t, repo, "commit", "-m", "Remove source skills")
			updated, previewID, err := client.RefreshSource(context.Background(), source)
			if err != nil {
				t.Fatal("empty catalog should be a successful refresh", err)
			}
			defer client.Release(previewID)
			if len(updated.Catalog) != len(source.Catalog) || updated.Revision == source.Revision || updated.LastError != "" {
				t.Fatalf("empty refresh lost prior inventory: %+v", updated)
			}
			for _, item := range updated.Catalog {
				if !item.Removed || item.New {
					t.Fatalf("upstream removal was not retained: %+v", item)
				}
			}
			fresh, freshID, err := client.RefreshSource(context.Background(), catalogSource(repo, scope))
			if err != nil || len(fresh.Catalog) != 0 || fresh.Revision == "" {
				t.Fatalf("new source with no skills rejected: %+v %v", fresh, err)
			}
			defer client.Release(freshID)
			if _, err = client.Preview(context.Background(), repo, "main", scope); err == nil {
				t.Fatal("ordinary discovery should still report that no skills were found")
			}
		})
	}
}

func TestSourceCatalogFailureNeverImpliesRemoval(t *testing.T) {
	repo := testRepo(t)
	client := New(t.TempDir())
	source, previewID, err := client.RefreshSource(context.Background(), catalogSource(repo, "."))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(previewID)
	before := source
	before.Catalog = append([]skillstore.SourceSkill{}, source.Catalog...)
	if err = os.WriteFile(filepath.Join(repo, "skills", "alpha", "SKILL.md"), []byte("malformed skill without frontmatter\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Break upstream metadata")
	partial, partialID, err := client.RefreshSource(context.Background(), source)
	if err != nil || partialID == "" || partial.LastError != "" || partial.Revision == source.Revision {
		t.Fatalf("malformed skill blocked partial refresh: %+v %q %v", partial, partialID, err)
	}
	defer client.Release(partialID)
	alpha := partialCatalogEntry(t, partial, "skills/alpha")
	if alpha.Removed || !strings.Contains(alpha.SkipReason, "frontmatter") || alpha.ContentHash != partialCatalogEntry(t, source, "skills/alpha").ContentHash {
		t.Fatalf("malformed skill was not retained as skipped: %+v", alpha)
	}
	if !reflect.DeepEqual(before, source) {
		t.Fatal("partial refresh mutated the caller's prior catalog")
	}
	source = partial
	before = source
	before.Catalog = append([]skillstore.SourceSkill{}, source.Catalog...)
	// Keep the repository directory present so validation reaches a failing
	// fetch, rather than rejecting a missing local repository path first.
	gitDir, offlineGitDir := filepath.Join(repo, ".git"), filepath.Join(repo, ".git-offline")
	if err = os.Rename(gitDir, offlineGitDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(offlineGitDir, gitDir) })
	failed, failedID, err := client.RefreshSource(context.Background(), source)
	if err == nil || !strings.Contains(err.Error(), "cannot refresh repository") || failedID != "" {
		t.Fatalf("fetch failure was not reported: %q %v", failedID, err)
	}
	if !reflect.DeepEqual(before, failed) {
		t.Fatal("failed fetch marked prior inventory as removed")
	}
	old, err := client.Candidates(previewID, []string{"skills/alpha"})
	if err != nil {
		t.Fatal("failed refresh invalidated previous preview", err)
	}
	body, err := os.ReadFile(filepath.Join(old[0].Path, "SKILL.md"))
	if err != nil || !bytes.Contains(body, []byte("Version one")) {
		t.Fatal("previous preview changed after refresh failure", err)
	}
}

func TestPrepareSourceSyncAfterRestartUsesReviewedCommit(t *testing.T) {
	repo := testRepo(t)
	cache := t.TempDir()
	client := New(cache)
	source, previewID, err := client.RefreshSource(context.Background(), catalogSource(repo, "."))
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Release(previewID); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Updated upstream", "Version two")
	writeTestSkill(t, repo, "skills/new", "new", "New upstream skill", "New skill")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Change upstream after review")
	restarted := New(cache)
	latest, latestID, err := restarted.RefreshSource(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Release(latestID)
	if latest.Revision == source.Revision || len(latest.Catalog) != len(source.Catalog)+1 {
		t.Fatal("upstream change was not fetched")
	}
	prepared, pinnedID, err := restarted.PrepareSourceSync(context.Background(), source, []string{"skills/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Release(pinnedID)
	if len(prepared) != 1 || prepared[0].Skill.Revision != source.Revision || prepared[0].Skill.SourceID != source.ID || prepared[0].Skill.Ref != source.Ref {
		t.Fatalf("prepared candidates changed reviewed identity: %+v", prepared)
	}
	body, err := os.ReadFile(filepath.Join(prepared[0].Path, "SKILL.md"))
	if err != nil || !bytes.Contains(body, []byte("Version one")) || bytes.Contains(body, []byte("Version two")) {
		t.Fatal("restarted sync drifted to the new head", err)
	}
	if _, _, err = restarted.PrepareSourceSync(context.Background(), source, []string{"skills/new"}); err == nil {
		t.Fatal("sync offered a path absent from the reviewed revision")
	}
}

func TestSourceCatalogSeparatesRepositoryRefsAndFutureAdditions(t *testing.T) {
	repo := testRepo(t)
	client := New(t.TempDir())
	source, previewID, err := client.RefreshSource(context.Background(), catalogSource(repo, "."))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(previewID)
	testGit(t, repo, "tag", "v1")
	for i := range source.Catalog {
		source.Catalog[i].New = false
	}
	writeTestSkill(t, repo, "skills/added", "added", "Future addition", "Added skill")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add skill after subscription")
	latest, latestID, err := client.RefreshSource(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(latestID)
	newCount := 0
	for _, entry := range latest.Catalog {
		if entry.New {
			newCount++
			if entry.Subpath != "skills/added" {
				t.Fatalf("existing catalog path marked new: %+v", entry)
			}
		}
	}
	if newCount != 1 {
		t.Fatal("future catalog addition was not discovered")
	}
	pinned := source
	pinned.ID, pinned.Ref = "source-tag", "v1"
	pinned, pinnedID, err := client.RefreshSource(context.Background(), pinned)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(pinnedID)
	if pinned.Revision != source.Revision || pinned.Revision == latest.Revision || pinned.Ref != "v1" || len(pinned.Catalog) != len(source.Catalog) {
		t.Fatalf("same repository refs were conflated: %+v", pinned)
	}
}
