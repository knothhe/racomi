package sources

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveRecordsUnsupportedEntriesWithoutMaterializingThem(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, header := range []*tar.Header{
		{Name: "AGENTS.md", Linkname: "../../outside", Typeflag: tar.TypeSymlink},
		{Name: "hard-link", Linkname: "AGENTS.md", Typeflag: tar.TypeLink},
		{Name: "pipe", Typeflag: tar.TypeFifo},
		{Name: "regular", Mode: 0644, Typeflag: tar.TypeReg, Size: 4},
	} {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Name == "regular" {
			if _, err := writer.Write([]byte("safe")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	issues, err := unpackArchive(&archive, root)
	if err != nil || len(issues) != 3 {
		t.Fatalf("unsupported entries should be recorded: %+v %v", issues, err)
	}
	for _, issue := range issues {
		if issue.Reason == "" {
			t.Fatal("missing issue reason")
		}
		if _, err := os.Lstat(filepath.Join(root, issue.Path)); !os.IsNotExist(err) {
			t.Fatalf("unsupported entry %q was materialized: %v", issue.Path, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(root, "regular")); err != nil || string(body) != "safe" {
		t.Fatalf("safe regular entry was not extracted: %q %v", body, err)
	}
}

func TestArchiveStillEnforcesLimitsAndReadErrors(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		if err := writer.WriteHeader(&tar.Header{Name: "large", Typeflag: tar.TypeReg, Size: maxRepositoryBytes + 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := unpackArchive(&archive, t.TempDir()); err == nil || !strings.Contains(err.Error(), "100 MiB") {
			t.Fatalf("large entry did not fail fatally: %v", err)
		}
	})
	t.Run("unsupported-entry-count", func(t *testing.T) {
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		for i := 0; i <= maxRepositoryEntries; i++ {
			if err := writer.WriteHeader(&tar.Header{Name: fmt.Sprintf("link-%d", i), Typeflag: tar.TypeSymlink, Linkname: "outside"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := unpackArchive(&archive, t.TempDir()); err == nil || !strings.Contains(err.Error(), "too many files") {
			t.Fatalf("unsupported entries bypassed archive count limit: %v", err)
		}
	})
	t.Run("truncated-content", func(t *testing.T) {
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		if err := writer.WriteHeader(&tar.Header{Name: "truncated", Typeflag: tar.TypeReg, Size: 8}); err != nil {
			t.Fatal(err)
		}
		if _, err := unpackArchive(&archive, t.TempDir()); err == nil {
			t.Fatal("truncated archive was accepted")
		}
	})
}

func TestScanValidatesFullSkillTreeIncludingIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "alpha", "alpha", "Unsafe skill", "Alpha")
	writeTestSkill(t, root, "beta", "beta", "Safe neighbor", "Beta")
	if err := os.MkdirAll(filepath.Join(root, "alpha", "node_modules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../beta/SKILL.md", filepath.Join(root, "alpha", "node_modules", "link")); err != nil {
		t.Fatal(err)
	}
	items, skipped, _, err := scanSkills(root, ".", "revision", nil, false)
	if err != nil || len(items) != 1 || items[0].Subpath != "beta" || len(items[0].ContentHash) != 64 || len(skipped) != 1 || skipped[0].Subpath != "alpha" {
		t.Fatalf("ignored child bypassed full skill validation: %+v %+v %v", items, skipped, err)
	}
}

func TestScanMarksLinkedSkillDocumentAndAllowsAllSkippedCatalog(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	issues := []repositoryIssue{{Path: "skills/alpha/SKILL.md", Reason: "symlink is not supported"}}
	items, skipped, warnings, err := scanSkills(root, ".", "revision", issues, true)
	if err != nil || len(items) != 0 || len(skipped) != 1 || skipped[0].Subpath != "skills/alpha" || len(warnings) != 0 {
		t.Fatalf("unmaterialized SKILL.md lost its identity: %+v %+v %+v %v", items, skipped, warnings, err)
	}
	if _, _, _, err := scanSkills(root, ".", "revision", issues, false); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ordinary preview should explain an all-skipped selection: %v", err)
	}
}

func TestScanDoesNotFollowSelectedScopeSymlinks(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	writeTestSkill(t, external, "alpha", "alpha", "External skill", "Must not be read")
	if err := os.Symlink(external, filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"skills", "skills/alpha"} {
		items, _, warnings, err := scanSkills(root, scope, "revision", nil, true)
		if err != nil || len(items) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "symlink") {
			t.Fatalf("selected scope %q followed a symlink: %+v %+v %v", scope, items, warnings, err)
		}
	}
}
