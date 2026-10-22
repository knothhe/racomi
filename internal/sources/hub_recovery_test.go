package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const recoveryHubLink = "https://skills.sh/team/repo/polish"
const recoveryGitSource = "https://github.com/team/repo.git"

func recoveryTestRepo(t *testing.T) string {
	t.Helper()
	repo := testRepo(t)
	for _, directory := range []string{"skills", "nested"} {
		if err := os.RemoveAll(filepath.Join(repo, directory)); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func cachedHubRecoveryClient(t *testing.T, repo string) (*Client, string) {
	t.Helper()
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add Hub repository recovery fixtures")
	revision := testGit(t, repo, "rev-parse", "HEAD")
	client := New(t.TempDir())
	hash := sha256.Sum256([]byte(recoveryGitSource))
	parent := filepath.Join(client.cacheDir, "repositories")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	// An exact cached commit avoids contacting GitHub while exercising the
	// public method's parsing, locking, extraction and stored allowlist.
	testGit(t, repo, "clone", "--bare", "--no-local", "--", repo, filepath.Join(parent, hex.EncodeToString(hash[:])))
	return client, revision
}

func TestPreviewHubRepositoryRecoversMissingSkillWithoutCopiesOrFixtures(t *testing.T) {
	for _, scope := range []string{".", "selected"} {
		t.Run(scope, func(t *testing.T) {
			repo := recoveryTestRepo(t)
			canonical := filepath.ToSlash(filepath.Join(scope, ".agents/skills/impeccable"))
			duplicate := filepath.ToSlash(filepath.Join(scope, ".claude/skills/impeccable"))
			fixture := filepath.ToSlash(filepath.Join(scope, "tests/fixture/audit"))
			standardSkipped := filepath.ToSlash(filepath.Join(scope, ".agents/skills/broken"))
			fixtureSkipped := filepath.ToSlash(filepath.Join(scope, "tests/fixture/broken"))
			writeTestSkill(t, repo, canonical, "impeccable", "Published canonical skill", "Use impeccable polish")
			writeTestSkill(t, repo, duplicate, "impeccable", "Generated tool adapter", "Generated variant")
			writeTestSkill(t, repo, fixture, "audit", "Internal fixture", "Not a published skill")
			for _, skipped := range []string{standardSkipped, fixtureSkipped} {
				writeTestSkill(t, repo, skipped, "broken", `""`, "Invalid metadata")
			}
			if scope != "." {
				writeTestSkill(t, repo, "skills/outside", "outside", "Outside requested scope", "Must stay outside")
			}
			client, revision := cachedHubRecoveryClient(t, repo)
			_, err := client.Preview(context.Background(), recoveryHubLink, revision, scope)
			if err == nil || !strings.Contains(err.Error(), `skills.sh skill "polish" was not found in the current repository revision`) {
				t.Fatalf("ordinary Hub preview no longer reports the missing selector: %v", err)
			}
			preview, err := client.PreviewHubRepository(context.Background(), recoveryHubLink, revision, scope)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Release(preview.ID) })
			if preview.Source != recoveryGitSource || preview.Ref != revision || preview.Revision != revision || preview.Subpath != scope {
				t.Fatalf("recovery changed the repository identity or scope: %+v", preview)
			}
			if len(preview.Candidates) != 1 || preview.Candidates[0].Name != "impeccable" || preview.Candidates[0].Subpath != canonical {
				t.Fatalf("recovery offered copies or fixtures: %+v", preview.Candidates)
			}
			if len(preview.Skipped) != 1 || preview.Skipped[0].Subpath != standardSkipped {
				t.Fatalf("recovery retained fixture diagnostics or lost a standard skill diagnostic: %+v", preview.Skipped)
			}
			if got := client.previews[preview.ID].preview; !reflect.DeepEqual(got, preview) {
				t.Fatalf("stored install allowlist differs from the displayed recovery: %+v", got)
			}
			selected, err := client.Candidates(preview.ID, []string{canonical})
			if err != nil || len(selected) != 1 || selected[0].Skill.Source != recoveryGitSource || selected[0].Skill.Ref != revision {
				t.Fatalf("canonical recovery candidate lost its provenance: %+v %v", selected, err)
			}
			for _, hidden := range []string{duplicate, fixture, standardSkipped, fixtureSkipped, "skills/outside"} {
				if _, err := client.Candidates(preview.ID, []string{hidden}); err == nil {
					t.Fatalf("recovery allowed installing an unoffered path %q", hidden)
				}
			}
			gitPreview, err := client.Preview(context.Background(), recoveryGitSource, revision, scope)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Release(gitPreview.ID) })
			if len(gitPreview.Candidates) != 3 || len(gitPreview.Skipped) != 2 {
				t.Fatalf("ordinary Git preview was silently filtered: %+v", gitPreview)
			}
			for _, path := range []string{canonical, duplicate, fixture} {
				if _, err := client.Candidates(gitPreview.ID, []string{path}); err != nil {
					t.Fatalf("ordinary Git preview no longer offers %q: %v", path, err)
				}
			}
		})
	}
}

func TestPreviewHubRepositoryKeepsEqualPriorityPathsAndDistinctSkills(t *testing.T) {
	repo := recoveryTestRepo(t)
	for _, path := range []string{".agents/skills/first", ".agents/skills/second", ".claude/skills/copy"} {
		writeTestSkill(t, repo, path, "impeccable", "Same named skill", "Review this explicit path")
	}
	writeTestSkill(t, repo, ".claude/skills/other", "other", "Different published skill", "Keep this skill")
	client, revision := cachedHubRecoveryClient(t, repo)
	preview, err := client.PreviewHubRepository(context.Background(), "skills.sh/team/repo/polish", revision, ".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Release(preview.ID) })
	paths := make([]string, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		paths = append(paths, candidate.Subpath)
	}
	want := []string{".agents/skills/first", ".agents/skills/second", ".claude/skills/other"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("recovery collapsed ambiguous paths or filtered a distinct skill: got %v, want %v", paths, want)
	}
}

func TestPreviewHubRepositoryKeepsFallbackWhenNoStandardSkillsExist(t *testing.T) {
	repo := recoveryTestRepo(t)
	const path = "custom/catalog/impeccable"
	writeTestSkill(t, repo, path, "impeccable", "Custom published location", "Fallback content")
	writeTestSkill(t, repo, "custom/catalog/broken", "broken", `""`, "Invalid metadata")
	client, revision := cachedHubRecoveryClient(t, repo)
	preview, err := client.PreviewHubRepository(context.Background(), "https://www.skills.sh/team/repo/polish", revision, ".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Release(preview.ID) })
	if len(preview.Candidates) != 1 || preview.Candidates[0].Subpath != path || len(preview.Skipped) != 1 || preview.Skipped[0].Subpath != "custom/catalog/broken" {
		t.Fatalf("recovery lost the custom-directory fallback: %+v", preview)
	}
	if candidates, err := client.Candidates(preview.ID, []string{path}); err != nil || len(candidates) != 1 {
		t.Fatalf("fallback candidate was not installable: %+v %v", candidates, err)
	}
}

func TestPreviewHubRepositoryReservesBudgetForCanonicalSkills(t *testing.T) {
	repo := recoveryTestRepo(t)
	for _, subpath := range []string{"skills/impeccable", ".agents/skills/impeccable"} {
		writeTestSkill(t, repo, subpath, "impeccable", "Hub skill variant", "Content from "+subpath)
		writeLargeTestFile(t, filepath.Join(repo, filepath.FromSlash(subpath), "large.bin"), 60<<20)
	}
	client, revision := cachedHubRecoveryClient(t, repo)
	preview, err := client.PreviewHubRepository(context.Background(), recoveryHubLink, revision, ".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Release(preview.ID) })
	if len(preview.Candidates) != 1 || preview.Candidates[0].Subpath != "skills/impeccable" {
		t.Fatalf("a generated copy consumed the canonical skill's extraction budget: %+v", preview)
	}
}

func TestPreviewHubRepositoryRejectsOtherSourcesAndMalformedLinks(t *testing.T) {
	inputs := []string{
		"", "team/repo", recoveryGitSource, "git@github.com:team/repo.git",
		"https://skills.sh/open.feishu.cn/lark-doc", "https://skills.sh/site/open.feishu.cn/lark-doc",
		"https://open.feishu.cn/.well-known/skills", "https://skills.sh/team/repo",
		"http://skills.sh/team/repo/polish", "ssh://skills.sh/team/repo/polish",
		"https://user@skills.sh/team/repo/polish", "https://skills.sh:443/team/repo/polish",
		"https://skills.sh:444/team/repo/polish", "https://skills.sh/team/repo/polish?",
		"https://skills.sh/team/repo/polish?token=secret", "https://skills.sh/team/repo/polish#",
		"https://skills.sh/team/repo/polish#section", "https://skills.sh/team/repo/%70olish",
		"https://skills.sh/team/repo/polish/extra", "https://skills.sh//team/repo/polish",
		"https://skills.sh/team/repo/polish/",
		"https://skills.sh/team/repo/../polish", "https://skills.sh/team/repo/./polish",
		"https://skills.sh/team/repo/polish\\extra", "https://skills.sh.evil.test/team/repo/polish",
		" https://skills.sh/team/repo/polish", "https://skills.sh/team/repo/polish\n",
	}
	client := New(t.TempDir())
	for _, input := range inputs {
		preview, err := client.PreviewHubRepository(context.Background(), input, "", "")
		if err == nil || preview.ID != "" || len(client.previews) != 0 {
			t.Fatalf("invalid recovery link %q was accepted: %+v %v", input, preview, err)
		}
	}
	for _, input := range []struct{ ref, subpath string }{{"--upload-pack=evil", "."}, {"main", "../escape"}} {
		if _, err := client.PreviewHubRepository(context.Background(), recoveryHubLink, input.ref, input.subpath); err == nil {
			t.Fatalf("recovery bypassed ref or subpath validation: %+v", input)
		}
	}
	if entries, err := os.ReadDir(client.cacheDir); err != nil || len(entries) != 0 {
		t.Fatalf("invalid recovery inputs touched the repository cache: %+v %v", entries, err)
	}
}
