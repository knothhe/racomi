package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestHostedManifestBoundsImplicitDirectoriesBeforeDownloading(t *testing.T) {
	files := []string{"SKILL.md"}
	for i := range 7000 {
		files = append(files, fmt.Sprintf("directory-%d/nested/file", i))
	}
	var downloads atomic.Int32
	client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-skills/index.json" {
			json.NewEncoder(w).Encode(map[string]any{"skills": []hostedEntry{{Name: "alpha", Description: "Fixture", Files: files}}})
			return
		}
		downloads.Add(1)
		http.NotFound(w, r)
	})
	assertHostedPreviewFailure(t, client, server.URL, "too many entries")
	if downloads.Load() != 0 {
		t.Fatal("oversized manifest downloaded files before validating its implied directories")
	}
}

func TestHostedIndexRedirectUsesFinalLocationForFiles(t *testing.T) {
	client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-skills/index.json":
			http.Redirect(w, r, "/service/catalog/index.json", http.StatusFound)
		case "/service/catalog/index.json":
			fmt.Fprint(w, `{"skills":[{"name":"alpha","description":"Fixture","files":["SKILL.md"]}]}`)
		case "/service/catalog/alpha/SKILL.md":
			fmt.Fprint(w, hostedAlphaDocument)
		default:
			http.NotFound(w, r)
		}
	})
	p, err := client.Preview(context.Background(), server.URL, "", "")
	if err != nil || len(p.Candidates) != 1 {
		t.Fatalf("redirected hosted index resolved files against the wrong location: %+v %v", p, err)
	}
}

func TestHostedSnapshotsRejectStaleAndDamagedContentAndRecover(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-skills/index.json":
			http.NotFound(w, r)
		case "/.well-known/skills/index.json":
			fmt.Fprint(w, `{"skills":[{"name":"alpha","description":"Fixture","files":["SKILL.md","examples/SKILL.md"]}]}`)
		case "/.well-known/skills/alpha/SKILL.md":
			fmt.Fprintf(w, "---\nname: alpha\ndescription: Fixture\n---\nVersion %d\n", version.Load())
		case "/.well-known/skills/alpha/examples/SKILL.md":
			fmt.Fprint(w, "---\nname: example\ndescription: Bundled example\n---\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.http = server.Client()
	ctx := context.Background()
	preview := func() Preview {
		t.Helper()
		p, err := client.Preview(ctx, server.URL, "", "alpha")
		if err != nil || len(p.Candidates) != 1 || p.Candidates[0].Name != "alpha" {
			t.Fatalf("manifest root was not preserved: %+v %v", p, err)
		}
		return p
	}
	first := preview()
	version.Store(2)
	second := preview()
	if first.Revision == second.Revision || client.PreviewAdvances(ctx, first.ID, second.Revision) || !client.PreviewAdvances(ctx, second.ID, first.Revision) {
		t.Fatal("hosted discovery accepted a stale preview or rejected the latest preview")
	}
	source := skillstore.Source{ID: "source-hosted", Repository: server.URL, Subpath: "alpha", Revision: second.Revision}
	source, err := client.CatalogForPreview(second.ID, source)
	if err != nil || len(source.Catalog) != 1 {
		t.Fatalf("nested documentation became a source skill: %+v %v", source, err)
	}
	spec, _ := parseSource(server.URL, "", "alpha")
	snapshot := filepath.Join(client.hostedSnapshotBase(spec), second.Revision)
	if err := os.WriteFile(filepath.Join(snapshot, "alpha", "SKILL.md"), []byte("corrupted cache"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.PrepareSourceSync(ctx, source, []string{"alpha"}); err == nil {
		t.Fatal("synchronization accepted changed cached bytes")
	}
	repaired := preview()
	if repaired.Revision != second.Revision {
		t.Fatal("repair changed the upstream content revision")
	}
	if err := client.Release(repaired.ID); err != nil {
		t.Fatal(err)
	}
	restarted := New(client.cacheDir)
	candidates, id, err := restarted.PrepareSourceSync(ctx, source, []string{"alpha"})
	if err != nil || len(candidates) != 1 || candidates[0].Skill.ContentHash != second.Candidates[0].ContentHash {
		t.Fatalf("refresh did not repair the persisted snapshot: %+v %v", candidates, err)
	}
	defer restarted.Release(id)
}

func TestHostedRefreshRecordsRemovalWithoutRemovingInstalledFiles(t *testing.T) {
	var removed atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-skills/index.json" {
			if removed.Load() {
				fmt.Fprint(w, `{"skills":[]}`)
			} else {
				fmt.Fprint(w, `{"skills":[{"name":"alpha","description":"Fixture","files":["SKILL.md"]}]}`)
			}
			return
		}
		fmt.Fprint(w, "---\nname: alpha\ndescription: Fixture\n---\n")
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.http = server.Client()
	ctx := context.Background()
	source := skillstore.Source{Repository: server.URL, Subpath: "alpha"}
	initial, id, err := client.RefreshSource(ctx, source)
	if err != nil || len(initial.Catalog) != 1 {
		t.Fatalf("first refresh: %+v %v", initial, err)
	}
	defer client.Release(id)
	old, err := client.Candidates(id, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	removed.Store(true)
	updated, newID, err := client.RefreshSource(ctx, initial)
	if err != nil || len(updated.Catalog) != 1 || !updated.Catalog[0].Removed {
		t.Fatalf("missing hosted skill was not marked removed: %+v %v", updated, err)
	}
	defer client.Release(newID)
	if _, err := os.ReadFile(filepath.Join(old[0].Path, "SKILL.md")); err != nil {
		t.Fatal("refresh damaged previous reviewed content")
	}
}
