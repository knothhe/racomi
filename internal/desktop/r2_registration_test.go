package desktop

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/knothhe/r2connect"
)

func TestR2RegistrationPrecedenceAndLegacyFallback(t *testing.T) {
	const embedded = `{"ClientID":"embedded-client","RedirectURL":"http://localhost:18796/oauth/cloudflare/callback"}`
	for _, test := range []struct {
		name, json, legacy, buildID, buildRedirect, envID, envRedirect, wantID, wantRedirect string
	}{
		{name: "embedded", json: embedded, legacy: "legacy-client", wantID: "embedded-client", wantRedirect: r2OAuthRedirectURI},
		{name: "build overrides embedded", json: embedded, buildID: "build-client", buildRedirect: "http://127.0.0.1:18797/oauth/cloudflare/callback", wantID: "build-client", wantRedirect: "http://127.0.0.1:18797/oauth/cloudflare/callback"},
		{name: "environment overrides build", json: embedded, buildID: "build-client", buildRedirect: "http://127.0.0.1:18797/oauth/cloudflare/callback", envID: " env-client ", envRedirect: "http://localhost:18798/oauth/cloudflare/callback", wantID: "env-client", wantRedirect: "http://localhost:18798/oauth/cloudflare/callback"},
		{name: "legacy fallback", json: `{"ClientID":""}`, legacy: "legacy-client", wantID: "legacy-client", wantRedirect: r2OAuthRedirectURI},
		{name: "blank registration stays unavailable", json: `{"ClientID":""}`, wantRedirect: r2OAuthRedirectURI},
	} {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == "RACOMI_CLOUDFLARE_CLIENT_ID" {
					return test.envID
				}
				if key == "RACOMI_CLOUDFLARE_REDIRECT_URL" {
					return test.envRedirect
				}
				return ""
			}
			got, err := loadR2Registration([]byte(test.json), test.legacy, test.buildID, test.buildRedirect, getenv)
			if err != nil || got.ClientID != test.wantID || got.RedirectURL != test.wantRedirect {
				t.Fatalf("incorrect application registration: %+v, %v", got, err)
			}
		})
	}
}

func TestR2RegistrationRejectsMalformedOrOffDeviceCallbacks(t *testing.T) {
	getenv := func(string) string { return "" }
	for _, data := range []string{"", `null`, `[]`, `{"ClientID":`, `{"ClientID":123}`, `{"ClientID":"client","ClientSecret":"never-accepted"}`, `{} {}`, `{"ClientID":"bad\nclient"}`} {
		if _, err := loadR2Registration([]byte(data), "", "", "", getenv); !errors.Is(err, errR2Registration) {
			t.Fatalf("accepted malformed registration %q: %v", data, err)
		}
	}
	for _, value := range []string{
		"https://localhost:18796/oauth/cloudflare/callback", "http://example.com:18796/oauth/cloudflare/callback",
		"http://user@localhost:18796/oauth/cloudflare/callback", "http://localhost:18796/oauth/cloudflare/callback#fragment",
		"http://localhost:18796/other", "http://localhost:18796/%6fauth/cloudflare/callback",
	} {
		if err := checkR2CallbackURL(value); !errors.Is(err, errR2Registration) {
			t.Fatalf("accepted off-device or ambiguous callback %q: %v", value, err)
		}
	}
	for _, value := range []string{r2OAuthRedirectURI, "http://127.0.0.1:18796/oauth/cloudflare/callback", "http://[::1]:18796/oauth/cloudflare/callback"} {
		if err := checkR2CallbackURL(value); err != nil {
			t.Fatalf("valid loopback callback rejected: %s: %v", value, err)
		}
	}
}

func TestR2RegistrationRuntimePreservesExistingMetadataAndManualConfiguration(t *testing.T) {
	t.Setenv("RACOMI_CLOUDFLARE_CLIENT_ID", "")
	t.Setenv("RACOMI_CLOUDFLARE_REDIRECT_URL", "")
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manual := cloudFixtureConfig()
	if _, err = s.SaveCloudSyncConfig(manual); err != nil {
		t.Fatal(err)
	}
	metadata := r2connect.State{Version: 1, ClientID: "legacy-client", Connections: []r2connect.Connection{}}
	if err = (r2connect.FileStateStore{Path: s.r2.path}).Save(metadata); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.r2.path)
	if err != nil {
		t.Fatal(err)
	}
	s.r2 = newR2OAuthRuntimeWithRegistration(s.store.Root(), []byte(`{"ClientID":""}`))
	if s.r2.client.ClientID != "legacy-client" || s.r2.unavailable() != nil {
		t.Fatal("legacy configured client identity was not retained")
	}
	after, err := os.ReadFile(s.r2.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("loading registration rewrote existing metadata")
	}
	s.r2 = newR2OAuthRuntimeWithRegistration(s.store.Root(), []byte(`{"ClientID":`))
	status := s.GetR2OAuthStatus()
	if status.Available || status.LastError != errR2Registration.Error() {
		t.Fatalf("malformed registration did not report unavailable: %+v", status)
	}
	if !s.GetCloudSyncStatus().Configured || s.GetCloudSyncStatus().Config.Endpoint != manual.Endpoint {
		t.Fatal("invalid registration disabled the saved manual S3 profile")
	}
	if _, err = s.StartR2OAuth(); err == nil {
		t.Fatal("invalid registration allowed OAuth to start")
	}
	if after, err = os.ReadFile(filepath.Join(s.store.Root(), "r2-oauth-state.json")); err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid registration overwrote recoverable authorization metadata")
	}
}
