package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestSourceSyncAfterScopedImportPrioritizesReviewedSelection(t *testing.T) {
	f := newSourceFixture(t)
	f.write("a-large", "large", "Unrelated large skill")
	f.write("z-group/first", "first", "First reviewed skill")
	f.write("z-group/second", "second", "Second reviewed skill")
	for subpath, size := range map[string]int64{
		"a-large":        90 << 20,
		"z-group/first":  10 << 20,
		"z-group/second": 10 << 20,
	} {
		file, err := os.Create(filepath.Join(f.repo, subpath, "asset.bin"))
		if err != nil {
			t.Fatal(err)
		}
		truncateErr := file.Truncate(size)
		closeErr := file.Close()
		if truncateErr != nil || closeErr != nil {
			t.Fatalf("create bounded skill fixture: %v %v", truncateErr, closeErr)
		}
	}
	f.commit("Add skills exceeding the combined preview budget")
	ctx := context.Background()
	preview, err := f.service.Preview(ctx, f.repo, "main", "z-group")
	if err != nil {
		t.Fatal(err)
	}
	app, err := f.service.Install(preview.ID, []string{"z-group/first"})
	if err != nil {
		t.Fatal(err)
	}
	first := sourceSkill(t, app, "first")
	source := app.State.Sources[0]
	if source.Subpath != "." || len(app.State.Skills) != 1 {
		t.Fatalf("scoped import did not register the full source: %+v", app.State)
	}
	var second skillstore.SourceSkill
	for _, item := range source.Catalog {
		if item.Subpath == "z-group/second" {
			second = item
		}
	}
	if second.Name != "second" || second.SkipReason != "" || second.ContentHash == "" || second.Removed {
		t.Fatalf("unselected scoped neighbor was not available for later sync: %+v", second)
	}

	// Re-extract the reviewed revision after restart, even if upstream advances.
	f.write("z-group/second", "second", "Changed after review")
	f.commit("Advance upstream after the scoped import")
	restarted, err := New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	app, err = restarted.SyncSource(ctx, source.ID, source.Revision, []string{second.Subpath})
	if err != nil {
		t.Fatalf("reviewed selection lost its extraction budget: %v", err)
	}
	installed := sourceSkill(t, app, "second")
	if len(app.State.Skills) != 2 || sourceSkill(t, app, "first").ID != first.ID || installed.ContentHash != second.ContentHash || installed.Revision != source.Revision {
		t.Fatalf("sync changed the reviewed content or installed unrelated skills: %+v", app.State.Skills)
	}
	document, err := restarted.ReadSkillDocument(installed.ID)
	if err != nil || !strings.Contains(document, "Second reviewed skill") || strings.Contains(document, "Changed after review") {
		t.Fatalf("sync did not retain the reviewed document: %q %v", document, err)
	}
	asset, err := os.Stat(filepath.Join(f.root, "objects", installed.ContentHash, "asset.bin"))
	if err != nil || asset.Size() != 10<<20 {
		t.Fatalf("sync offered a truncated skill asset: %v %v", asset, err)
	}
}
