package sources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func availabilityLink(skill string) string { return "https://skills.sh/team/repo/" + skill }

func availabilityCacheRepo(client *Client, source string) string {
	return filepath.Join(client.cacheDir, "repositories", strings.TrimSuffix(filepath.Base(client.hubAvailabilityPath(source)), ".json"))
}

func availabilityFetch(t *testing.T, client *Client, calls *int) hubAvailabilityFetch {
	t.Helper()
	return func(ctx context.Context, source string) (string, error) {
		*calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > hubAvailabilityBudget {
			t.Fatal("repository availability operation has no bounded deadline")
		}
		return availabilityCacheRepo(client, source), nil
	}
}

func TestCheckHubAvailabilityUsesNamesAndCanonicalDiscoveryWithoutPreviews(t *testing.T) {
	repo := recoveryTestRepo(t)
	writeTestSkill(t, repo, ".agents/skills/impeccable", "impeccable", "Published skill", "Current canonical skill")
	writeTestSkill(t, repo, ".claude/skills/impeccable", "impeccable", "Generated adapter", "Adapter copy")
	writeTestSkill(t, repo, "tests/fixture/audit", "audit", "Internal fixture", "Not published")
	writeTestSkill(t, repo, ".agents/skills/different-directory", "actual-name", "Frontmatter identity", "Discover by name")
	client, revision := cachedHubRecoveryClient(t, repo)
	now := time.Now().UTC()
	calls := 0
	inputs := []string{availabilityLink("polish"), availabilityLink("audit"), availabilityLink("actual-name"), availabilityLink("impeccable"), availabilityLink("actual-name")}
	results, err := client.checkHubAvailability(context.Background(), inputs, func() time.Time { return now }, availabilityFetch(t, client, &calls))
	if err != nil || len(results) != len(inputs) || calls != 1 {
		t.Fatalf("availability did not group repository requests: %+v %v, fetches=%d", results, err, calls)
	}
	for i, want := range []string{"missing", "missing", "available", "available", "available"} {
		if got := results[i]; got.URL != inputs[i] || got.State != want || got.Revision != revision || got.CheckedAt != now.Format(time.RFC3339Nano) {
			t.Fatalf("availability result %d lost order, discovery or revision: %+v", i, got)
		}
	}
	if len(client.previews) != 0 {
		t.Fatal("availability created an installable preview")
	}
	if _, err := os.Stat(filepath.Join(client.cacheDir, "previews")); !os.IsNotExist(err) {
		t.Fatalf("availability extracted repository content: %v", err)
	}
	// The public API can answer another selector from the persisted snapshot,
	// without the injected offline fetch or another tree/metadata extraction.
	public, err := client.CheckHubAvailability(context.Background(), []string{availabilityLink("different-directory")})
	if err != nil || len(public) != 1 || public[0].State != "available" {
		t.Fatalf("public API could not reuse the repository snapshot: %+v %v", public, err)
	}
}

func TestCheckHubAvailabilityReportsUnsafeAmbiguousAndInvalidContent(t *testing.T) {
	repo := recoveryTestRepo(t)
	writeTestSkill(t, repo, ".agents/skills/unsafe", "unsafe", "Unsafe asset", "Cannot install links")
	if err := os.Symlink("SKILL.md", filepath.Join(repo, ".agents/skills/unsafe/linked.txt")); err != nil {
		t.Fatal(err)
	}
	for _, location := range []string{".agents/skills/first", ".agents/skills/second"} {
		writeTestSkill(t, repo, location, "ambiguous", "Ambiguous name", "Keep the ambiguity")
	}
	writeTestSkill(t, repo, ".agents/skills/broken-directory", "invalid-description", `""`, "Invalid description, readable name")
	writeTestSkill(t, repo, ".agents/skills/oversized", "oversized", "Oversized skill content", "Budget validation")
	writeLargeTestFile(t, filepath.Join(repo, ".agents/skills/oversized/large.bin"), maxRepositoryBytes+1)
	// Both skills fit individually. Whole-repository extraction contention
	// must not make the later selector unavailable.
	for _, name := range []string{"large-first", "large-second"} {
		writeTestSkill(t, repo, ".agents/skills/"+name, name, "Individually valid large skill", "Metadata verification only")
		writeLargeTestFile(t, filepath.Join(repo, ".agents/skills/"+name+"/large.bin"), 60<<20)
	}
	client, revision := cachedHubRecoveryClient(t, repo)
	calls := 0
	inputs := []string{availabilityLink("unsafe"), availabilityLink("ambiguous"), availabilityLink("invalid-description"), availabilityLink("oversized"), availabilityLink("large-first"), availabilityLink("large-second")}
	results, err := client.checkHubAvailability(context.Background(), inputs, time.Now, availabilityFetch(t, client, &calls))
	if err != nil || len(results) != len(inputs) {
		t.Fatalf("availability failed: %+v %v", results, err)
	}
	for i, reason := range []string{"symlink is not supported", "matches multiple repository skills", "skill description cannot be empty", "100 MiB"} {
		if results[i].State != "unavailable" || !strings.Contains(results[i].Reason, reason) {
			t.Fatalf("unusable skill %d was mistaken for missing or lost its cause: %+v", i, results[i])
		}
	}
	for _, result := range results[4:] {
		if result.State != "available" {
			t.Fatalf("cross-skill preview contention hid an individually available skill: %+v", result)
		}
	}
	_, err = client.Preview(context.Background(), availabilityLink("invalid-description"), revision, ".")
	if err == nil || !strings.Contains(err.Error(), "is unavailable:") || !strings.Contains(err.Error(), "skill description cannot be empty") {
		t.Fatalf("normal preview lost the readable name of invalid metadata: %v", err)
	}
}

func TestCheckHubAvailabilityCachesRepositorySnapshotsAcrossSelectorsAndRestarts(t *testing.T) {
	repo := recoveryTestRepo(t)
	writeTestSkill(t, repo, "skills/published", "published", "Published skill", "Current content")
	client, revision := cachedHubRecoveryClient(t, repo)
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	calls := 0
	fetch := availabilityFetch(t, client, &calls)
	first, err := client.checkHubAvailability(context.Background(), []string{availabilityLink("polish")}, clock, fetch)
	if err != nil || first[0].State != "missing" || calls != 1 {
		t.Fatalf("initial availability check failed: %+v %v %d", first, err, calls)
	}
	now = now.Add(time.Minute)
	restarted := New(client.cacheDir)
	second, err := restarted.checkHubAvailability(context.Background(), []string{availabilityLink("published")}, clock, fetch)
	if err != nil || second[0].State != "available" || second[0].Revision != revision || second[0].CheckedAt != first[0].CheckedAt || calls != 1 {
		t.Fatalf("snapshot was not reused across selectors and clients: %+v %v %d", second, err, calls)
	}
	// A foreground refresh has advanced the local HEAD during the TTL.
	writeTestSkill(t, repo, "skills/polish", "polish", "Newly restored skill", "New content")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Restore an indexed skill")
	testGit(t, repo, "push", "--", availabilityCacheRepo(client, recoveryGitSource), "main")
	third, err := restarted.checkHubAvailability(context.Background(), []string{availabilityLink("polish")}, clock, fetch)
	if err != nil || third[0].State != "available" || third[0].Revision == revision || calls != 2 {
		t.Fatalf("known HEAD change did not invalidate a fresh snapshot: %+v %v %d", third, err, calls)
	}
	now = now.Add(hubAvailabilityTTL)
	fourth, err := restarted.checkHubAvailability(context.Background(), []string{availabilityLink("polish")}, clock, fetch)
	if err != nil || fourth[0].State != "available" || calls != 3 || fourth[0].CheckedAt == third[0].CheckedAt {
		t.Fatalf("expired snapshot was not refreshed: %+v %v %d", fourth, err, calls)
	}
	data, err := os.ReadFile(restarted.hubAvailabilityPath(recoveryGitSource))
	if err != nil || len(data) == 0 || len(data) > maxHubAvailabilityCacheBytes {
		t.Fatalf("persisted availability cache is absent or unbounded: %d %v", len(data), err)
	}
}

func TestCheckHubAvailabilityDoesNotReplacePreferredSkillWithInvalidMetadata(t *testing.T) {
	repo := recoveryTestRepo(t)
	writeTestSkill(t, repo, ".agents/skills/impeccable", "impeccable", `""`, "Preferred skill with invalid metadata")
	writeTestSkill(t, repo, ".claude/skills/impeccable", "impeccable", "Valid generated copy", "Different lower priority variant")
	client, revision := cachedHubRecoveryClient(t, repo)
	calls := 0
	results, err := client.checkHubAvailability(context.Background(), []string{availabilityLink("impeccable")}, time.Now, availabilityFetch(t, client, &calls))
	if err != nil || len(results) != 1 || results[0].State != "unavailable" || results[0].Revision != revision || !strings.Contains(results[0].Reason, "skill description cannot be empty") {
		t.Fatalf("invalid preferred skill was replaced by a valid copy during availability checking: %+v %v", results, err)
	}
}

func TestCheckHubAvailabilityDoesNotHideAmbiguityWhenOneMatchIsUnsafe(t *testing.T) {
	repo := recoveryTestRepo(t)
	for _, subpath := range []string{"skills/first", "skills/second"} {
		writeTestSkill(t, repo, subpath, "impeccable", "Hub skill variant", "Content from "+subpath)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(repo, "skills", "first", "linked.txt")); err != nil {
		t.Fatal(err)
	}
	client, _ := cachedHubRecoveryClient(t, repo)
	calls := 0
	results, err := client.checkHubAvailability(context.Background(), []string{availabilityLink("impeccable")}, time.Now, availabilityFetch(t, client, &calls))
	if err != nil || len(results) != 1 || results[0].State != "unavailable" || !strings.Contains(results[0].Reason, "matches multiple repository skills") || !strings.Contains(results[0].Reason, "skills/first") || !strings.Contains(results[0].Reason, "skills/second") {
		t.Fatalf("an unsafe matching skill hid the selector's ambiguity: %+v %v", results, err)
	}
}

func TestCheckHubAvailabilityDoesNotCacheOperationalFailures(t *testing.T) {
	client := New(t.TempDir())
	inputs := []string{availabilityLink("polish"), availabilityLink("other")}
	calls := 0
	fetch := func(ctx context.Context, source string) (string, error) {
		calls++
		return "", errors.New("repository connection failed: permission denied")
	}
	for attempt := 1; attempt <= 2; attempt++ {
		results, err := client.checkHubAvailability(context.Background(), inputs, time.Now, fetch)
		if err != nil || len(results) != 2 || calls != attempt {
			t.Fatalf("operational failure was cached or duplicated: %+v %v %d", results, err, calls)
		}
		for _, result := range results {
			if result.State != "unknown" || !strings.Contains(result.Reason, "permission denied") || result.Revision != "" {
				t.Fatalf("operational failure claimed a skill was missing: %+v", result)
			}
		}
	}
	if _, err := os.Stat(client.hubAvailabilityPath(recoveryGitSource)); !os.IsNotExist(err) {
		t.Fatalf("unknown availability was persisted: %v", err)
	}
}

func TestCheckHubAvailabilityCancellationIsUnknownAndDoesNotWaitForForegroundLock(t *testing.T) {
	client := New(t.TempDir())
	client.mu.Lock()
	defer client.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	results, err := client.checkHubAvailability(ctx, []string{availabilityLink("polish")}, time.Now, func(context.Context, string) (string, error) {
		t.Fatal("canceled availability fetch ran behind the foreground lock")
		return "", nil
	})
	if err != nil || len(results) != 1 || results[0].State != "unknown" || !strings.Contains(results[0].Reason, "deadline exceeded") || time.Since(started) > time.Second {
		t.Fatalf("background cancellation waited behind foreground work: %+v %v", results, err)
	}
}

func TestCheckHubAvailabilityCancellationStopsLaterRepositories(t *testing.T) {
	client := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	inputs := []string{availabilityLink("polish"), "https://skills.sh/other/repository/skill"}
	results, err := client.checkHubAvailability(ctx, inputs, time.Now, func(ctx context.Context, source string) (string, error) {
		calls++
		cancel()
		return "", ctx.Err()
	})
	if err != nil || calls != 1 || len(results) != 2 {
		t.Fatalf("cancellation did not stop the batch: %+v %v %d", results, err, calls)
	}
	for _, result := range results {
		if result.State != "unknown" || !strings.Contains(result.Reason, "canceled") {
			t.Fatalf("canceled scan produced a factual classification: %+v", result)
		}
	}
}

func TestCheckHubAvailabilityRejectsUnsupportedInputsAndBoundsBatchSize(t *testing.T) {
	client := New(t.TempDir())
	inputs := []string{"team/repo", "https://github.com/team/repo", "https://skills.sh/open.feishu.cn/lark-doc", "https://skills.sh/site/open.feishu.cn/lark-doc", "https://skills.sh:443/team/repo/polish", "https://skills.sh/team/repo/%70olish", "https://skills.sh/team/repo/polish?", "https://skills.sh/team/repo/polish/", "https://skills.sh/team/repo/../polish"}
	results, err := client.CheckHubAvailability(context.Background(), inputs)
	if err != nil || len(results) != len(inputs) {
		t.Fatalf("unsupported inputs did not retain their result positions: %+v %v", results, err)
	}
	for i, result := range results {
		if result.URL != inputs[i] || result.State != "unknown" || result.Reason == "" {
			t.Fatalf("unsupported link was classified as missing: %+v", result)
		}
	}
	if _, err := client.CheckHubAvailability(context.Background(), make([]string, 201)); err == nil {
		t.Fatal("unbounded availability batch was accepted")
	}
	if entries, err := os.ReadDir(client.cacheDir); err != nil || len(entries) != 0 {
		t.Fatalf("invalid availability requests touched the cache: %+v %v", entries, err)
	}
}

func TestCheckHubAvailabilityCacheWriteFailureDoesNotChangeClassification(t *testing.T) {
	repo := recoveryTestRepo(t)
	writeTestSkill(t, repo, "skills/published", "published", "Published skill", "Current content")
	client, _ := cachedHubRecoveryClient(t, repo)
	if err := os.WriteFile(filepath.Join(client.cacheDir, "hub-availability"), []byte("Prevent directory creation"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	for attempt := 1; attempt <= 2; attempt++ {
		results, err := client.checkHubAvailability(context.Background(), []string{availabilityLink("published")}, time.Now, availabilityFetch(t, client, &calls))
		if err != nil || results[0].State != "available" || calls != attempt {
			t.Fatalf("best-effort cache persistence affected availability: %+v %v %d", results, err, calls)
		}
	}
}
