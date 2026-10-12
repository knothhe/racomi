package sources

import (
	"context"
	"os"
	"testing"
	"time"
)

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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := New(cache)
	results, err := client.Search(ctx, "find-skills")
	if err != nil {
		t.Fatal(err)
	}
	var selected *SearchResult
	for i := range results {
		if results[i].Slug == "vercel-labs/skills/find-skills" {
			selected = &results[i]
			break
		}
	}
	if selected == nil {
		t.Fatal("the public index did not return vercel-labs/skills/find-skills")
	}
	preview, err := client.Preview(ctx, selected.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Candidates) != 1 || preview.Candidates[0].Name != selected.Name {
		t.Fatalf("Hub result and Git candidate disagree: %#v %#v", selected, preview)
	}
	candidates, err := client.Candidates(preview.ID, []string{preview.Candidates[0].Subpath})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("cannot prepare the public candidate: %v", err)
	}
	t.Logf("Hub %s -> %s (%s), candidate %s at %s", selected.Slug, preview.Source, preview.Revision, candidates[0].Skill.Name, candidates[0].Skill.Subpath)
}
