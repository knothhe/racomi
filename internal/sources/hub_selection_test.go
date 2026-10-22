package sources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHubSkillVariant(t *testing.T, repo, subpath, marker string) {
	t.Helper()
	writeTestSkill(t, repo, subpath, "impeccable", "Hub selection fixture", marker)
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(subpath), "reference.txt"), []byte(marker), 0644); err != nil {
		t.Fatal(err)
	}
}

func previewHubSkill(t *testing.T, client *Client, repo, subpath string) (Preview, error) {
	t.Helper()
	// skills.sh owner/repository/impeccable resolves to this repository and
	// name selector; using a local Git fixture keeps the test deterministic.
	spec := sourceSpec{source: repo, ref: "main", subpath: subpath, skill: "impeccable"}
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.previewLocked(context.Background(), spec, true)
}

func assertHubSkillSelection(t *testing.T, client *Client, preview Preview, scope, selected, marker string, unoffered ...string) {
	t.Helper()
	t.Cleanup(func() { _ = client.Release(preview.ID) })
	if preview.Subpath != scope || len(preview.Candidates) != 1 || preview.Candidates[0].Subpath != selected || preview.Candidates[0].Name != "impeccable" {
		t.Fatalf("Hub selector lost its scope or selected the wrong variant: %+v", preview)
	}
	candidates, err := client.Candidates(preview.ID, []string{selected})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("selected Hub skill was not installable: %+v %v", candidates, err)
	}
	body, err := os.ReadFile(filepath.Join(candidates[0].Path, "SKILL.md"))
	if err != nil || !strings.Contains(string(body), marker) {
		t.Fatalf("Hub preview selected another variant's document: %q %v", body, err)
	}
	reference, err := os.ReadFile(filepath.Join(candidates[0].Path, "reference.txt"))
	if err != nil || string(reference) != marker {
		t.Fatalf("Hub preview did not keep the selected variant's complete content: %q %v", reference, err)
	}
	for _, subpath := range unoffered {
		if _, err := client.Candidates(preview.ID, []string{subpath}); err == nil {
			t.Fatalf("Hub preview allowed an unoffered variant %q", subpath)
		}
	}
}

func TestHubSkillSelectionPrefersAgentDirectoryToDistributionCopies(t *testing.T) {
	repo := testRepo(t)
	variants := []string{
		".agents/skills/impeccable",
		".claude/skills/impeccable",
		"plugin/skills/impeccable",
		"tests/oracle/workspaces/ctx-pin/.agents/skills/impeccable",
	}
	for _, subpath := range variants {
		writeHubSkillVariant(t, repo, subpath, "variant from "+subpath)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add distinct agent, plugin and test skill copies")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, ".")
	if err != nil {
		t.Fatal("standard agent skill was made ambiguous by distribution copies", err)
	}
	assertHubSkillSelection(t, client, preview, ".", variants[0], "variant from "+variants[0], variants[1:]...)
}

func TestHubSkillSelectionPrefersSkillsDirectoryToAgentDirectory(t *testing.T) {
	repo := testRepo(t)
	writeHubSkillVariant(t, repo, "skills/impeccable", "Primary source skill")
	writeHubSkillVariant(t, repo, ".agents/skills/impeccable", "Generated agent skill")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add source and agent skill variants")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, ".")
	if err != nil {
		t.Fatal("source skill was made ambiguous by an agent copy", err)
	}
	assertHubSkillSelection(t, client, preview, ".", "skills/impeccable", "Primary source skill", ".agents/skills/impeccable")
}

func TestHubSkillSelectionRejectsAmbiguousStandardDirectory(t *testing.T) {
	repo := testRepo(t)
	writeHubSkillVariant(t, repo, "skills/first", "First source skill")
	writeHubSkillVariant(t, repo, "skills/second", "Second source skill")
	writeHubSkillVariant(t, repo, ".agents/skills/impeccable", "Lower priority copy")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add ambiguous source skills and a lower priority copy")
	client := New(t.TempDir())
	_, err := previewHubSkill(t, client, repo, ".")
	if err == nil || !strings.Contains(err.Error(), "matches multiple repository skills") {
		t.Fatalf("ambiguous standard-directory skills were selected arbitrarily: %v", err)
	}
	if len(client.previews) != 0 {
		t.Fatal("ambiguous Hub selection left an installable preview")
	}
}

func TestHubSkillSelectionHonorsExplicitScope(t *testing.T) {
	repo := testRepo(t)
	outside := "skills/impeccable"
	selected := "selected/.agents/skills/impeccable"
	fixture := "selected/tests/fixture/impeccable"
	writeHubSkillVariant(t, repo, outside, "Outside source skill")
	writeHubSkillVariant(t, repo, selected, "Scoped agent skill")
	writeHubSkillVariant(t, repo, fixture, "Scoped test fixture")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add scoped agent skill and out-of-scope source skill")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, "selected")
	if err != nil {
		t.Fatal("Hub selection did not resolve within the explicit scope", err)
	}
	assertHubSkillSelection(t, client, preview, "selected", selected, "Scoped agent skill", outside, fixture)
}

func TestGitRepositoryPreviewPreservesDistinctHubSkillCopies(t *testing.T) {
	repo := testRepo(t)
	variants := []string{
		"skills/impeccable",
		".agents/skills/impeccable",
		".claude/skills/impeccable",
		"plugin/skills/impeccable",
		"tests/fixture/impeccable",
	}
	for _, subpath := range variants {
		writeHubSkillVariant(t, repo, subpath, "variant from "+subpath)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add skill copies for unrestricted Git preview")
	client := New(t.TempDir())
	preview, err := client.Preview(context.Background(), repo, "main", ".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Release(preview.ID) })
	if preview.Subpath != "." || len(preview.Candidates) != len(variants)+2 {
		t.Fatalf("unrestricted Git preview silently filtered skills: %+v", preview)
	}
	for _, subpath := range variants {
		candidates, err := client.Candidates(preview.ID, []string{subpath})
		if err != nil || len(candidates) != 1 {
			t.Fatalf("Git preview did not offer %q: %+v %v", subpath, candidates, err)
		}
		body, err := os.ReadFile(filepath.Join(candidates[0].Path, "reference.txt"))
		if err != nil || string(body) != "variant from "+subpath {
			t.Fatalf("Git preview changed %q: %q %v", subpath, body, err)
		}
	}
}

func TestHubSkillSelectionDoesNotReplaceUnsafePreferredSkill(t *testing.T) {
	repo := testRepo(t)
	preferred := ".agents/skills/impeccable"
	writeHubSkillVariant(t, repo, preferred, "Preferred skill with an unsupported asset")
	writeHubSkillVariant(t, repo, ".claude/skills/impeccable", "Different lower priority variant")
	if err := os.Symlink("reference.txt", filepath.Join(repo, filepath.FromSlash(preferred), "linked.txt")); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Make the preferred skill unsafe to install")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, ".")
	if err == nil {
		_ = client.Release(preview.ID)
		t.Fatalf("unsafe preferred skill was silently replaced by a different variant: %+v", preview)
	}
	if !strings.Contains(err.Error(), "is unavailable:") || !strings.Contains(err.Error(), "symlink is not supported") || !strings.Contains(err.Error(), "linked.txt") {
		t.Fatalf("unsafe preferred skill lost its concrete failure reason: %v", err)
	}
	if len(client.previews) != 0 {
		t.Fatal("unsafe preferred skill left an installable preview")
	}
}

func TestHubSkillSelectionIgnoresUnrelatedSymlinkMarker(t *testing.T) {
	repo := testRepo(t)
	selected := ".agents/skills/impeccable"
	writeHubSkillVariant(t, repo, selected, "Healthy requested skill")
	unrelated := filepath.Join(repo, "skills", "unrelated")
	if err := os.MkdirAll(unrelated, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "document.md"), []byte("Unrelated linked document"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("document.md", filepath.Join(unrelated, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add an unrelated linked skill marker in a preferred container")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, ".")
	if err != nil {
		t.Fatal("unrelated symlink marker displaced the requested skill", err)
	}
	assertHubSkillSelection(t, client, preview, ".", selected, "Healthy requested skill", "skills/unrelated")
}

func TestHubSkillSelectionDoesNotReplaceOversizedPreferredSkill(t *testing.T) {
	repo := testRepo(t)
	preferred := ".agents/skills/impeccable"
	writeHubSkillVariant(t, repo, preferred, "Preferred oversized skill")
	writeHubSkillVariant(t, repo, ".claude/skills/impeccable", "Different smaller variant")
	writeLargeTestFile(t, filepath.Join(repo, filepath.FromSlash(preferred), "large.bin"), maxRepositoryBytes+1)
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Make the preferred skill exceed the content limit")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, ".")
	if err == nil {
		_ = client.Release(preview.ID)
		t.Fatalf("oversized preferred skill was silently replaced by a different variant: %+v", preview)
	}
	if !strings.Contains(err.Error(), "is unavailable:") || !strings.Contains(err.Error(), "100 MiB") {
		t.Fatalf("oversized preferred skill lost its content limit reason: %v", err)
	}
	if len(client.previews) != 0 {
		t.Fatal("oversized preferred skill left an installable preview")
	}
}

func TestHubSkillSelectionDoesNotReplacePreferredSkillWithInvalidMetadata(t *testing.T) {
	repo := testRepo(t)
	writeTestSkill(t, repo, ".agents/skills/impeccable", "impeccable", `""`, "Preferred skill with invalid metadata")
	writeHubSkillVariant(t, repo, ".claude/skills/impeccable", "Different valid lower priority variant")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Invalidate the preferred Hub skill metadata")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, ".")
	if err == nil {
		_ = client.Release(preview.ID)
		t.Fatalf("invalid preferred skill was silently replaced by a different variant: %+v", preview)
	}
	if !strings.Contains(err.Error(), "is unavailable:") || !strings.Contains(err.Error(), "skill description cannot be empty") {
		t.Fatalf("invalid preferred skill lost its metadata failure reason: %v", err)
	}
	if len(client.previews) != 0 {
		t.Fatal("invalid preferred skill left an installable preview")
	}
}

func TestHubSkillSelectionDoesNotHideAmbiguityWhenOneMatchIsUnsafe(t *testing.T) {
	repo := testRepo(t)
	for _, subpath := range []string{"skills/first", "skills/second"} {
		writeHubSkillVariant(t, repo, subpath, "Variant from "+subpath)
	}
	if err := os.Symlink("reference.txt", filepath.Join(repo, "skills", "first", "linked.txt")); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add one unsafe and one healthy match for a Hub skill")
	client := New(t.TempDir())
	preview, err := previewHubSkill(t, client, repo, ".")
	if err == nil {
		_ = client.Release(preview.ID)
		t.Fatalf("an unsafe matching skill hid the selector's ambiguity: %+v", preview)
	}
	if !strings.Contains(err.Error(), "matches multiple repository skills") || !strings.Contains(err.Error(), "skills/first") || !strings.Contains(err.Error(), "skills/second") {
		t.Fatalf("ambiguous Hub selector lost its matching paths: %v", err)
	}
}
