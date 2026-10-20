package sources

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// This reproduces the unindented prose and embedded colon shipped in
// Leonxlnx/taste-skill revision a9c73c3. The source file must stay unchanged.
const tasteSkillFrontmatter = "---\nname: design-taste-frontend\ndescription: tasteskill v2, the anti-slop frontend skill.\nUse it when building websites.\nAlso for app screens: dashboards and settings.\n---\n# Taste skill\n"

func TestFrontmatterSupportsPlainDescriptionPunctuationAndContinuation(t *testing.T) {
	for _, row := range []struct {
		name, body, description string
	}{
		{"taste skill", tasteSkillFrontmatter, "tasteskill v2, the anti-slop frontend skill. Use it when building websites. Also for app screens: dashboards and settings."},
		{"inline colon", "---\nname: alpha\ndescription: Build interfaces: dashboards and settings.\nlicense: MIT\nmetadata:\n  version: 1\n---\n", "Build interfaces: dashboards and settings."},
		{"indented prose", "---\nname: alpha\ndescription: Build interfaces\n  Use it for screens: dashboards and settings.\n---\n", "Build interfaces Use it for screens: dashboards and settings."},
	} {
		t.Run(row.name, func(t *testing.T) {
			name, description, err := metadata([]byte(row.body))
			if err != nil || name == "" || description != row.description {
				t.Fatalf("plain description was not preserved: %q %q %v", name, description, err)
			}
		})
	}
}

func TestPlainDescriptionCompatibilityKeepsMalformedMetadataInvalid(t *testing.T) {
	for _, row := range []struct{ name, fields string }{
		{"unsafe name", "name: ../escape\ndescription: Build screens: dashboards"},
		{"numeric name", "name: 42\ndescription: Build screens: dashboards"},
		{"duplicate name", "name: alpha\nname: beta\ndescription: Build screens: dashboards"},
		{"duplicate description", "name: alpha\ndescription: Build screens: dashboards\ndescription: Other text"},
		{"mapping", "name: alpha\ndescription:\n  screens: dashboards"},
		{"nested mapping", "name: alpha\ndescription: Build interfaces\n  screens: dashboards"},
		{"sequence", "name: alpha\ndescription: Build interfaces\n- dashboards"},
		{"unterminated quote", "name: alpha\ndescription: \"Build screens: dashboards"},
		{"unterminated collection", "name: alpha\ndescription: [Build screens: dashboards"},
		{"malformed other field", "name: alpha\ndescription: Build screens: dashboards\nmetadata: [unclosed"},
		{"typed description", "name: alpha\ndescription: 42\nUnindented prose"},
		{"empty description", "name: alpha\ndescription:\nUnindented prose"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if _, _, err := metadata([]byte("---\n" + row.fields + "\n---\n")); err == nil {
				t.Fatal("compatibility accepted malformed metadata")
			}
		})
	}
}

func TestPlainDescriptionCompatibilitySurvivesGitPreviewWithoutRewritingSkill(t *testing.T) {
	repo := testRepo(t)
	path := filepath.Join(repo, "skills", "taste-skill")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(tasteSkillFrontmatter), 0644); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add plain prose frontmatter")
	client := New(t.TempDir())
	preview, err := client.Preview(context.Background(), repo, "main", "skills/taste-skill")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(preview.ID)
	if len(preview.Candidates) != 1 || len(preview.Skipped) != 0 || preview.Candidates[0].Name != "design-taste-frontend" {
		t.Fatalf("compatible skill was skipped: %+v", preview)
	}
	selected, err := client.Candidates(preview.ID, []string{"skills/taste-skill"})
	if err != nil || len(selected) != 1 {
		t.Fatalf("compatible skill cannot be imported: %+v %v", selected, err)
	}
	body, err := os.ReadFile(filepath.Join(selected[0].Path, "SKILL.md"))
	if err != nil || !bytes.Equal(body, []byte(tasteSkillFrontmatter)) {
		t.Fatalf("compatibility rewrote the reviewed skill: %q %v", body, err)
	}
}
