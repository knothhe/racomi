package sources

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
	"racomi/internal/testutil"
)

func TestDistributionCandidatesGroupDistinctContentsAndKeepAmbiguousFamilies(t *testing.T) {
	items := []CandidateInfo{
		{Name: "overlay", Subpath: ".claude/skills/adapter", ContentHash: "variant"},
		{Name: "overlay", Subpath: "skills/overlay", ContentHash: "canonical"},
		{Name: "overlay", Subpath: ".agents/skills/overlay", ContentHash: "canonical"},
		{Name: "overlay", Subpath: ".codex/skills/overlay", ContentHash: "variant"},
		{Name: "overlay", Subpath: "tests/fixture/.agents/skills/overlay", ContentHash: "fixture"},
		{Name: "overlay", Subpath: "skills/team/overlay", ContentHash: "namespace"},
	}
	grouped := groupDistributionCandidates(items)
	if len(grouped) != 3 {
		t.Fatalf("copies were not grouped or separate scopes were merged: %+v", grouped)
	}
	canonical, _ := findPreviewCandidate(grouped, "skills/overlay")
	if canonical == nil || len(canonical.Variants) != 1 || canonical.Variants[0].Subpath != ".claude/skills/adapter" {
		t.Fatalf("preferred build or distinct content choices were lost: %+v", grouped)
	}
	if !reflect.DeepEqual(grouped, groupDistributionCandidates(grouped)) {
		t.Fatal("grouping a preview again changed its logical candidates")
	}
	ambiguous := append(items, CandidateInfo{Name: "overlay", Subpath: ".agents/skills/second", ContentHash: "second"})
	if grouped = groupDistributionCandidates(ambiguous); len(grouped) != len(ambiguous) {
		t.Fatalf("same-tier ambiguity was silently folded into a family: %+v", grouped)
	}
}

func TestGitAndHubPreviewsShareVariantsAndKeepPhysicalCatalogOrigins(t *testing.T) {
	repo := testRepo(t)
	canonical, identical, variant := "skills/impeccable", ".agents/skills/impeccable", ".claude/skills/adapter"
	for _, subpath := range []string{canonical, identical} {
		writeHubSkillVariant(t, repo, subpath, "Canonical content")
	}
	writeHubSkillVariant(t, repo, variant, "Claude-specific content")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add distribution variants")
	client := New(t.TempDir())
	gitPreview, err := client.Preview(context.Background(), repo, "main", ".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Release(gitPreview.ID) })
	hubPreview, err := previewHubSkill(t, client, repo, ".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Release(hubPreview.ID) })
	for _, preview := range []Preview{gitPreview, hubPreview} {
		item, _ := findPreviewCandidate(preview.Candidates, canonical)
		if item == nil || len(item.Variants) != 1 || item.Variants[0].Subpath != variant {
			t.Fatalf("Git/Hub preview disagree on distribution variants: %+v", preview)
		}
		selected, err := client.Candidates(preview.ID, []string{variant})
		if err != nil || len(selected) != 1 || selected[0].Skill.Subpath != variant || selected[0].Skill.ContentHash != item.Variants[0].ContentHash {
			t.Fatalf("variant selection lost physical provenance: %+v %v", selected, err)
		}
		if _, err = client.Candidates(preview.ID, []string{canonical, variant}); err == nil {
			t.Fatal("preview allowed two builds of one logical skill")
		}
		if _, err = client.Candidates(preview.ID, []string{identical}); err == nil {
			t.Fatal("preview allowed an identical copy omitted from its public choices")
		}
	}
	described, err := client.DescribePreview(gitPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := findPreviewCandidate(described.Candidates, canonical)
	item.Variants[0].Subpath = "changed"
	described, err = client.DescribePreview(gitPreview.ID)
	item, _ = findPreviewCandidate(described.Candidates, canonical)
	if err != nil || item.Variants[0].Subpath != variant {
		t.Fatal("DescribePreview exposed mutable nested variant slices")
	}

	source, err := client.CatalogForPreview(gitPreview.ID, skillstore.Source{Repository: repo, Ref: "main", Subpath: "."})
	if err != nil || len(source.Catalog) != 5 {
		t.Fatalf("physical source catalog lost distribution paths: %+v %v", source, err)
	}
	selected, previewID, err := client.PrepareSourceSync(context.Background(), source, []string{identical})
	if previewID != "" {
		defer client.Release(previewID)
	}
	if err != nil || len(selected) != 1 || selected[0].Skill.Subpath != identical || filepath.Base(selected[0].Path) != "impeccable" {
		t.Fatalf("source synchronization lost an installed identical copy's origin: %+v %v", selected, err)
	}
	_, previewID, err = client.PrepareSourceSync(context.Background(), source, []string{canonical, identical})
	if previewID != "" {
		defer client.Release(previewID)
	}
	if err == nil {
		t.Fatal("source synchronization allowed two identical copies of a logical skill")
	}
}

func TestHubExtractionReservesBudgetForDistinctNamespacedFamilies(t *testing.T) {
	roots := map[string]*skillExtraction{
		"skills/team-a/captions":         {name: "captions", priority: true, validMetadata: true},
		".agents/skills/team-a/copy":     {name: "captions", priority: true, validMetadata: true},
		".agents/skills/team-b/captions": {name: "captions", priority: true, validMetadata: true},
	}
	prioritizeHubRepositoryRoots(roots, ".")
	if !roots["skills/team-a/captions"].priority || !roots[".agents/skills/team-b/captions"].priority || roots[".agents/skills/team-a/copy"].priority {
		t.Fatal("a distribution alias took priority over a distinct namespaced skill")
	}
}

func TestSkippedDistributionKeepsFamilyAmbiguityInPreviewAndCatalog(t *testing.T) {
	repo := testRepo(t)
	first, skipped, copy := ".agents/skills/first", ".agents/skills/second", ".claude/skills/copy"
	for _, subpath := range []string{first, skipped, copy} {
		writeHubSkillVariant(t, repo, subpath, "Content from "+subpath)
	}
	if err := testutil.Symlink(t, "reference.txt", filepath.Join(repo, filepath.FromSlash(skipped), "linked.txt")); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Block one ambiguous distribution path")
	client := New(t.TempDir())
	preview, err := client.Preview(context.Background(), repo, "main", ".")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Release(preview.ID)
	for _, subpath := range []string{first, copy} {
		item, _ := findPreviewCandidate(preview.Candidates, subpath)
		if item == nil || len(item.Variants) != 0 {
			t.Fatalf("a skipped same-tier path hid the family's ambiguity: %+v", preview)
		}
	}
	source, err := client.CatalogForPreview(preview.ID, skillstore.Source{Repository: repo, Ref: "main", Subpath: "."})
	if err != nil {
		t.Fatal(err)
	}
	keys := skillstore.DistributionCatalogKeys(source.Catalog)
	if keys[first] == keys[copy] {
		t.Fatalf("catalog discarded trusted metadata for the skipped ambiguous path: %+v", source.Catalog)
	}
	candidates, previewID, err := client.PrepareSourceSync(context.Background(), source, []string{first, copy})
	if previewID != "" {
		defer client.Release(previewID)
	}
	if err != nil || len(candidates) != 2 {
		t.Fatalf("source synchronization changed the reviewed ambiguous identities: %+v %v", candidates, err)
	}
}

func TestSelectedAlternateUpdatesWhenItBecomesAnIdenticalHiddenCopy(t *testing.T) {
	repo := testRepo(t)
	canonical, alternate := ".agents/skills/captions", ".claude/skills/copy"
	writeTestSkill(t, repo, canonical, "captions", "Caption overlay", "Canonical content")
	writeTestSkill(t, repo, alternate, "captions", "Caption overlay", "Alternate content")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Add caption distributions")
	client := New(t.TempDir())
	store, err := skillstore.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.SaveSource(skillstore.Source{Name: "Captions", Repository: repo, Ref: "main", Subpath: ".", Mode: skillstore.SourceModeAll})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	refresh := func() string {
		t.Helper()
		var previewID string
		source, previewID, err = client.RefreshSource(ctx, source)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.RecordSourceRefresh(source); err != nil {
			t.Fatal(err)
		}
		return previewID
	}
	synchronize := func() skillstore.Skill {
		t.Helper()
		candidates, previewID, err := client.PrepareSourceSync(ctx, source, []string{alternate})
		if err != nil {
			t.Fatal(err)
		}
		defer client.Release(previewID)
		if err = store.SyncSource(source.ID, source.Revision, candidates); err != nil {
			t.Fatal(err)
		}
		state, err := store.State()
		if err != nil || len(state.Skills) != 1 {
			t.Fatalf("alternate synchronization duplicated a logical skill: %+v %v", state, err)
		}
		return state.Skills[0]
	}
	_ = client.Release(refresh())
	installed := synchronize()
	writeTestSkill(t, repo, alternate, "captions", "Caption overlay", "Canonical content")
	testGit(t, repo, "add", ".")
	testGit(t, repo, "commit", "-m", "Make selected distribution match canonical content")
	updates, err := client.CheckUpdates(ctx, []skillstore.Skill{installed})
	if err != nil || len(updates) != 1 || !updates[0].Available || updates[0].Removed || updates[0].Error != "" {
		t.Fatalf("update checking lost the selected alternate's physical path: %+v %v", updates, err)
	}
	previewID := refresh()
	defer client.Release(previewID)
	preview, err := client.DescribePreview(previewID)
	item, _ := findPreviewCandidate(preview.Candidates, canonical)
	if err != nil || item == nil || len(item.Variants) != 0 {
		t.Fatalf("identical content still created a variant choice: %+v %v", preview, err)
	}
	if _, err = client.Candidates(previewID, []string{alternate}); err == nil {
		t.Fatal("identical physical alias leaked into the public preview allowlist")
	}
	updated := synchronize()
	if updated.ID != installed.ID || updated.InstalledAt != installed.InstalledAt || updated.Subpath != alternate || updated.ContentHash == installed.ContentHash {
		t.Fatalf("hidden identical alternate lost its identity or update provenance: %+v", updated)
	}
	document, err := os.ReadFile(filepath.Join(store.Root(), "objects", updated.ContentHash, "SKILL.md"))
	if err != nil || !strings.Contains(string(document), "Canonical content") {
		t.Fatalf("selected physical alternate did not update its installed content: %q %v", document, err)
	}
}
