package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const hostedV02Schema = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"
const hostedAlphaDocument = "---\nname: alpha\ndescription: Hosted alpha fixture\n---\nReviewed hosted skill.\n"

func hostedPreviewFixture(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client := New(t.TempDir())
	client.http = server.Client()
	return client, server
}

func assertHostedPreviewFailure(t *testing.T, client *Client, source, reason string) {
	t.Helper()
	preview, err := client.Preview(context.Background(), source, "", "")
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("expected %q failure; preview=%+v error=%v", reason, preview, err)
	}
	if preview.ID != "" || len(client.previews) != 0 {
		t.Fatalf("failed hosted preview offered candidates: %+v", preview)
	}
	entries, err := os.ReadDir(filepath.Join(client.cacheDir, "previews"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed hosted preview retained partial content: %v", entries)
	}
}

func TestParseHostedSources(t *testing.T) {
	for _, row := range []struct {
		input, source, skill, subpath string
	}{
		{"skills.sh/open.feishu.cn/lark-doc", "https://open.feishu.cn", "lark-doc", "lark-doc"},
		{"https://skills.sh/site/open.feishu.cn/lark-doc", "https://open.feishu.cn", "lark-doc", "lark-doc"},
		{"https://www.skills.sh/OPEN.FEISHU.CN/lark-doc", "https://open.feishu.cn", "lark-doc", "lark-doc"},
		{"https://open.feishu.cn/", "https://open.feishu.cn", "", "."},
		{"https://open.feishu.cn/.well-known/skills/index.json", "https://open.feishu.cn/.well-known/skills", "", "."},
		{"https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/index.json", "https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills", "", "."},
	} {
		t.Run(row.input, func(t *testing.T) {
			spec, err := parseSource(row.input, "", "")
			if err != nil || !spec.hosted || spec.source != row.source || spec.skill != row.skill || spec.subpath != row.subpath || spec.ref != "" {
				t.Fatalf("hosted identity or scope changed: %+v %v", spec, err)
			}
			if _, err := parseSource(row.input, "main", ""); err == nil {
				t.Fatal("hosted source accepted a Git ref")
			}
		})
	}
	for _, input := range []string{
		"https://skills.sh/bad..example/alpha", "https://skills.sh/-bad.example/alpha", "https://skills.sh/bad-.example/alpha", "https://skills.sh/localhost/alpha",
		"https://bad..example", "https://-bad.example", "https://bad-.example", "https://user:password@hosted.example",
		"https://hosted.example/.well-known/skills?token=secret", "https://hosted.example/prefix/../.well-known/skills", "http://hosted.example",
	} {
		if _, err := parseSource(input, "", ""); err == nil {
			t.Errorf("accepted malformed hosted source %q", input)
		}
	}
}

func TestHostedPreviewLegacyFallbackPreservesScopedFiles(t *testing.T) {
	var neighborRequests atomic.Int32
	client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-skills/index.json":
			fmt.Fprint(w, "<html>Host fallback</html>")
		case "/.well-known/skills/index.json":
			fmt.Fprint(w, `{"skills":[{"name":"alpha","description":"Alpha","files":["SKILL.md","references/nested/guide.md","assets/binary.dat"]},{"name":"beta","description":"Beta","files":["SKILL.md"]}]}`)
		case "/.well-known/skills/alpha/SKILL.md":
			// The catalog slug remains stable when SKILL.md has a different name.
			fmt.Fprint(w, strings.Replace(hostedAlphaDocument, "name: alpha", "name: alpha-skill", 1))
		case "/.well-known/skills/alpha/references/nested/guide.md":
			fmt.Fprint(w, "Nested reference")
		case "/.well-known/skills/alpha/assets/binary.dat":
			w.Write([]byte{0, 1, 0xff, 2})
		case "/.well-known/skills/beta/SKILL.md":
			neighborRequests.Add(1)
			http.Error(w, "unselected skill should not be downloaded", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})
	preview, err := client.Preview(context.Background(), server.URL, "", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Hosted || preview.Local || preview.Source != server.URL || preview.Ref != "" || preview.Subpath != "alpha" || len(preview.Candidates) != 1 || preview.Candidates[0].Name != "alpha-skill" || neighborRequests.Load() != 0 {
		t.Fatalf("bad scoped hosted preview: %+v", preview)
	}
	candidates, err := client.Candidates(preview.ID, []string{"alpha"})
	if err != nil || len(candidates) != 1 || candidates[0].Skill.Source != server.URL || candidates[0].Skill.Subpath != "alpha" {
		t.Fatalf("hosted source metadata was lost: %+v %v", candidates, err)
	}
	for file, want := range map[string]string{"references/nested/guide.md": "Nested reference", "assets/binary.dat": string([]byte{0, 1, 0xff, 2})} {
		content, err := os.ReadFile(filepath.Join(candidates[0].Path, filepath.FromSlash(file)))
		if err != nil || string(content) != want {
			t.Fatalf("hosted file %s changed: %q %v", file, content, err)
		}
	}
	if _, err := client.Candidates(preview.ID, []string{"beta"}); err == nil {
		t.Fatal("scoped preview offered an unselected skill")
	}
}

func TestHostedPreviewV02VerifiedArtifacts(t *testing.T) {
	for _, kind := range []string{"skill-md", "tar.gz", "zip"} {
		t.Run(kind, func(t *testing.T) {
			body, artifactType := []byte(hostedAlphaDocument), "skill-md"
			if kind != "skill-md" {
				artifactType = "archive"
				body = makeHostedTestArchive(t, kind, []hostedArchiveTestEntry{{name: "alpha/SKILL.md", body: hostedAlphaDocument, mode: 0644}, {name: "alpha/references/guide.md", body: "Archive reference", mode: 0644}})
			}
			hash := sha256.Sum256(body)
			client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/service/.well-known/agent-skills/index.json":
					json.NewEncoder(w).Encode(map[string]any{"$schema": hostedV02Schema, "skills": []hostedEntry{{Name: "alpha", Description: "Alpha", Type: artifactType, URL: "artifacts/alpha", Digest: "sha256:" + hex.EncodeToString(hash[:])}}})
				case "/service/.well-known/agent-skills/artifacts/alpha":
					w.Write(body)
				default:
					http.NotFound(w, r)
				}
			})
			source := server.URL + "/service/.well-known/agent-skills"
			preview, err := client.Preview(context.Background(), source, "", "")
			if err != nil || !preview.Hosted || preview.Source != source || len(preview.Candidates) != 1 {
				t.Fatalf("verified hosted artifact failed: %+v %v", preview, err)
			}
			candidates, err := client.Candidates(preview.ID, []string{"alpha"})
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(candidates[0].Path, "SKILL.md"))
			if err != nil || string(content) != hostedAlphaDocument {
				t.Fatalf("verified document changed: %q %v", content, err)
			}
			if kind != "skill-md" {
				if content, err := os.ReadFile(filepath.Join(candidates[0].Path, "references", "guide.md")); err != nil || string(content) != "Archive reference" {
					t.Fatalf("archive asset missing: %q %v", content, err)
				}
			}
		})
	}
}

func TestHostedPreviewRejectsMissingAndWrongArtifactDigests(t *testing.T) {
	for _, artifactType := range []string{"skill-md", "archive"} {
		for _, digest := range []string{"", "sha256:abc", "sha256:" + strings.Repeat("0", 64)} {
			t.Run(artifactType+"/"+digest, func(t *testing.T) {
				client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/index.json") {
						json.NewEncoder(w).Encode(map[string]any{"$schema": hostedV02Schema, "skills": []hostedEntry{{Name: "alpha", Description: "Alpha", Type: artifactType, URL: "artifact", Digest: digest}}})
						return
					}
					fmt.Fprint(w, hostedAlphaDocument)
				})
				assertHostedPreviewFailure(t, client, server.URL+"/.well-known/agent-skills", "digest")
			})
		}
	}
}

func TestHostedPreviewRejectsUnknownSchemas(t *testing.T) {
	for _, schema := range []string{"https://schemas.agentskills.io/discovery/99.0.0/schema.json", "https://example.com/discovery/schema.json"} {
		t.Run(schema, func(t *testing.T) {
			client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"$schema": schema, "skills": []hostedEntry{{Name: "alpha", Description: "Alpha", Files: []string{"SKILL.md"}}}})
			})
			assertHostedPreviewFailure(t, client, server.URL+"/.well-known/agent-skills", "schema")
		})
	}
}

func TestHostedPreviewRejectsUnsafeLegacyFileManifests(t *testing.T) {
	for _, files := range [][]string{
		{"SKILL.md", "../outside"}, {"SKILL.md", "references/../outside"}, {"SKILL.md", "/outside"},
		{"SKILL.md", ".Git/config"}, {"SKILL.md", "references/.git/config"}, {"SKILL.md", "references/A.md", "references/a.md"},
		{"SKILL.md", "C:\\outside"}, {"references/guide.md"},
	} {
		t.Run(strings.Join(files, ","), func(t *testing.T) {
			client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"skills": []hostedEntry{{Name: "alpha", Description: "Alpha", Files: files}}})
			})
			preview, err := client.Preview(context.Background(), server.URL+"/.well-known/skills", "", "")
			if err == nil || preview.ID != "" || len(client.previews) != 0 {
				t.Fatalf("accepted unsafe manifest %v: %+v %v", files, preview, err)
			}
		})
	}
}

func TestHostedPreviewRejectsInvalidMetadataAndPartialDownloads(t *testing.T) {
	for _, row := range []struct {
		name, index, document, reason string
	}{
		{"duplicate names", `{"skills":[{"name":"alpha","description":"Alpha","files":["SKILL.md"]},{"name":"ALPHA","description":"Duplicate","files":["SKILL.md"]}]}`, hostedAlphaDocument, "metadata"},
		{"unsafe name", `{"skills":[{"name":"../alpha","description":"Alpha","files":["SKILL.md"]}]}`, hostedAlphaDocument, "metadata"},
		{"missing description", `{"skills":[{"name":"alpha","description":"","files":["SKILL.md"]}]}`, hostedAlphaDocument, "metadata"},
		{"invalid document", `{"skills":[{"name":"alpha","description":"Alpha","files":["SKILL.md"]}]}`, "not YAML frontmatter", "metadata"},
		{"missing asset after valid skill", `{"skills":[{"name":"alpha","description":"Alpha","files":["SKILL.md"]},{"name":"beta","description":"Beta","files":["SKILL.md","missing.md"]}]}`, hostedAlphaDocument, "HTTP 404"},
	} {
		t.Run(row.name, func(t *testing.T) {
			client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/index.json"):
					fmt.Fprint(w, row.index)
				case strings.HasSuffix(r.URL.Path, "/SKILL.md"):
					fmt.Fprint(w, strings.Replace(row.document, "name: alpha", "name: "+filepath.Base(filepath.Dir(r.URL.Path)), 1))
				default:
					http.NotFound(w, r)
				}
			})
			assertHostedPreviewFailure(t, client, server.URL+"/.well-known/skills", row.reason)
		})
	}
}

func TestHostedPreviewCancellationAndIndexLimit(t *testing.T) {
	t.Run("cancellation interrupts a file request", func(t *testing.T) {
		started := make(chan struct{})
		client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/index.json") {
				fmt.Fprint(w, `{"skills":[{"name":"alpha","description":"Alpha","files":["SKILL.md"]}]}`)
				return
			}
			close(started)
			<-r.Context().Done()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := client.Preview(ctx, server.URL+"/.well-known/skills", "", "")
			result <- err
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("hosted file request never started")
		}
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) || len(client.previews) != 0 {
				t.Fatalf("cancellation did not stop preview: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("canceled hosted preview did not stop")
		}
	})
	t.Run("oversized index is rejected", func(t *testing.T) {
		client, server := hostedPreviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, strings.Repeat(" ", maxSearchBytes+1))
		})
		assertHostedPreviewFailure(t, client, server.URL+"/.well-known/skills", "download limit")
	})
}
