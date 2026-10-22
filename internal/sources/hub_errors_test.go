package sources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func previewHubSelectorFailure(t *testing.T, repo, selector string) error {
	t.Helper()
	client := New(t.TempDir())
	client.mu.Lock()
	preview, err := client.previewLocked(context.Background(), sourceSpec{
		source: repo, ref: "main", subpath: ".", skill: selector,
	}, true)
	client.mu.Unlock()
	if err == nil {
		_ = client.Release(preview.ID)
		t.Fatalf("expected Hub selector %q to fail: %+v", selector, preview)
	}
	if preview.ID != "" || len(client.previews) != 0 {
		t.Fatalf("failed Hub selector left an installable preview: %+v", preview)
	}
	entries, readErr := os.ReadDir(filepath.Join(client.cacheDir, "previews"))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed Hub selector left extracted preview content: %+v", entries)
	}
	return err
}

func TestHubSkillSelectionReportsMissingSelector(t *testing.T) {
	err := previewHubSelectorFailure(t, testRepo(t), "polish")
	want := `skills.sh skill "polish" was not found in the current repository revision; the index may be outdated or the skill may have been renamed or removed; preview the repository to see available skills`
	if err.Error() != want {
		t.Fatalf("missing Hub skill did not receive a distinct diagnostic: %v", err)
	}
	if strings.Contains(err.Error(), "did not match exactly one") {
		t.Fatalf("missing Hub skill was reported as ambiguous: %v", err)
	}
}

func TestHubSkillSelectionReportsAmbiguousPathsInStableOrder(t *testing.T) {
	repo := testRepo(t)
	for _, subpath := range []string{"skills/z-polish", "skills/a-polish"} {
		writeTestSkill(t, repo, subpath, "polish", "An ambiguous Hub selector", "Distinct skill content")
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add ambiguous Hub selector paths")
	err := previewHubSelectorFailure(t, repo, "polish")
	message := err.Error()
	want := `skills.sh skill "polish" matches multiple repository skills: "skills/a-polish", "skills/z-polish"; specify a skill subdirectory`
	if message != want {
		t.Fatalf("ambiguous Hub skill did not report its paths in stable order: %v", err)
	}
	if strings.Contains(message, "not found") || strings.Contains(message, "did not match exactly one") {
		t.Fatalf("ambiguous Hub skill did not receive a distinct diagnostic: %v", err)
	}
}

func TestHubSkillSelectionPreservesInvalidMetadataReason(t *testing.T) {
	repo := testRepo(t)
	writeTestSkill(t, repo, "skills/polish", "polish", `""`, "Requested skill with an empty description")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add invalid requested Hub skill metadata")
	err := previewHubSelectorFailure(t, repo, "polish")
	for _, detail := range []string{`skills.sh skill "polish" is unavailable:`, "skills/polish", "skill description cannot be empty"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("unusable Hub skill lost diagnostic detail %q: %v", detail, err)
		}
	}
	if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "did not match exactly one") {
		t.Fatalf("unusable Hub skill was reported as missing or ambiguous: %v", err)
	}
}

func TestHubSkillSelectionDoesNotUseFixtureWhenStandardSkillsExist(t *testing.T) {
	repo := testRepo(t)
	writeTestSkill(t, repo, "skills/impeccable", "impeccable", "Published skill", "Current repository skill")
	writeTestSkill(t, repo, "tests/fixture/audit", "audit", "Internal test fixture", "Not a published skill")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add a stale Hub selector that only matches an internal fixture")
	err := previewHubSelectorFailure(t, repo, "audit")
	if !strings.Contains(err.Error(), `skills.sh skill "audit" was not found in the current repository revision`) {
		t.Fatalf("internal fixture was not excluded from standard-directory Hub discovery: %v", err)
	}
}

func TestHubSkillSelectionUsesUniqueCustomDirectoryFallback(t *testing.T) {
	repo := testRepo(t)
	if err := os.RemoveAll(filepath.Join(repo, "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, "nested")); err != nil {
		t.Fatal(err)
	}
	const selected = "custom/catalog/polish"
	writeTestSkill(t, repo, selected, "polish", "Custom skill location", "Requested fallback skill")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Move repository skills to a custom directory")
	client := New(t.TempDir())
	client.mu.Lock()
	preview, err := client.previewLocked(context.Background(), sourceSpec{
		source: repo, ref: "main", subpath: ".", skill: "polish",
	}, true)
	client.mu.Unlock()
	if err != nil {
		t.Fatal("unique custom-directory skill could not be previewed", err)
	}
	t.Cleanup(func() { _ = client.Release(preview.ID) })
	if len(preview.Candidates) != 1 || preview.Candidates[0].Subpath != selected || preview.Candidates[0].Name != "polish" {
		t.Fatalf("custom-directory fallback selected the wrong skill: %+v", preview)
	}
	candidates, err := client.Candidates(preview.ID, []string{selected})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("custom-directory fallback was not installable: %+v %v", candidates, err)
	}
}
