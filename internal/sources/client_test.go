package sources

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"racomi/internal/testutil"
	skillstore "racomi/internal/skills"
)

func TestGitCommandDistinguishesTimeoutAndCancellation(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		name := "canceled"
		cause := context.Canceled
		message := "Git operation was canceled"
		if timeout {
			name, cause, message = "timeout", context.DeadlineExceeded, "Git operation timed out"
		}
		t.Run(name, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if timeout {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}
			defer cancel()
			_, err := gitCommand(ctx, "", "--version")
			if !errors.Is(err, cause) || !strings.Contains(err.Error(), message) {
				t.Fatalf("incorrect Git failure reason: %v", err)
			}
		})
	}
}

func testGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Racomi test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Racomi test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeTestSkill(t *testing.T, repo, subpath, name, description, body string) {
	t.Helper()
	dir := filepath.Join(repo, filepath.FromSlash(subpath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n", name, description, body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func testRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	// macOS may expose the temp directory through /var, itself a symlink.
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "init", "-b", "main")
	writeTestSkill(t, repo, "skills/alpha", "alpha", `"A skill: with punctuation"`, "Version one")
	writeTestSkill(t, repo, "nested/beta", "beta", ">\n  Folded multiline\n  description", "Beta")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Initial skills")
	return repo
}

func TestPreviewKeepsCommitAndSelectsOnlyOfferedPaths(t *testing.T) {
	repo := testRepo(t)
	client := New(t.TempDir())
	ctx := context.Background()
	first, err := client.Preview(ctx, repo, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Candidates) != 2 || first.Ref != "main" || first.Source != repo {
		t.Fatalf("bad preview: %#v", first)
	}
	if first.Candidates[0].Description != "Folded multiline description" {
		t.Fatalf("YAML folding was lost: %q", first.Candidates[0].Description)
	}
	old, err := client.Candidates(first.ID, []string{"skills/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if old[0].Skill.Revision != first.Revision || old[0].Skill.Subpath != "skills/alpha" {
		t.Fatalf("source info lost: %#v", old[0].Skill)
	}
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Updated", "Version two")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Improve alpha")
	second, err := client.Preview(ctx, repo, "main", "skills/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision == first.Revision || len(second.Candidates) != 1 {
		t.Fatal("preview did not refresh source")
	}
	contents, err := os.ReadFile(filepath.Join(old[0].Path, "SKILL.md"))
	if err != nil || !bytes.Contains(contents, []byte("Version one")) {
		t.Fatalf("old preview drifted: %s, %v", contents, err)
	}
	if _, err := client.Candidates(first.ID, []string{"../outside"}); err == nil {
		t.Fatal("accepted traversal selection")
	}
	if _, err := client.Candidates(first.ID, []string{"skills"}); err == nil {
		t.Fatal("accepted unoffered selection")
	}
	if _, err := client.Preview(ctx, repo, "does-not-exist", ""); err == nil {
		t.Fatal("accepted missing ref")
	}
	if _, err := client.Candidates(first.ID, []string{"skills/alpha"}); err != nil {
		t.Fatalf("failed preview damaged earlier version: %v", err)
	}
}

func TestCheckUpdatesSeparatesRefsAndSkillContent(t *testing.T) {
	repo := testRepo(t)
	client := New(t.TempDir())
	ctx := context.Background()
	initial, err := client.Preview(ctx, repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "tag", "v1")
	skills := []skillstore.Skill{
		{ID: "alpha", Name: "alpha", Source: repo, Ref: "main", Subpath: "skills/alpha", Revision: initial.Revision},
		{ID: "beta", Name: "beta", Source: repo, Ref: "main", Subpath: "nested/beta", Revision: initial.Revision},
		{ID: "pinned", Name: "alpha", Source: repo, Ref: "v1", Subpath: "skills/alpha", Revision: initial.Revision},
	}
	writeTestSkill(t, repo, "skills/alpha", "alpha", "Improved", "New files")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Update alpha only")
	updates, err := client.CheckUpdates(ctx, skills)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 3 || !updates[0].Available || updates[1].Available || updates[2].Available || updates[0].Error != "" {
		t.Fatalf("incorrect update results: %#v", updates)
	}
	if !strings.Contains(updates[0].Summary, "Update alpha only") {
		t.Fatalf("missing commit summary: %q", updates[0].Summary)
	}
	updated, err := client.PrepareUpdate(ctx, skills[0])
	if err != nil {
		t.Fatal(err)
	}
	if updated.Skill.ID != "alpha" || updated.Skill.Revision == initial.Revision {
		t.Fatalf("bad update candidate: %#v", updated)
	}
	if err := os.Rename(repo, repo+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Rename(repo+"-moved", repo) })
	updates, err = client.CheckUpdates(ctx, skills[:1])
	if err != nil || updates[0].Error == "" {
		t.Fatalf("source failure was not reported: %#v %v", updates, err)
	}
	old, err := client.Candidates(initial.ID, []string{"skills/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := os.ReadFile(filepath.Join(old[0].Path, "SKILL.md"))
	if !bytes.Contains(contents, []byte("Version one")) {
		t.Fatal("failed refresh overwrote old content")
	}
}

func TestCheckUpdatesRejectsStoredRevisionOptions(t *testing.T) {
	client := New(t.TempDir())
	updates, err := client.CheckUpdates(context.Background(), []skillstore.Skill{{ID: "malicious", Source: "owner/repo", Ref: "main", Subpath: ".", Revision: "--output=/tmp/racomi-unwanted"}})
	if err != nil || len(updates) != 1 || !strings.Contains(updates[0].Error, "invalid Git revision") {
		t.Fatalf("stored Git options were not rejected before invoking Git: %#v %v", updates, err)
	}
}

func TestConcurrentClientsUseSameCacheSafely(t *testing.T) {
	repo := testRepo(t)
	cache := t.TempDir()
	clients := []*Client{New(cache), New(cache)}
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			if _, err := client.Preview(context.Background(), repo, "main", ""); err != nil {
				t.Error(err)
			}
		}(client)
	}
	wg.Wait()
}

func TestReleaseOnlyDeletesOwnedPreviewRoots(t *testing.T) {
	repo := testRepo(t)
	client := New(t.TempDir())
	preview, err := client.Preview(context.Background(), repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := client.Candidates(preview.ID, []string{"skills/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("Keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := client.ReleaseCandidate(skillstore.Candidate{Path: outside}); err != nil {
		t.Fatal(err)
	}
	if err := client.Release("../../" + outside); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal("arbitrary caller path was deleted")
	}
	// A client sharing the cache cannot discard another client's preview.
	other := New(client.cacheDir)
	if err := other.Release(preview.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidates[0].Path); err != nil {
		t.Fatal("other client deleted an unowned preview")
	}
	if err := client.ReleaseCandidate(candidates[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(client.cacheDir, "previews", preview.ID)); !os.IsNotExist(err) {
		t.Fatalf("preview root still exists: %v", err)
	}
	if _, err := client.Candidates(preview.ID, []string{"skills/alpha"}); err == nil {
		t.Fatal("released preview remained available")
	}
	if err := client.Release(preview.ID); err != nil {
		t.Fatal("release should be idempotent")
	}
	if err := client.ReleaseCandidate(candidates[0]); err != nil {
		t.Fatal("candidate release should be idempotent")
	}
	if _, err := os.Stat(filepath.Join(repo, "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatal("release touched source repository")
	}
}

func TestSearchPublicAPIContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" || r.URL.Query().Get("q") != "test & skill" || r.URL.Query().Get("limit") != "20" {
			t.Errorf("unexpected query: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"skills":[{"id":"owner/repo/alpha","name":"alpha","source":"owner/repo","installs":1},{"id":"other/repo/beta","name":"beta","source":"other/repo","installs":4},{"id":"skills.example.com/hosted","name":"hosted","source":"skills.example.com","installs":0}]}`)
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.searchBase = server.URL
	results, err := client.Search(context.Background(), "test & skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Name != "beta" || results[1].URL != "https://skills.sh/owner/repo/alpha" {
		t.Fatalf("bad search: %#v", results)
	}
	if !results[0].Supported || !results[1].Supported || !results[2].Supported || results[2].UnsupportedReason != "" {
		t.Fatalf("Git/hosted support status was not preserved: %#v", results)
	}
	if spec, err := parseSource(results[2].URL, "", ""); err != nil || !spec.hosted || spec.source != "https://skills.example.com" || spec.subpath != "hosted" {
		t.Fatalf("hosted search result cannot be previewed: %+v %v", spec, err)
	}
}

func TestSearchErrorsAreVisible(t *testing.T) {
	for _, response := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate limit", 429, "rate limited"},
		{"missing skills", 200, `{}`},
		{"null skills", 200, `{"skills":null}`},
		{"malformed", 200, `{`},
		{"unsafe source", 200, `{"skills":[{"id":"owner/repo/alpha","name":"alpha","source":"--exec","installs":0}]}`},
		{"too large", 200, strings.Repeat(" ", maxSearchBytes+1)},
	} {
		t.Run(response.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(response.status)
				fmt.Fprint(w, response.body)
			}))
			defer server.Close()
			client := New(t.TempDir())
			client.searchBase = server.URL
			if _, err := client.Search(context.Background(), "test"); err == nil {
				t.Fatal("failure was silently returned as empty results")
			}
		})
	}
}

func TestFrontmatterRequiresSafeCompleteMetadata(t *testing.T) {
	for _, content := range []string{
		"# No metadata", "---\nname: alpha\n---\n", "---\nname: ../escape\ndescription: Bad\n---\n",
		"---\nname: 42\ndescription: Bad\n---\n", "---\nname: alpha\nname: beta\ndescription: Bad\n---\n",
		"---\nname: alpha\ndescription: ''\n---\n", "---\nname: alpha\ndescription: Bad\n",
	} {
		if _, _, err := metadata([]byte(content)); err == nil {
			t.Errorf("accepted invalid metadata: %q", content)
		}
	}
	name, description, err := metadata([]byte("\xef\xbb\xbf---\r\nname: 'safe-skill'\r\ndescription: |\r\n  First line\r\n  Second line\r\n---\r\n"))
	if err != nil || name != "safe-skill" || description != "First line\nSecond line" {
		t.Fatalf("literal YAML was not preserved: %q %q %v", name, description, err)
	}
}

func TestSourceParsingAndUnsafePaths(t *testing.T) {
	valid := []struct{ input, source, skill, tail string }{
		{"owner/repo", "https://github.com/owner/repo.git", "", ""},
		{"https://github.com/owner/repo/tree/feature/branch/skills/alpha", "https://github.com/owner/repo.git", "", "feature/branch/skills/alpha"},
		{"https://skills.sh/owner/repo/alpha", "https://github.com/owner/repo.git", "alpha", ""},
		{"https://skills.sh/site/repo/alpha", "https://github.com/site/repo.git", "alpha", ""},
		{"git@github.com:owner/repo.git", "git@github.com:owner/repo.git", "", ""},
	}
	for _, row := range valid {
		spec, err := parseSource(row.input, "", "")
		if err != nil || spec.source != row.source || spec.skill != row.skill || spec.treeTail != row.tail {
			t.Errorf("parse %q: %#v, %v", row.input, spec, err)
		}
	}
	for _, input := range []string{"--upload-pack=evil", "ext::evil", "http://example.com/repo", "https://user:secret@example.com/repo", "https://github.com/owner/repo/tree/main/../escape", "https://skills.sh/owner/repo/../../evil"} {
		if _, err := parseSource(input, "", ""); err == nil {
			t.Errorf("accepted unsafe source %q", input)
		}
	}
	for _, subpath := range []string{"../escape", "a/../../escape", "/absolute", `a\..\escape`, ".git/config"} {
		if _, err := parseSource("owner/repo", "", subpath); err == nil {
			t.Errorf("accepted unsafe subpath %q", subpath)
		}
	}
	if _, err := parseSource("owner/repo", "--help", ""); err == nil {
		t.Fatal("accepted option ref")
	}
}

func TestArchiveRejectsTraversalAndReservedPaths(t *testing.T) {
	for _, header := range []*tar.Header{
		{Name: "../escape", Mode: 0644, Typeflag: tar.TypeReg},
		{Name: "../link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink},
		{Name: ".Git/config", Mode: 0644, Typeflag: tar.TypeReg},
	} {
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := unpackArchive(&archive, t.TempDir()); err == nil {
			t.Errorf("accepted malicious archive header %#v", header)
		}
	}
}

func TestRepositorySymlinkAndInvalidSkillAreSkipped(t *testing.T) {
	for _, invalid := range []string{"symlink", "metadata"} {
		t.Run(invalid, func(t *testing.T) {
			repo := testRepo(t)
			if invalid == "symlink" {
				if err := testutil.Symlink(t, "/etc/passwd", filepath.Join(repo, "skills", "alpha", "outside")); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestSkill(t, repo, "bad", "../escape", "Bad", "Bad")
			}
			testGit(t, repo, "add", ".")
			testGit(t, repo, "commit", "-m", "Unsafe content")
			client := New(t.TempDir())
			preview, err := client.Preview(context.Background(), repo, "main", "")
			if err != nil {
				t.Fatal("unaffected skills should still be offered", err)
			}
			defer client.Release(preview.ID)
			skippedPath, expectedCount := "skills/alpha", 1
			if invalid == "metadata" {
				skippedPath, expectedCount = "bad", 2
			}
			if len(preview.Candidates) != expectedCount || len(preview.Skipped) != 1 || preview.Skipped[0].Subpath != skippedPath || preview.Skipped[0].Reason == "" {
				t.Fatalf("unsafe skill was not individually skipped: %+v", preview)
			}
			if _, err := client.Candidates(preview.ID, []string{skippedPath}); err == nil {
				t.Fatal("skipped skill was selectable")
			}
		})
	}
}
