package sources

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	skillstore "racomi/internal/skills"
)

// Exercise repositories that previously exceeded the full-clone time limit.
// Temporary caches leave the application's source state and installed skills alone.
func TestLiveLargeRepositoryRefresh(t *testing.T) {
	if os.Getenv("RACOMI_TEST_LIVE_SOURCES") != "1" {
		t.Skip("set RACOMI_TEST_LIVE_SOURCES=1 to validate large Git sources")
	}
	for _, item := range []struct{ repository, ref string }{
		{"tt-a1i/archify", "main"},
		{"cloudflare/cloudflare-docs", "production"},
		{"heygen-com/hyperframes", "main"},
	} {
		t.Run(item.repository, func(t *testing.T) {
			client := New(t.TempDir())
			source, err := NormalizeSource(skillstore.Source{Repository: item.repository, Ref: item.ref})
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			source, id, err := client.RefreshSource(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Release(id)
			repo, err := client.repository(context.Background(), source.Repository, false, source.Ref)
			if err != nil {
				t.Fatal(err)
			}
			count, err := gitCommand(context.Background(), repo, "rev-list", "--count", source.Revision)
			if err != nil || count != "1\n" {
				t.Fatalf("downloaded unrequested history: %q %v", count, err)
			}
			t.Logf("Refreshed %s at %s in %s: %d skills, one commit", source.Repository, source.Ref, time.Since(start).Round(time.Millisecond), len(source.Catalog))
		})
	}
}

// This smoke test only reads two public leaderboard pages, without cloning any
// repositories. It verifies the anonymous endpoint used by the website itself.
func TestLiveSkillsHubList(t *testing.T) {
	if os.Getenv("RACOMI_TEST_LIVE_SOURCES") != "1" {
		t.Skip("set RACOMI_TEST_LIVE_SOURCES=1 to validate the public skills.sh service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := New(t.TempDir())
	for number := 0; number < 2; number++ {
		page, err := client.ListSkills(ctx, number)
		if err != nil {
			t.Fatal(err)
		}
		if page.Page != number || len(page.Skills) != skillListPageSize || page.Total < (number+1)*skillListPageSize || !page.HasMore {
			t.Fatalf("unexpected public leaderboard page: number=%d, page=%d, skills=%d, total=%d, hasMore=%v", number, page.Page, len(page.Skills), page.Total, page.HasMore)
		}
		if !page.Skills[0].Supported {
			t.Fatalf("first leaderboard result is not Git-backed: %#v", page.Skills[0])
		}
		for _, skill := range page.Skills {
			if skill.Name == "" || skill.Source == "" || skill.Slug == "" || skill.URL != "https://skills.sh/"+skill.Slug || skill.Installs < 0 {
				t.Fatalf("incomplete public leaderboard metadata: %#v", skill)
			}
		}
		t.Logf("Leaderboard page %d: %d of %d skills, first %s, last %s", number, len(page.Skills), page.Total, page.Skills[0].Slug, page.Skills[len(page.Skills)-1].Slug)
	}
}

// This test only reads the public Hub and its public Git repository. It is
// opt-in so the regular suite remains deterministic and works offline.
func TestLiveSkillsHubPreview(t *testing.T) {
	if os.Getenv("RACOMI_TEST_LIVE_SOURCES") != "1" {
		t.Skip("set RACOMI_TEST_LIVE_SOURCES=1 to validate the public skills.sh service")
	}
	cache, err := os.MkdirTemp("/private/tmp", "racomi-sources-live-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cache)
	client := New(cache)
	for _, test := range []struct{ query, slug, subpath string }{
		{"find-skills", "vercel-labs/skills/find-skills", "skills/find-skills"},
		{"impeccable", "pbakaus/impeccable/impeccable", ".agents/skills/impeccable"},
		// The Hub retains old standalone entries that upstream has merged
		// into impeccable commands. audit also names an upstream test fixture.
		{"polish", "pbakaus/impeccable/polish", ""},
		{"audit", "pbakaus/impeccable/audit", ""},
	} {
		t.Run(test.query, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), SourceOperationTimeout)
			defer cancel()
			selected := &SearchResult{Slug: test.slug, URL: "https://skills.sh/" + test.slug}
			if test.subpath != "" {
				results, err := client.Search(ctx, test.query)
				if err != nil {
					t.Fatal(err)
				}
				selected = nil
				for i := range results {
					if results[i].Slug == test.slug {
						selected = &results[i]
						break
					}
				}
				if selected == nil {
					t.Fatalf("the public index did not return %s", test.slug)
				}
			}
			preview, err := client.Preview(ctx, selected.URL, "", "")
			if test.subpath == "" {
				if err == nil {
					_ = client.Release(preview.ID)
					t.Fatalf("stale Hub entry offered an unrelated skill: %+v", preview)
				}
				if !strings.Contains(err.Error(), "was not found in the current repository revision") {
					t.Fatalf("stale Hub entry did not receive an actionable error: %v", err)
				}
				t.Logf("Hub %s correctly rejected: %v", selected.Slug, err)
				recovered, err := client.PreviewHubRepository(ctx, selected.URL, "", "")
				if err != nil {
					t.Fatal("cannot browse the repository after a stale Hub entry", err)
				}
				t.Cleanup(func() { _ = client.Release(recovered.ID) })
				if len(recovered.Candidates) != 1 || recovered.Candidates[0].Name != "impeccable" || recovered.Candidates[0].Subpath != ".agents/skills/impeccable" {
					t.Fatalf("recovery offered generated copies or fixtures: %+v", recovered)
				}
				if _, err := client.Candidates(recovered.ID, []string{"tests/oracle/workspaces/ctx-pin/.claude/skills/audit"}); err == nil {
					t.Fatal("recovery allowed installing an unoffered fixture")
				}
				t.Logf("Hub %s recovered to %s (%s), candidate %s", selected.Slug, recovered.Source, recovered.Revision, recovered.Candidates[0].Subpath)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Release(preview.ID) })
			if len(preview.Candidates) != 1 || preview.Candidates[0].Name != selected.Name || preview.Candidates[0].Subpath != test.subpath {
				t.Fatalf("Hub result and Git candidate disagree: %#v %#v", selected, preview)
			}
			candidates, err := client.Candidates(preview.ID, []string{test.subpath})
			if err != nil || len(candidates) != 1 {
				t.Fatalf("cannot prepare the public candidate: %v", err)
			}
			t.Logf("Hub %s -> %s (%s), candidate %s at %s", selected.Slug, preview.Source, preview.Revision, candidates[0].Skill.Name, candidates[0].Skill.Subpath)
		})
	}
}

func TestLiveSkillsHubAvailability(t *testing.T) {
	if os.Getenv("RACOMI_TEST_LIVE_SOURCES") != "1" {
		t.Skip("set RACOMI_TEST_LIVE_SOURCES=1 to validate public Hub availability")
	}
	cache := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	inputs := []string{
		"https://skills.sh/pbakaus/impeccable/polish",
		"https://skills.sh/pbakaus/impeccable/impeccable",
		"https://skills.sh/pbakaus/impeccable/audit",
		"https://skills.sh/vercel-labs/skills/find-skills",
	}
	want := []string{"missing", "available", "missing", "available"}
	client := New(cache)
	statuses, err := client.CheckHubAvailability(ctx, inputs)
	if err != nil || len(statuses) != len(inputs) {
		t.Fatalf("cannot check the public discovery cards: %+v %v", statuses, err)
	}
	for i, status := range statuses {
		if status.URL != inputs[i] || status.State != want[i] || status.Revision == "" || status.CheckedAt == "" {
			t.Fatalf("incorrect availability for %s: %+v; want %s", inputs[i], status, want[i])
		}
		t.Logf("%s: %s at %s", status.URL, status.State, status.Revision)
	}
	if entries, err := os.ReadDir(filepath.Join(cache, "previews")); !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatalf("card checks created preview content: %+v %v", entries, err)
	}
	// A new Client models restarting the app. The same fresh repository facts
	// must remain available without repeating downloads for each card.
	cached, err := New(cache).CheckHubAvailability(ctx, inputs)
	if err != nil || len(cached) != len(statuses) {
		t.Fatalf("cannot reuse persisted availability: %+v %v", cached, err)
	}
	for i := range cached {
		if cached[i] != statuses[i] {
			t.Fatalf("fresh persisted availability was not reused: %+v, want %+v", cached[i], statuses[i])
		}
	}
}

// This opt-in test downloads public hosted skill data into a temporary cache.
// It exercises both the legacy directory manifest and the current archive form.
func TestLiveHostedSkillsHubPreview(t *testing.T) {
	if os.Getenv("RACOMI_TEST_LIVE_SOURCES") != "1" {
		t.Skip("set RACOMI_TEST_LIVE_SOURCES=1 to validate public hosted skills")
	}
	cache, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := New(cache)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results, err := client.Search(ctx, "lark-doc")
	if err != nil {
		t.Fatal(err)
	}
	var selected *SearchResult
	for i := range results {
		if results[i].Slug == "open.feishu.cn/lark-doc" {
			selected = &results[i]
			break
		}
	}
	if selected == nil || !selected.Supported || selected.Source != "open.feishu.cn" {
		t.Fatalf("public hosted result is missing or unsupported: %#v", selected)
	}
	for _, test := range []struct {
		name, input, subpath string
	}{
		{name: "Hub legacy manifest", input: selected.URL},
		{name: "v0.2 digest-verified archive", input: "https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills", subpath: "lark-doc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			preview, err := client.Preview(ctx, test.input, "", test.subpath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { client.Release(preview.ID) })
			if !preview.Hosted || preview.Local || preview.Ref != "" || preview.Subpath != "lark-doc" || len(preview.Candidates) != 1 || preview.Candidates[0].Name != "lark-doc" {
				t.Fatalf("hosted preview did not select only lark-doc: %+v", preview)
			}
			candidates, err := client.Candidates(preview.ID, []string{"lark-doc"})
			if err != nil || len(candidates) != 1 {
				t.Fatalf("cannot prepare public hosted candidate: %+v %v", candidates, err)
			}
			files := map[string]bool{}
			err = filepath.WalkDir(candidates[0].Path, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.Type().IsRegular() {
					rel, err := filepath.Rel(candidates[0].Path, path)
					if err != nil {
						return err
					}
					files[filepath.ToSlash(rel)] = true
				}
				return nil
			})
			if err != nil || len(files) != 44 || !files["SKILL.md"] || !files["references/lark-doc-fetch.md"] || !files["references/genres/research-report.md"] {
				t.Fatalf("public hosted skill lost supporting files: count=%d files=%v error=%v", len(files), files, err)
			}
			if err := client.Release(preview.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(candidates[0].Path); !os.IsNotExist(err) {
				t.Fatalf("released hosted preview still exists: %v", err)
			}
			t.Logf("Hosted %s -> %s (%s), lark-doc with %d files", selected.Slug, preview.Source, preview.Revision, len(files))
		})
	}
}
