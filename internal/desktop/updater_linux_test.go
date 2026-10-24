package desktop

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"racomi/internal/buildinfo"
	"runtime"
	"strings"
	"testing"
)

type fixedTransport map[string][]byte

func (f fixedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, ok := f[request.URL.String()]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
}

// The staged executable reports a build identity; only the exact release the
// feed names may replace the running stable CI build.
func TestLinuxUpdaterStagesOnlyVerifiedReleaseIdentity(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("release fixtures are x86_64")
	}
	public, private, _ := ed25519.GenerateKey(nil)
	originalFeed, originalKey, originalClient := buildinfo.FeedURL, buildinfo.PublicKey, updateHTTPClient
	defer func() {
		buildinfo.FeedURL, buildinfo.PublicKey, updateHTTPClient = originalFeed, originalKey, originalClient
	}()
	buildinfo.FeedURL = "https://example.test/linux-update.json"
	buildinfo.PublicKey = base64.StdEncoding.EncodeToString(public)
	current := buildinfo.Current()

	script := func(info buildinfo.Info) string {
		data, _ := json.Marshal(info)
		return fmt.Sprintf("#!/bin/sh\n[ \"$1\" = --build-info ] && printf '%%s\\n' '%s'\n", data)
	}
	release := buildinfo.Info{ID: current.ID, Channel: "stable", Executor: "ci", OS: "linux", Arch: "amd64", Version: "9.0.0", Commit: testCommit}
	for name, test := range map[string]struct {
		info buildinfo.Info
		ok   bool
	}{
		"exact release":  {release, true},
		"other version":  {func() buildinfo.Info { i := release; i.Version = "8.0.0"; return i }(), false},
		"other commit":   {func() buildinfo.Info { i := release; i.Commit = strings.Repeat("f", 40); return i }(), false},
		"local build":    {func() buildinfo.Info { i := release; i.Executor = "local"; return i }(), false},
		"other identity": {func() buildinfo.Info { i := release; i.ID = "com.example.other"; return i }(), false},
	} {
		t.Run(name, func(t *testing.T) {
			archiveName := "racomi-9.0.0-x86_64-linux"
			archive := linuxArchiveFixture(t, map[string]string{archiveName + "/racomi": script(test.info)})
			feedData := signedFeed(t, "9.0.0", archive, private)
			feed, asset, err := parseLinuxUpdateFeed(feedData, "amd64")
			if err != nil {
				t.Fatal(err)
			}
			updateHTTPClient = &http.Client{Transport: fixedTransport{buildinfo.FeedURL: feedData, asset.URL: archive}}
			fetched, _, err := fetchLinuxFeed()
			if err != nil || fetched.Version != "9.0.0" {
				t.Fatalf("feed %v: %v", fetched, err)
			}
			dir := t.TempDir()
			u := &linuxUpdater{executable: filepath.Join(dir, "racomi")}
			staged, err := u.download(feed, asset)
			leftovers, _ := filepath.Glob(filepath.Join(dir, updatePrefix+"*"))
			if !test.ok {
				if err == nil || len(leftovers) != 0 {
					t.Fatalf("unverified release staged: %v, %v", err, leftovers)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(staged)
			if err != nil || info.Mode().Perm() != 0o755 || filepath.Dir(staged) != dir {
				t.Fatalf("staged %s: %v %v", staged, info, err)
			}
		})
	}
}

func TestAutomaticUpdatePreferenceDefaultsOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater.json")
	if !loadAutomaticUpdates(path) || !loadAutomaticUpdates("") {
		t.Fatal("automatic checks must default on")
	}
	os.WriteFile(path, []byte(`{"automatic":false}`), 0o600)
	if loadAutomaticUpdates(path) {
		t.Fatal("saved preference ignored")
	}
}
