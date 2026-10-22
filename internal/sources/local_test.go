package sources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"racomi/internal/testutil"
	skillstore "racomi/internal/skills"
)

func TestLocalPreviewWithoutGitKeepsReviewedContentAndOnlyOfferedPaths(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	writeTestSkill(t, root, "alpha", "alpha", "First local skill", "Original")
	writeTestSkill(t, root, "nested/beta", "beta", "Second local skill", "Beta")
	script := filepath.Join(root, "alpha", "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	client := New(t.TempDir())
	preview, err := client.PreviewLocal(context.Background(), root)
	if err != nil || !preview.Local || len(preview.Candidates) != 2 || preview.Ref != "" || preview.Revision != "" {
		t.Fatalf("ordinary directory did not produce a local preview: %+v %v", preview, err)
	}
	if _, err = client.Candidates(preview.ID, []string{"not-offered"}); err == nil {
		t.Fatal("local preview accepted a path it did not offer")
	}
	if selected, err := client.Candidates(preview.ID, nil); err != nil || len(selected) != 0 {
		t.Fatalf("empty local selection installed content: %+v %v", selected, err)
	}
	writeTestSkill(t, root, "alpha", "alpha", "Changed", "Changed after preview")
	selected, err := client.Candidates(preview.ID, []string{"alpha", "alpha"})
	if err != nil || len(selected) != 1 || selected[0].Skill.Source != "" || selected[0].Skill.Ref != "" || selected[0].Skill.Revision != "" {
		t.Fatalf("local selection did not stay independent: %+v %v", selected, err)
	}
	body, err := os.ReadFile(filepath.Join(selected[0].Path, "SKILL.md"))
	if err != nil || !strings.Contains(string(body), "Original") || strings.Contains(string(body), "Changed") {
		t.Fatalf("local preview read later source edits: %q %v", body, err)
	}
	hash, err := skillstore.HashDir(selected[0].Path)
	if err != nil || hash != selected[0].Skill.ContentHash {
		t.Fatalf("local cache changed permissions or content: %s %v", hash, err)
	}
	cache := selected[0].Path
	if err = client.Release(preview.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("local preview cache was not released: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "alpha", "SKILL.md")); err != nil {
		t.Fatalf("releasing the preview touched the user's directory: %v", err)
	}
}

func TestLocalPreviewSupportsSingleSkillAndReportsInvalidNeighbors(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "alpha", "alpha", "Valid", "Alpha")
	writeTestSkill(t, root, "linked", "linked", "Unsafe", "Linked")
	writeTestSkill(t, root, "invalid", "invalid", "Invalid", "Invalid")
	if err := testutil.Symlink(t, filepath.Join(root, "alpha", "SKILL.md"), filepath.Join(root, "linked", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid", "SKILL.md"), []byte("missing metadata"), 0644); err != nil {
		t.Fatal(err)
	}
	client := New(t.TempDir())
	preview, err := client.PreviewLocal(context.Background(), root)
	if err != nil || len(preview.Candidates) != 1 || preview.Candidates[0].Name != "alpha" || len(preview.Skipped) != 2 {
		t.Fatalf("local invalid skills hid a valid neighbor or became installable: %+v %v", preview, err)
	}
	if _, err := client.Candidates(preview.ID, []string{"linked"}); err == nil {
		t.Fatal("symlink-containing skill was offered for installation")
	}
	single, err := client.PreviewLocal(context.Background(), filepath.Join(root, "alpha"))
	if err != nil || len(single.Candidates) != 1 || single.Candidates[0].Subpath != "." {
		t.Fatalf("single local skill directory was not supported: %+v %v", single, err)
	}
	empty, err := client.PreviewLocal(context.Background(), t.TempDir())
	if err != nil || len(empty.Candidates) != 0 {
		t.Fatalf("empty directory did not produce an empty review: %+v %v", empty, err)
	}
}

func TestLocalPreviewRejectsInvalidInputsCancellationAndOversize(t *testing.T) {
	client := New(t.TempDir())
	file := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(file, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative/path", file, filepath.Join(t.TempDir(), "missing")} {
		if _, err := client.PreviewLocal(context.Background(), path); err == nil {
			t.Fatalf("invalid local path %q was accepted", path)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.PreviewLocal(ctx, t.TempDir()); err != context.Canceled {
		t.Fatalf("canceled local scan was accepted: %v", err)
	}
	large := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(large, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(large, maxRepositoryBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PreviewLocal(context.Background(), filepath.Dir(large)); err == nil || !strings.Contains(err.Error(), "100 MiB") {
		t.Fatalf("oversized local tree was accepted: %v", err)
	}
	if len(client.previews) != 0 {
		t.Fatal("failed local scans registered usable previews")
	}
}
