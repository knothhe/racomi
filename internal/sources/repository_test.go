package sources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	skillstore "racomi/internal/skills"
)

func TestRepositoryDownloadsOnlyRequestedSnapshotAndRetainsReviewedCommits(t *testing.T) {
	repo := testRepo(t)
	old := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "tag", "v1")
	testGit(t, repo, "checkout", "-b", "feature/branch")
	writeTestSkill(t, repo, "skills/feature", "feature", "Feature", "Feature branch")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Feature skills")
	feature := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "tag", "-a", "feature-release", "-m", "Annotated release")
	testGit(t, repo, "checkout", "main")
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Latest", "Latest main")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Main skills")
	main := testGit(t, repo, "rev-parse", "HEAD")
	ctx := context.Background()
	client := New(t.TempDir())
	preview, err := client.Preview(ctx, repo, "", "")
	if err != nil || preview.Ref != "main" || preview.Revision != main {
		t.Fatalf("default branch was not resolved: %+v %v", preview, err)
	}
	cache, err := client.repository(ctx, repo, false, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{old, feature, "refs/heads/feature/branch", "refs/tags/v1", "refs/tags/feature-release"} {
		if _, err := resolveRevision(ctx, cache, absent); err == nil {
			t.Fatalf("downloaded unrelated history/ref %s", absent)
		}
	}
	for _, item := range []struct{ ref, revision string }{
		{"feature/branch", feature}, {"v1", old}, {"feature-release", feature},
		{"refs/heads/main", main}, {"refs/tags/v1", old},
	} {
		preview, err := client.Preview(ctx, repo, item.ref, "")
		if err != nil || preview.Revision != item.revision || preview.Ref != item.ref {
			t.Fatalf("specified ref %q drifted: %+v %v", item.ref, preview, err)
		}
	}
	// A moving branch must not make a reviewed snapshot eligible for pruning.
	writeTestSkill(t, repo, "skills/alpha", "alpha", "New", "After review")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Advance main")
	if _, err := client.Preview(ctx, repo, "main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommand(ctx, cache, "gc", "--prune=now"); err != nil {
		t.Fatal(err)
	}
	if revision, err := resolveRevision(ctx, cache, "refs/racomi/revisions/"+main); err != nil || revision != main {
		t.Fatalf("reviewed commit was pruned: %s %v", revision, err)
	}
}

func TestHistoricalSyncFetchesExactCommitAfterCacheLoss(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	client := New(t.TempDir())
	source, id, err := client.RefreshSource(ctx, catalogSource(repo, "."))
	if err != nil {
		t.Fatal(err)
	}
	client.Release(id)
	writeTestSkill(t, repo, "skills/alpha", "alpha", "New", "After review")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Advance upstream")
	newHead := testGit(t, repo, "rev-parse", "HEAD")
	if err := os.RemoveAll(filepath.Join(client.cacheDir, "repositories")); err != nil {
		t.Fatal(err)
	}
	restarted := New(client.cacheDir)
	candidates, _, err := restarted.PrepareSourceSync(ctx, source, []string{"skills/alpha"})
	if err != nil || len(candidates) != 1 || candidates[0].Skill.Revision != source.Revision {
		t.Fatalf("historical sync drifted: %+v %v", candidates, err)
	}
	document, err := os.ReadFile(filepath.Join(candidates[0].Path, "SKILL.md"))
	if err != nil || !strings.Contains(string(document), "Version one") {
		t.Fatalf("historical content changed: %s %v", document, err)
	}
	cache, err := restarted.repository(ctx, repo, false, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRevision(ctx, cache, newHead); err == nil {
		t.Fatal("historical synchronization downloaded the new head")
	}
}

func TestHistoricalRefsWorkInFreshShallowCache(t *testing.T) {
	repo := testRepo(t)
	old := testGit(t, repo, "rev-parse", "HEAD")
	writeTestSkill(t, repo, "skills/alpha", "alpha", "New", "New main")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Advance upstream")
	for _, ref := range []string{old, old[:10]} {
		t.Run(ref, func(t *testing.T) {
			client := New(t.TempDir())
			preview, err := client.Preview(context.Background(), repo, ref, "")
			if err != nil || preview.Revision != old || preview.Ref != ref {
				t.Fatalf("historical commit did not resolve: %+v %v", preview, err)
			}
		})
	}
}

func TestShallowPreviewAncestryRejectsStaleAndRewrittenHistory(t *testing.T) {
	repo := testRepo(t)
	client := New(t.TempDir())
	ctx := context.Background()
	first, err := client.Preview(ctx, repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		testGit(t, repo, "commit", "--allow-empty", "-m", "Advance upstream")
	}
	latest, err := client.Preview(ctx, repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if !client.PreviewAdvances(ctx, latest.ID, first.Revision) || client.PreviewAdvances(ctx, first.ID, latest.Revision) {
		t.Fatal("shallow boundary was mistaken for changed history or a stale preview was accepted")
	}
	testGit(t, repo, "checkout", "--orphan", "rewritten")
	testGit(t, repo, "commit", "-m", "Rewrite history")
	rewritten, err := client.Preview(ctx, repo, "rewritten", "")
	if err != nil || client.PreviewAdvances(ctx, rewritten.ID, latest.Revision) {
		t.Fatalf("unrelated history accepted: %+v %v", rewritten, err)
	}
}

func TestCheckUpdatesFetchesEveryRequestedRef(t *testing.T) {
	repo := testRepo(t)
	client := New(t.TempDir())
	ctx := context.Background()
	initial, err := client.Preview(ctx, repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "checkout", "-b", "release")
	if _, err := client.Preview(ctx, repo, "release", ""); err != nil {
		t.Fatal(err)
	}
	skills := []skillstore.Skill{}
	for _, ref := range []string{"main", "release"} {
		testGit(t, repo, "checkout", ref)
		writeTestSkill(t, repo, "skills/alpha", "alpha", ref, ref+" changed")
		testGit(t, repo, "add", ".")
		testGit(t, repo, "commit", "-m", ref+" changes")
		skills = append(skills, skillstore.Skill{ID: ref, Source: repo, Ref: ref, Subpath: "skills/alpha", Revision: initial.Revision})
	}
	updates, err := client.CheckUpdates(ctx, skills)
	if err != nil || len(updates) != 2 || !updates[0].Available || !updates[1].Available || updates[0].Error != "" || updates[1].Error != "" || updates[0].Revision == updates[1].Revision {
		t.Fatalf("one ref reused another ref's fetch: %+v %v", updates, err)
	}
}

func TestGitTransferTimeoutsKeepCancellationAndEffectiveBudget(t *testing.T) {
	// A synthetic Git executable lets the caller's deadline expire without
	// network access or a five-minute wait. exec replaces the shell for cleanup.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nexec /bin/sleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, operation := range []struct {
		name string
		run  func(context.Context, string, ...string) (string, error)
	}{{"repository download", gitDownload}, {"repository connection", gitConnection}} {
		t.Run(operation.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			_, err := operation.run(ctx, "", "--version")
			if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), operation.name+" timed out after 1 seconds") {
				t.Fatalf("lost deadline or reported the default instead of caller's budget: %v", err)
			}
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
			_, err = operation.run(ctx, "", "--version")
			if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "was canceled") || strings.Contains(err.Error(), "timed out") {
				t.Fatalf("cancellation mislabeled: %v", err)
			}
		})
	}
}

func TestTreeURLResolvesLongestRemoteBranchBeforeDownloading(t *testing.T) {
	repo := testRepo(t)
	testGit(t, repo, "tag", "feature")
	testGit(t, repo, "checkout", "-b", "feature/branch")
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Feature", "Feature branch")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Feature skill")
	ctx := context.Background()
	client := New(t.TempDir())
	preview, err := client.previewOptionsLocked(ctx, sourceSpec{source: repo, subpath: ".", treeTail: "feature/branch/skills/alpha"}, true, false)
	if err != nil || preview.Ref != "feature/branch" || preview.Subpath != "skills/alpha" || len(preview.Candidates) != 1 {
		t.Fatalf("slash branch/path boundary changed: %+v %v", preview, err)
	}
	cache, err := client.repository(ctx, repo, false, preview.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRevision(ctx, cache, "refs/tags/feature"); err == nil {
		t.Fatal("downloaded a shorter unrelated tag")
	}
}

func TestDeletedCachedTagDoesNotShadowRequestedBranch(t *testing.T) {
	repo := testRepo(t)
	testGit(t, repo, "tag", "release")
	old := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "checkout", "-b", "release")
	testGit(t, repo, "commit", "--allow-empty", "-m", "Advance release branch")
	current := testGit(t, repo, "rev-parse", "HEAD")
	ctx := context.Background()
	client := New(t.TempDir())
	preview, err := client.Preview(ctx, repo, "release", "")
	if err != nil || preview.Revision != old {
		t.Fatalf("Git's tag precedence changed: %+v %v", preview, err)
	}
	testGit(t, repo, "tag", "-d", "release")
	preview, err = client.Preview(ctx, repo, "release", "")
	if err != nil || preview.Revision != current {
		t.Fatalf("deleted cached tag shadowed the requested branch: %+v %v", preview, err)
	}
	// A cached immutable commit remains usable even after upstream disappears.
	testGit(t, repo, "update-ref", "-d", "refs/heads/release")
	preview, err = client.Preview(ctx, repo, old, "")
	if err != nil || preview.Revision != old {
		t.Fatalf("retained historical version was lost: %+v %v", preview, err)
	}
}

func TestLegacyFullCloneCacheStillSupportsScopedRefreshAndHistory(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	client := New(t.TempDir())
	cache, err := client.repository(ctx, repo, true, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommand(ctx, "", "clone", "--bare", "--no-local", "--", repo, cache); err != nil {
		t.Fatal(err)
	}
	old := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "commit", "--allow-empty", "-m", "Advance legacy cache")
	latest, err := client.Preview(ctx, repo, "main", "")
	if err != nil || latest.Revision == old {
		t.Fatalf("legacy cache was not refreshed: %+v %v", latest, err)
	}
	if _, err := gitCommand(ctx, cache, "gc", "--prune=now"); err != nil {
		t.Fatal(err)
	}
	historical, err := client.Preview(ctx, repo, old, "")
	if err != nil || historical.Revision != old {
		t.Fatalf("legacy reviewed commit lost: %+v %v", historical, err)
	}
}

func TestDefaultBranchStaysPinnedWhenTagSharesItsName(t *testing.T) {
	repo := testRepo(t)
	old := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "tag", "main", old)
	ctx := context.Background()
	client := New(t.TempDir())
	source := catalogSource(repo, ".")
	source.Ref = ""
	source, id, err := client.RefreshSource(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(id)
	if source.Ref != "heads/main" {
		t.Fatalf("default branch was not pinned: %+v", source)
	}
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Updated", "Branch content")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Advance default branch")
	current := testGit(t, repo, "rev-parse", "HEAD")
	// Also populate the colliding tag in the shared cache.
	tag, err := client.Preview(ctx, repo, "main", "")
	if err != nil || tag.Revision != old {
		t.Fatalf("tag precedence changed: %+v %v", tag, err)
	}
	defer client.Release(tag.ID)
	source, id, err = client.RefreshSource(ctx, source)
	if err != nil || source.Revision != current {
		t.Fatalf("default branch drifted to the tag: %+v %v", source, err)
	}
	defer client.Release(id)
	candidates, id, err := client.PrepareSourceSync(ctx, source, []string{"skills/alpha"})
	if err != nil || len(candidates) != 1 || candidates[0].Skill.Ref != source.Ref || candidates[0].Skill.Revision != current {
		t.Fatalf("synchronization lost the pinned branch: %+v %v", candidates, err)
	}
	defer client.Release(id)
}

func TestLegacyNamespaceRefsRefreshWithAndWithoutCache(t *testing.T) {
	repo := testRepo(t)
	old := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "tag", "main")
	testGit(t, repo, "commit", "--allow-empty", "-m", "Advance branch")
	current := testGit(t, repo, "rev-parse", "HEAD")
	legacyRef := testGit(t, repo, "symbolic-ref", "--short", "HEAD")
	if legacyRef != "heads/main" {
		t.Fatalf("unexpected legacy ref: %q", legacyRef)
	}
	for _, cached := range []bool{true, false} {
		name := "fresh cache"
		if cached {
			name = "legacy full clone"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			client := New(t.TempDir())
			if cached {
				cache, err := client.repository(ctx, repo, true, "refs/heads/main")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(cache); err != nil {
					t.Fatal(err)
				}
				if _, err := gitCommand(ctx, "", "clone", "--bare", "--no-local", "--", repo, cache); err != nil {
					t.Fatal(err)
				}
			}
			for _, item := range []struct{ ref, revision string }{{legacyRef, current}, {"tags/main", old}} {
				source := catalogSource(repo, ".")
				source.Ref = item.ref
				updated, id, err := client.RefreshSource(ctx, source)
				if err != nil || updated.Ref != item.ref || updated.Revision != item.revision {
					t.Fatalf("saved namespace ref changed or failed: %+v %v", updated, err)
				}
				defer client.Release(id)
			}
		})
	}
}

func TestSHA256RepositoryPreviewRefreshAndHistoricalSync(t *testing.T) {
	repo := t.TempDir()
	testGit(t, repo, "init", "--object-format=sha256", "-b", "main")
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Alpha", "Version one")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Initial skill")
	old := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "tag", "-a", "v1", "-m", "Annotated release")
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Alpha", "Version two")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Advance branch")
	current := testGit(t, repo, "rev-parse", "HEAD")
	for _, item := range []struct{ ref, revision string }{{"", current}, {"main", current}, {"v1", old}, {old, old}, {old[:10], old}, {old[:40], old}} {
		t.Run("ref="+item.ref, func(t *testing.T) {
			client := New(t.TempDir())
			preview, err := client.Preview(context.Background(), repo, item.ref, "")
			if err != nil || preview.Revision != item.revision {
				t.Fatalf("SHA-256 source failed: %+v %v", preview, err)
			}
			defer client.Release(preview.ID)
		})
	}
	ctx := context.Background()
	client := New(t.TempDir())
	source, id, err := client.RefreshSource(ctx, catalogSource(repo, "."))
	if err != nil || source.Revision != current {
		t.Fatalf("SHA-256 refresh failed: %+v %v", source, err)
	}
	if err := client.Release(id); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "commit", "--allow-empty", "-m", "Advance after review")
	if err := os.RemoveAll(filepath.Join(client.cacheDir, "repositories")); err != nil {
		t.Fatal(err)
	}
	candidates, id, err := client.PrepareSourceSync(ctx, source, []string{"skills/alpha"})
	if err != nil || len(candidates) != 1 || candidates[0].Skill.Revision != current {
		t.Fatalf("SHA-256 historical synchronization drifted: %+v %v", candidates, err)
	}
	defer client.Release(id)
}
