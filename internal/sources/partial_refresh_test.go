package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"racomi/internal/testutil"
	skillstore "racomi/internal/skills"
)

func partialCatalogEntry(t *testing.T, source skillstore.Source, subpath string) skillstore.SourceSkill {
	t.Helper()
	for _, item := range source.Catalog {
		if item.Subpath == subpath {
			return item
		}
	}
	t.Fatalf("missing catalog path %q: %+v", subpath, source.Catalog)
	return skillstore.SourceSkill{}
}

func TestSourceRefreshSkipsUnrelatedSymlink(t *testing.T) {
	repo := testRepo(t)
	aliases := map[string]string{
		".claude/CLAUDE.md":   "../AGENTS.md",
		".claude/agents":      "../agents",
		".claude/skills":      "../skills",
		".opencode/agents":    "../agents",
		".windsurf/workflows": "../workflows",
		"GEMINI.md":           "AGENTS.md",
	}
	for name, target := range aliases {
		filename := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := testutil.Symlink(t, target, filename); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add repository instructions alias")
	client := New(t.TempDir())
	registered := catalogSource(repo, ".")
	registered.Warnings = []string{`skipped repository entry "GEMINI.md": symlink is not supported`}
	source, previewID, err := client.RefreshSource(context.Background(), registered)
	if err != nil {
		t.Fatal("unrelated repository symlink blocked refresh", err)
	}
	defer client.Release(previewID)
	if source.LastError != "" || len(source.Catalog) != 2 || len(source.Warnings) != 0 {
		t.Fatalf("unrelated aliases affected source discovery: %+v", source)
	}
	preview, err := client.DescribePreview(previewID)
	if err != nil || len(preview.Candidates) != 2 || len(preview.Skipped) != 0 || len(preview.Warnings) != 0 {
		t.Fatalf("invalid partial preview: %+v %v", preview, err)
	}
	candidates, err := client.Candidates(previewID, []string{"skills/alpha"})
	if err != nil || len(candidates) != 1 {
		t.Fatal("normal skill was unavailable", err)
	}
	root := filepath.Dir(filepath.Dir(candidates[0].Path))
	for name := range aliases {
		if _, err = os.Lstat(filepath.Join(root, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("skipped symlink %q was materialized: %v", name, err)
		}
	}
}

func TestPartialSourceRefreshRetainsSkippedSkillsAndRecovers(t *testing.T) {
	for _, problem := range []string{"metadata", "skill-document-symlink", "skill-asset-symlink", "skill-directory-symlink", "ancestor-directory-symlink", "skill-size"} {
		t.Run(problem, func(t *testing.T) {
			repo := testRepo(t)
			client := New(t.TempDir())
			source, previewID, err := client.RefreshSource(context.Background(), catalogSource(repo, "."))
			if err != nil {
				t.Fatal(err)
			}
			if err = client.Release(previewID); err != nil {
				t.Fatal(err)
			}
			for i := range source.Catalog {
				source.Catalog[i].New = false
			}
			oldAlpha := partialCatalogEntry(t, source, "skills/alpha")
			oldBeta := partialCatalogEntry(t, source, "nested/beta")
			alphaDir := filepath.Join(repo, "skills", "alpha")
			switch problem {
			case "metadata":
				err = os.WriteFile(filepath.Join(alphaDir, "SKILL.md"), []byte("Malformed document without frontmatter\n"), 0644)
			case "skill-size":
				writeLargeTestFile(t, filepath.Join(alphaDir, "large.bin"), maxRepositoryBytes+1)
			case "skill-document-symlink":
				if err = os.Remove(filepath.Join(alphaDir, "SKILL.md")); err == nil {
					err = testutil.Symlink(t, "../../nested/beta/SKILL.md", filepath.Join(alphaDir, "SKILL.md"))
				}
			case "skill-asset-symlink":
				if err = os.MkdirAll(filepath.Join(alphaDir, "references"), 0755); err == nil {
					err = testutil.Symlink(t, "../../../nested/beta/SKILL.md", filepath.Join(alphaDir, "references", "alias.md"))
				}
			case "skill-directory-symlink":
				if err = os.RemoveAll(alphaDir); err == nil {
					err = testutil.Symlink(t, "../../nested/beta", alphaDir)
				}
			case "ancestor-directory-symlink":
				if err = os.RemoveAll(filepath.Join(repo, "skills")); err == nil {
					err = testutil.Symlink(t, "nested", filepath.Join(repo, "skills"))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			writeTestSkill(t, repo, "nested/beta", "beta", "Updated normal neighbor", "Beta version two")
			testGit(t, repo, "add", "-A")
			testGit(t, repo, "commit", "-m", "Skip one invalid skill and update its neighbor")
			updated, updatedID, err := client.RefreshSource(context.Background(), source)
			if err != nil {
				t.Fatal("invalid skill blocked normal source update", err)
			}
			defer client.Release(updatedID)
			if updated.Revision == source.Revision || updated.LastError != "" {
				t.Fatalf("partial refresh did not advance successfully: %+v", updated)
			}
			alpha := partialCatalogEntry(t, updated, "skills/alpha")
			if alpha.SkipReason == "" || alpha.Removed {
				t.Fatalf("skipped skill incorrectly removed: %+v", alpha)
			}
			alpha.SkipReason = ""
			if alpha != oldAlpha {
				t.Fatalf("skipped skill lost previously reviewed metadata: got %+v, want %+v", alpha, oldAlpha)
			}
			beta := partialCatalogEntry(t, updated, "nested/beta")
			if beta.ContentHash == oldBeta.ContentHash || beta.Revision != updated.Revision || beta.SkipReason != "" || beta.Removed {
				t.Fatalf("normal neighbor was not updated: %+v", beta)
			}
			preview, err := client.DescribePreview(updatedID)
			if err != nil || len(preview.Candidates) != 1 || preview.Candidates[0].Subpath != "nested/beta" {
				t.Fatalf("preview offers skipped skill: %+v %v", preview, err)
			}
			if problem != "skill-directory-symlink" && problem != "ancestor-directory-symlink" && (len(preview.Skipped) == 0 || preview.Skipped[0].Subpath == "" || preview.Skipped[0].Reason == "") {
				t.Fatalf("preview did not explain skipped skill: %+v", preview.Skipped)
			}
			if _, _, err = client.PrepareSourceSync(context.Background(), updated, []string{"skills/alpha"}); err == nil {
				t.Fatal("explicit synchronization accepted a skipped skill")
			}
			if err = os.RemoveAll(filepath.Join(repo, "skills")); err != nil {
				t.Fatal(err)
			}
			writeTestSkill(t, repo, "skills/alpha", "alpha", "Recovered alpha", "Recovered content")
			testGit(t, repo, "add", "-A")
			testGit(t, repo, "commit", "-m", "Restore alpha as valid skill")
			recovered, recoveredID, err := client.RefreshSource(context.Background(), updated)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Release(recoveredID)
			alpha = partialCatalogEntry(t, recovered, "skills/alpha")
			if alpha.SkipReason != "" || alpha.Removed || alpha.Revision != recovered.Revision || alpha.ContentHash == oldAlpha.ContentHash {
				t.Fatalf("recovered skill still skipped: %+v", alpha)
			}
			if len(recovered.Warnings) != 0 {
				t.Fatalf("recovery retained stale warnings: %+v", recovered.Warnings)
			}
			testGit(t, repo, "rm", "-r", "skills")
			testGit(t, repo, "commit", "-m", "Delete alpha upstream")
			removed, removedID, err := client.RefreshSource(context.Background(), recovered)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Release(removedID)
			alpha = partialCatalogEntry(t, removed, "skills/alpha")
			if !alpha.Removed || alpha.SkipReason != "" {
				t.Fatalf("real upstream deletion was not distinguished from skipped skill: %+v", alpha)
			}
		})
	}
}
