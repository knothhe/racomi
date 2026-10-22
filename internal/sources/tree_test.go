package sources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLargeTestFile(t *testing.T, filename string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	truncateErr := file.Truncate(size)
	closeErr := file.Close()
	if truncateErr != nil || closeErr != nil {
		t.Fatalf("create large fixture: %v %v", truncateErr, closeErr)
	}
}

func TestLargeNonSkillContentDoesNotBlockPreviewOrRefresh(t *testing.T) {
	repo := testRepo(t)
	writeLargeTestFile(t, filepath.Join(repo, "assets", "large.bin"), maxRepositoryBytes+1)
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add unrelated large repository asset")
	client := New(t.TempDir())
	preview, err := client.Preview(context.Background(), repo, "main", ".")
	if err != nil {
		t.Fatal("unrelated repository content blocked skill discovery", err)
	}
	defer client.Release(preview.ID)
	if len(preview.Candidates) != 2 || len(preview.Skipped) != 0 || len(preview.Warnings) != 0 {
		t.Fatalf("unrelated content changed usable skills: %+v", preview)
	}
	root := client.previews[preview.ID].root
	if _, err = os.Stat(filepath.Join(root, "assets", "large.bin")); !os.IsNotExist(err) {
		t.Fatalf("unrelated large blob was materialized: %v", err)
	}
	source, id, err := client.RefreshSource(context.Background(), catalogSource(repo, "."))
	if err != nil || len(source.Catalog) != 2 || source.LastError != "" {
		t.Fatalf("unrelated content blocked source refresh: %+v %v", source, err)
	}
	defer client.Release(id)

	// A root SKILL.md makes the entire repository its skill content. It must
	// be skipped as oversized, while nested, complete skill trees stay usable.
	writeTestSkill(t, repo, ".", "root-skill", "Whole repository skill", "Root")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Identify the repository root as a skill")
	updated, id, err := client.RefreshSource(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(id)
	if rootSkill := partialCatalogEntry(t, updated, "."); rootSkill.Removed || !strings.Contains(rootSkill.SkipReason, "100 MiB") {
		t.Fatalf("oversized repository root skill was truncated or treated as removed: %+v", rootSkill)
	}
	if got := client.previews[id].preview; len(got.Candidates) != 2 || len(got.Skipped) != 1 || got.Skipped[0].Subpath != "." {
		t.Fatalf("root skill limit excluded usable nested skills: %+v", got)
	}
}

func TestExplicitScopeAndSkillNameGetPreviewBudgetFirst(t *testing.T) {
	repo := testRepo(t)
	writeTestSkill(t, repo, "a-large", "unrelated", "Large unrelated skill", "Other")
	writeTestSkill(t, repo, "z-directory", "wanted-name", "Explicitly selected skill", "Wanted")
	writeLargeTestFile(t, filepath.Join(repo, "a-large", "asset.bin"), 90<<20)
	writeLargeTestFile(t, filepath.Join(repo, "z-directory", "asset.bin"), 20<<20)
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add skills exceeding the combined preview budget")
	client := New(t.TempDir())
	for _, selector := range []string{"subpath", "frontmatter-name"} {
		t.Run(selector, func(t *testing.T) {
			spec := sourceSpec{source: repo, ref: "main", subpath: "z-directory"}
			if selector == "frontmatter-name" {
				// This is the spec produced by a skills.sh owner/repo/skill link.
				spec.subpath, spec.skill = ".", "wanted-name"
			}
			client.mu.Lock()
			preview, err := client.previewLocked(context.Background(), spec, true)
			client.mu.Unlock()
			if err != nil {
				t.Fatal("outside skill consumed the explicit selection's budget", err)
			}
			defer client.Release(preview.ID)
			if len(preview.Candidates) != 1 || preview.Candidates[0].Name != "wanted-name" {
				t.Fatalf("explicit selector did not receive a complete skill: %+v", preview)
			}
			candidates, err := client.Candidates(preview.ID, []string{"z-directory"})
			if err != nil || len(candidates) != 1 {
				t.Fatalf("explicit skill was not selectable: %+v %v", candidates, err)
			}
			info, err := os.Stat(filepath.Join(candidates[0].Path, "asset.bin"))
			if err != nil || info.Size() != 20<<20 {
				t.Fatalf("skill was offered without its complete asset: %v %v", info, err)
			}
			// Installing a discovery preview widens its catalog to the source
			// scope. Omitted outside skills must be skipped, never removed.
			source, err := client.CatalogForPreview(preview.ID, catalogSource(repo, "."))
			if err != nil {
				t.Fatal(err)
			}
			other := partialCatalogEntry(t, source, "a-large")
			if other.Removed || !strings.Contains(other.SkipReason, "100 MiB") {
				t.Fatalf("outside budget limit lost its skipped identity: %+v", other)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(repo, "a-large", "SKILL.md"), []byte("Malformed frontmatter\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Make the large skill invalid without changing its asset")
	preview, err := client.Preview(context.Background(), repo, "main", ".")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(preview.ID)
	if len(preview.Candidates) != 3 || len(preview.Skipped) != 1 || preview.Skipped[0].Subpath != "a-large" {
		t.Fatalf("invalid metadata consumed a healthy neighbor's budget: %+v", preview)
	}
}

func TestSkillSelectorAmbiguityCannotBeHiddenByPreviewBudget(t *testing.T) {
	repo := testRepo(t)
	writeTestSkill(t, repo, "skills/a/wanted", "other", "Directory name match", "Other")
	writeTestSkill(t, repo, "skills/z/directory", "wanted", "Frontmatter name match", "Wanted")
	writeLargeTestFile(t, filepath.Join(repo, "skills", "a", "wanted", "asset.bin"), 60<<20)
	writeLargeTestFile(t, filepath.Join(repo, "skills", "z", "directory", "asset.bin"), 60<<20)
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add ambiguous skills.sh matches exceeding combined budget")
	client := New(t.TempDir())
	spec := sourceSpec{source: repo, ref: "main", subpath: ".", skill: "wanted"}
	client.mu.Lock()
	_, err := client.previewLocked(context.Background(), spec, true)
	client.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "matches multiple repository skills") {
		t.Fatalf("budget skip hid the ambiguous skills.sh name: %v", err)
	}
	if len(client.previews) != 0 {
		t.Fatal("ambiguous selector left an installable preview")
	}
}

func TestInvalidNestedMetadataKeepsCompleteParentSkillAndDeletionSemantics(t *testing.T) {
	repo := testRepo(t)
	writeTestSkill(t, repo, "parent", "parent", "Valid parent skill", "Parent")
	writeTestSkill(t, repo, "parent/nested", "nested", "Nested skill", "Nested")
	if err := os.WriteFile(filepath.Join(repo, "parent", "nested", "SKILL.md"), []byte("Not skill frontmatter\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Keep an invalid nested marker as ordinary parent content")
	client := New(t.TempDir())
	source, id, err := client.RefreshSource(context.Background(), catalogSource(repo, "."))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(id)
	parent := partialCatalogEntry(t, source, "parent")
	child := partialCatalogEntry(t, source, "parent/nested")
	if parent.SkipReason != "" || parent.ContentHash == "" || child.SkipReason == "" {
		t.Fatalf("nested metadata polluted a valid parent: parent=%+v child=%+v", parent, child)
	}
	candidates, err := client.Candidates(id, []string{"parent"})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("parent skill was not offered: %+v %v", candidates, err)
	}
	body, err := os.ReadFile(filepath.Join(candidates[0].Path, "nested", "SKILL.md"))
	if err != nil || string(body) != "Not skill frontmatter\n" {
		t.Fatalf("invalid nested marker was omitted from parent content: %q %v", body, err)
	}
	testGit(t, repo, "rm", "parent/SKILL.md")
	testGit(t, repo, "commit", "-m", "Remove the parent skill marker")
	updated, updatedID, err := client.RefreshSource(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(updatedID)
	parent = partialCatalogEntry(t, updated, "parent")
	if !parent.Removed || parent.SkipReason != "" {
		t.Fatalf("metadata-only nested skip hid a real parent deletion: %+v", parent)
	}
}

func TestExtractionPlanKeepsWholeSkillsAndBoundsFiles(t *testing.T) {
	regular := func(name string, size int64) repositoryEntry {
		return repositoryEntry{Path: name, Kind: "blob", Mode: "100644", Size: size}
	}
	plan := func(entries []repositoryEntry) ([]string, []repositoryIssue) {
		roots, issues := discoverSkillRoots(entries, nil, ".")
		return allocateSkillExtraction(entries, issues, roots)
	}
	t.Run("cumulative-skill-size", func(t *testing.T) {
		entries := []repositoryEntry{
			regular("alpha/SKILL.md", 20), regular("alpha/a", 60<<20), regular("alpha/b", 60<<20),
			regular("beta/SKILL.md", 20), regular("beta/script.sh", 30),
		}
		files, issues := plan(entries)
		if strings.Join(files, ",") != "beta/SKILL.md,beta/script.sh" || len(issues) != 1 || issues[0].Path != "alpha/SKILL.md" {
			t.Fatalf("oversized skill was truncated or blocked its neighbor: %v %+v", files, issues)
		}
	})
	t.Run("file-count", func(t *testing.T) {
		entries := []repositoryEntry{regular("alpha/SKILL.md", 10), regular("beta/SKILL.md", 10)}
		for i := 0; i < maxRepositoryEntries; i++ {
			entries = append(entries, regular(fmt.Sprintf("alpha/file-%d", i), 0))
		}
		files, issues := plan(entries)
		if len(files) != 1 || files[0] != "beta/SKILL.md" || len(issues) != 1 || !strings.Contains(issues[0].Reason, "20,000") {
			t.Fatalf("skill file-count limit blocked its neighbor: %d %+v", len(files), issues)
		}
	})
	t.Run("oversized-marker", func(t *testing.T) {
		entries := []repositoryEntry{regular("alpha/SKILL.md", 2<<20), regular("beta/SKILL.md", 10)}
		files, issues := plan(entries)
		if len(files) != 1 || files[0] != "beta/SKILL.md" || len(issues) == 0 || !strings.Contains(issues[0].Reason, "1 MiB") {
			t.Fatalf("oversized marker consumed preview resources: %v %+v", files, issues)
		}
	})
}

func TestGitTreeInspectionRejectsUnsafeAndMalformedEntries(t *testing.T) {
	object := strings.Repeat("a", 40)
	for _, name := range []string{"../escape", ".Git/config", "dir/./file", "dir\\file", "line\nbreak"} {
		_, _, err := readCommitTree(strings.NewReader("100644 blob " + object + " 1\t" + name + "\x00"))
		if err == nil {
			t.Errorf("accepted unsafe tree path %q", name)
		}
	}
	for _, input := range []string{
		"100644 blob " + object + " 1\tmissing-terminator",
		"100644 blob " + object + " -1\tnegative\x00",
		"100644 blob invalid 1\tinvalid-object\x00",
	} {
		if _, _, err := readCommitTree(strings.NewReader(input)); err == nil {
			t.Errorf("accepted malformed tree record %q", input)
		}
	}
	input := "120000 blob " + object + " 8\talias\x00" + "160000 commit " + object + " -\tsubmodule\x00"
	entries, issues, err := readCommitTree(strings.NewReader(input))
	if err != nil || len(entries) != 2 || len(issues) != 2 || issues[0].Reason != "symlink is not supported" {
		t.Fatalf("links/submodules were not recorded safely: %+v %+v %v", entries, issues, err)
	}
}
