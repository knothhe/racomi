package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	skillstore "racomi/internal/skills"
)

type hostedTestTransport func(*http.Request) (*http.Response, error)

func (f hostedTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHostedImportAndReviewedSourceSyncWithoutGit(t *testing.T) {
	// The service creates its own sources.Client. Route its public HTTPS URLs
	// through a trusted local TLS server while keeping the source identity real.
	// This test is intentionally sequential because it replaces DefaultTransport.
	const origin = "https://hosted.example"
	var mu sync.Mutex
	version, requests := 1, 0
	offline, unavailable := false, false
	document := func(v int) string {
		return fmt.Sprintf("---\nname: lark-doc\ndescription: hosted document fixture\n---\nReviewed document version %d.\n", v)
	}
	reference := func(v int) []byte { return []byte(fmt.Sprintf("Reference version %d.\n", v)) }
	asset := func(v int) []byte { return []byte{0, byte(v), 0xff, 1} }
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		v, fail := version, unavailable
		mu.Unlock()
		if fail {
			http.Error(w, "publisher unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/hosted.example/lark-doc", "/site/hosted.example/lark-doc":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<code>npx skills add https://hosted.example</code>`)
		case "/.well-known/agent-skills/index.json":
			// Some publishers return their HTML fallback with HTTP 200. That
			// must still allow discovery of the legacy files[] index.
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html>Not found</html>")
		case "/.well-known/skills/index.json":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"skills": []map[string]any{
				{"name": "lark-doc", "description": "hosted document fixture", "files": []string{"SKILL.md", "references/nested/instructions.md", "assets/binary.dat"}},
				{"name": "lark-shared", "description": "unselected neighbor", "files": []string{"SKILL.md"}},
			}})
		case "/.well-known/skills/lark-doc/SKILL.md":
			fmt.Fprint(w, document(v))
		case "/.well-known/skills/lark-doc/references/nested/instructions.md":
			w.Write(reference(v))
		case "/.well-known/skills/lark-doc/assets/binary.dat":
			w.Write(asset(v))
		case "/.well-known/skills/lark-shared/SKILL.md":
			fmt.Fprint(w, "---\nname: lark-shared\ndescription: unselected neighbor\n---\nShared rules.\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = hostedTestTransport(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		requests++
		isOffline := offline
		mu.Unlock()
		if isOffline {
			return nil, errors.New("publisher is offline")
		}
		if req.URL.Scheme != "https" || (req.URL.Hostname() != "hosted.example" && req.URL.Hostname() != "skills.sh" && req.URL.Hostname() != "www.skills.sh") {
			return nil, fmt.Errorf("unexpected hosted fixture request: %s", req.URL)
		}
		local := req.Clone(req.Context())
		local.URL.Scheme, local.URL.Host = serverURL.Scheme, serverURL.Host
		response, err := server.Client().Transport.RoundTrip(local)
		if response != nil {
			// This transport remaps a connection, not an HTTP redirect. Keep
			// the logical final URL for resolving relative manifest files.
			response.Request = req
		}
		return response, err
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	// A hosted import and its later updates must work when Git is unavailable.
	t.Setenv("PATH", t.TempDir())
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "data")
	service, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	preview, err := service.Preview(ctx, "https://skills.sh/hosted.example/lark-doc", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Hosted || preview.Local || preview.Source != origin || preview.Ref != "" || preview.Subpath != "lark-doc" || len(preview.Candidates) != 1 || preview.Candidates[0].Subpath != "lark-doc" {
		t.Fatalf("hosted skill link lost its identity or scope: %+v", preview)
	}
	state, err := service.GetState()
	if err != nil || len(state.State.Skills) != 0 || len(state.State.Sources) != 0 {
		t.Fatalf("hosted preview wrote library state: %+v %v", state.State, err)
	}
	mu.Lock()
	version, beforeInstall := 2, requests
	mu.Unlock()
	state, err = service.Install(preview.ID, []string{"lark-doc"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	installRequests := requests
	mu.Unlock()
	if installRequests != beforeInstall {
		t.Fatal("install refetched hosted files after review")
	}
	first := sourceSkill(t, state, "lark-doc")
	if len(state.State.Skills) != 1 || len(state.State.Sources) != 1 || first.Source != origin || first.Ref != "" || first.SourceID != state.State.Sources[0].ID || first.Revision != preview.Revision {
		t.Fatalf("hosted import lost its source association: %+v", state.State)
	}
	assertHostedContent := func(s *Service, skill skillstore.Skill, v int) {
		t.Helper()
		got, err := s.ReadSkillDocument(skill.ID)
		if err != nil || got != document(v) {
			t.Fatalf("wrong reviewed hosted document: %q %v", got, err)
		}
		for path, want := range map[string][]byte{
			"references/nested/instructions.md": reference(v),
			"assets/binary.dat":                 asset(v),
		} {
			got, err := os.ReadFile(filepath.Join(root, "objects", skill.ContentHash, filepath.FromSlash(path)))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("supporting file %q changed or was omitted: %v %v", path, got, err)
			}
		}
	}
	assertHostedContent(service, first, 1)
	state, err = service.SavePreset(skillstore.Preset{Name: "Hosted documents", SkillIDs: []string{first.ID}})
	if err != nil {
		t.Fatal(err)
	}
	preset := state.State.Presets[0]
	preview, err = service.Preview(ctx, origin, "", "lark-doc")
	if err != nil {
		t.Fatal(err)
	}
	state, err = service.Install(preview.ID, []string{"lark-doc"})
	if err != nil {
		t.Fatal(err)
	}
	second := sourceSkill(t, state, "lark-doc")
	if len(state.State.Skills) != 1 || len(state.State.Sources) != 1 || second.ID != first.ID || second.SourceID != first.SourceID || second.InstalledAt != first.InstalledAt || second.ContentHash == first.ContentHash || !reflect.DeepEqual(state.State.Presets[0], preset) {
		t.Fatalf("reimport changed identity or preset association: %+v", state.State)
	}
	assertHostedContent(service, second, 2)
	mu.Lock()
	version = 3
	mu.Unlock()
	updates, err := service.CheckUpdates(ctx)
	if err != nil || len(updates) != 1 || updates[0].SkillID != first.ID || !updates[0].Available || updates[0].Removed || updates[0].Error != "" {
		t.Fatalf("hosted update was not discovered: %+v %v", updates, err)
	}
	state, err = service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	reviewed := state.State.Sources[0]
	if sourceSkill(t, state, "lark-doc").ContentHash != second.ContentHash || reviewed.Revision != updates[0].Revision {
		t.Fatal("update check changed installed content or failed to retain its reviewed revision")
	}
	assertHostedContent(service, second, 2)
	// Refresh releases its preview. A new service must still be able to sync
	// that exact reviewed content after the publisher changes or disappears.
	mu.Lock()
	version, offline, beforeSync := 4, true, requests
	mu.Unlock()
	restarted, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	state, err = restarted.SyncSource(ctx, reviewed.ID, reviewed.Revision, []string{"lark-doc"})
	if err != nil {
		t.Fatalf("reviewed hosted snapshot did not survive release and restart: %v", err)
	}
	mu.Lock()
	syncRequests := requests
	offline, unavailable = false, true
	mu.Unlock()
	third := sourceSkill(t, state, "lark-doc")
	if syncRequests != beforeSync || third.ID != first.ID || third.SourceID != first.SourceID || third.Revision != reviewed.Revision || !reflect.DeepEqual(state.State.Presets[0], preset) {
		t.Fatalf("sync did not use the persisted reviewed snapshot: %+v", state.State)
	}
	assertHostedContent(restarted, third, 3)
	lastCatalog := state.State.Sources[0].Catalog
	state, err = restarted.RefreshSource(ctx, reviewed.ID)
	if err != nil || state.State.Sources[0].LastError == "" || !reflect.DeepEqual(state.State.Sources[0].Catalog, lastCatalog) || sourceSkill(t, state, "lark-doc").ContentHash != third.ContentHash || sourceSkill(t, state, "lark-doc").UpstreamRemoved {
		t.Fatalf("failed hosted refresh changed local content or catalog: %+v %v", state.State, err)
	}
	if _, err = restarted.SyncSource(ctx, reviewed.ID, reviewed.Revision, []string{"lark-doc"}); err == nil {
		t.Fatal("failed hosted refresh still allowed synchronization")
	}
	assertHostedContent(restarted, third, 3)
	if !strings.HasPrefix(third.Source, "https://") {
		t.Fatal("hosted source identity was replaced with the local TLS test address")
	}
}
