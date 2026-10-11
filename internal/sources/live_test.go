package sources

import (
	"context"
	"os"
	"testing"
	"time"
)

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
