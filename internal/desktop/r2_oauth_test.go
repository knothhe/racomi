package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"racomi/internal/cloudflare"
	skillstore "racomi/internal/skills"
)

const r2TestAccount = "11111111111111111111111111111111"
const r2TestBucket = "racomi-oauth-test"

type r2ControlFixture struct {
	mu              sync.Mutex
	exchanges       int
	mints           int
	deletes         int
	revokes         int
	deleteStatus    int
	lastVerifier    string
	lastPolicy      map[string]any
	omitRefresh     bool
	expiresIn       int64
	revokedTokens   []string
	exchangeStarted chan struct{}
	exchangeRelease chan struct{}
}

func (f *r2ControlFixture) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case req.Method == "POST" && req.URL.Path == "/oauth2/token":
		_ = req.ParseForm()
		f.mu.Lock()
		f.exchanges++
		f.lastVerifier = req.Form.Get("code_verifier")
		started, release := f.exchangeStarted, f.exchangeRelease
		omitRefresh := f.omitRefresh
		expiresIn := f.expiresIn
		f.mu.Unlock()
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		if release != nil {
			select {
			case <-release:
			case <-req.Context().Done():
				return
			}
		}
		if expiresIn == 0 {
			expiresIn = 3600
		}
		response := map[string]any{"access_token": "synthetic-oauth-access", "expires_in": expiresIn, "token_type": "Bearer", "scope": cloudflare.OAuthScopes}
		if !omitRefresh {
			response["refresh_token"] = "synthetic-oauth-refresh"
		}
		_ = json.NewEncoder(w).Encode(response)
	case req.Method == "POST" && req.URL.Path == "/oauth2/revoke":
		_ = req.ParseForm()
		f.mu.Lock()
		f.revokes++
		f.revokedTokens = append(f.revokedTokens, req.Form.Get("token"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case req.URL.Path == "/client/v4/accounts" && req.Method == "GET":
		_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"`+r2TestAccount+`","name":"Isolated account"}],"result_info":{"total_pages":1}}`)
	case req.URL.Path == "/client/v4/accounts/"+r2TestAccount+"/r2/buckets" && req.Method == "GET":
		_, _ = io.WriteString(w, `{"success":true,"result":{"buckets":[{"name":"`+r2TestBucket+`"}]}}`)
	case req.URL.Path == "/client/v4/accounts/"+r2TestAccount+"/tokens/permission_groups" && req.Method == "GET":
		_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"33333333333333333333333333333333","name":"Workers R2 Storage Bucket Item Write","is_selectable":true,"scopes":["com.cloudflare.edge.r2.bucket"]}]}`)
	case req.URL.Path == "/client/v4/accounts/"+r2TestAccount+"/tokens" && req.Method == "POST":
		var policy map[string]any
		if json.NewDecoder(req.Body).Decode(&policy) != nil {
			http.Error(w, "bad fixture request", 400)
			return
		}
		f.mu.Lock()
		f.mints++
		id := fmt.Sprintf("%032x", f.mints+100)
		f.lastPolicy = policy
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"id": id, "value": "synthetic-r2-token-" + id, "expires_on": policy["expires_on"]}})
	case strings.HasPrefix(req.URL.Path, "/client/v4/accounts/"+r2TestAccount+"/tokens/") && req.Method == "DELETE":
		f.mu.Lock()
		f.deletes++
		status := f.deleteStatus
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":10000}]}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"id": filepath.Base(req.URL.Path)}})
	default:
		http.Error(w, "unexpected isolated Cloudflare request", 404)
	}
}

func TestR2OAuthCodeOnlyGrantSyncsAfterAuthorizationExpiresAndRevokesAccessToken(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	control.mu.Lock()
	control.omitRefresh = true
	control.mu.Unlock()
	authorizeR2Fixture(t, s, urls)
	config, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal("code-only grant could not provision R2 credentials:", err)
	}
	config.Prefix, config.Passphrase = "code-only-library", "code-only synthetic encryption phrase"
	state, err := s.SaveCloudSyncConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	installR2S3Fixture(t)
	if err = s.TestCloudSyncConnection(t.Context(), state.CloudSync.Config); err != nil {
		t.Fatal("code-only connection test failed:", err)
	}
	preset, err := s.store.SavePreset(skillstore.Preset{Name: "Code-only first version"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SyncCloud(t.Context()); err != nil {
		t.Fatal("code-only sync failed:", err)
	}
	key := s.r2.oauthKey()
	data, err := s.r2.secrets.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	var tokens cloudflare.TokenSet
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens.RefreshToken != "" {
		t.Fatal("code-only authorization unexpectedly saved a refresh token")
	}
	tokens.ExpiresAt = time.Now().Add(-time.Minute)
	data, _ = json.Marshal(tokens)
	if err = s.r2.secrets.Save(key, data); err != nil {
		t.Fatal(err)
	}
	preset.Name = "Code-only sync with expired OAuth"
	if _, err = s.store.SavePreset(preset); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SyncCloud(t.Context()); err != nil {
		t.Fatal("valid S3 credentials incorrectly required a live OAuth token:", err)
	}
	backups, err := s.ListCloudBackups(t.Context())
	if err != nil || len(backups) != 2 {
		t.Fatalf("expired OAuth blocked S3 history: %d, %v", len(backups), err)
	}
	const reconnect = "Cloudflare authorization expired. Connect Cloudflare again to manage R2 credentials."
	if _, err = s.ListR2Accounts(t.Context()); err == nil || err.Error() != reconnect {
		t.Fatalf("expired code-only grant did not request reauthorization: %v", err)
	}
	if _, err = s.DisconnectR2OAuth(t.Context()); err == nil || err.Error() != reconnect {
		t.Fatalf("expired code-only grant attempted remote cleanup without reauthorization: %v", err)
	}
	control.mu.Lock()
	exchanges, deletes := control.exchanges, control.deletes
	control.mu.Unlock()
	if exchanges != 1 || deletes != 0 || !s.GetCloudSyncStatus().Configured {
		t.Fatal("expired code-only management modified credentials or attempted a refresh")
	}
	authorizeR2Fixture(t, s, urls)
	if _, err = s.DisconnectR2OAuth(t.Context()); err != nil {
		t.Fatal("reauthorized code-only disconnect failed:", err)
	}
	control.mu.Lock()
	revoked := append([]string{}, control.revokedTokens...)
	control.mu.Unlock()
	if len(revoked) != 1 || revoked[0] != "synthetic-oauth-access" {
		t.Fatal("disconnect did not explicitly revoke the access token from the code-only grant")
	}
}

func TestR2OAuthCodeOnlyShortLivedTokenRemainsUsable(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	control.mu.Lock()
	control.omitRefresh, control.expiresIn = true, 30
	control.mu.Unlock()
	authorizeR2Fixture(t, s, urls)
	accounts, err := s.ListR2Accounts(t.Context())
	if err != nil || len(accounts) != 1 {
		t.Fatalf("a still-valid code-only token was treated as expired: %v", err)
	}
	control.mu.Lock()
	exchanges := control.exchanges
	control.mu.Unlock()
	if exchanges != 1 {
		t.Fatal("a short-lived code-only token triggered an unsupported refresh")
	}
}

func r2TestService(t *testing.T) (*Service, *r2ControlFixture, *memoryR2SecretBackend, <-chan string) {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	control := &r2ControlFixture{}
	server := httptest.NewServer(control)
	t.Cleanup(server.Close)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	backend := &memoryR2SecretBackend{items: map[string][]byte{}}
	urls := make(chan string, 3)
	r := newR2OAuthRuntime(s.store.Root())
	r.client = cloudflare.NewClient("synthetic-racomi-client")
	r.client.APIBaseURL, r.client.OAuthBaseURL = server.URL+"/client/v4", server.URL+"/oauth2"
	// Capture the original transport, since the S3 fixture below only intercepts
	// its one expected official R2 hostname through DefaultTransport.
	r.client.HTTPClient = &http.Client{Transport: http.DefaultTransport, Timeout: 3 * time.Second}
	r.secrets = &r2SystemSecretStore{service: r2SecretService(s.store.Root()), backend: backend}
	r.redirectURI = fmt.Sprintf("http://localhost:%d/oauth/cloudflare/callback", port)
	r.openURL = func(value string) error { urls <- value; return nil }
	s.r2 = r
	t.Cleanup(func() {
		if err := s.ServiceShutdown(); err != nil {
			t.Error(err)
		}
	})
	return s, control, backend, urls
}

func beginR2Authorization(t *testing.T, s *Service, urls <-chan string) (*url.URL, url.Values) {
	t.Helper()
	status, err := s.StartR2OAuth()
	if err != nil || !status.Authorizing {
		t.Fatalf("could not begin isolated OAuth: %v", err)
	}
	select {
	case value := <-urls:
		auth, err := url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		query := auth.Query()
		if query.Get("code_challenge_method") != "S256" || query.Get("state") == "" || query.Get("client_secret") != "" {
			t.Fatal("native authorization did not use PKCE without a client secret")
		}
		callback, err := url.Parse(query.Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		return callback, query
	case <-time.After(3 * time.Second):
		t.Fatal("authorization did not open the browser")
	}
	return nil, nil
}

func r2Callback(t *testing.T, callback *url.URL, query url.Values) (int, error) {
	t.Helper()
	u := *callback
	u.RawQuery = query.Encode()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(u.String())
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, nil
}

func authorizeR2Fixture(t *testing.T, s *Service, urls <-chan string) {
	t.Helper()
	callback, query := beginR2Authorization(t, s, urls)
	code, err := r2Callback(t, callback, url.Values{"state": {query.Get("state")}, "code": {"synthetic-authorization-code"}})
	if err != nil || code != 200 {
		t.Fatalf("isolated OAuth callback failed: HTTP %d, %v", code, err)
	}
	if status := s.GetR2OAuthStatus(); !status.Connected || status.Authorizing || status.LastError != "" {
		t.Fatalf("authorization did not complete: %+v", status)
	}
}

type r2FixtureObject struct {
	data []byte
	etag string
}
type r2S3Fixture struct {
	mu       sync.Mutex
	objects  map[string]r2FixtureObject
	requests int
}
type r2FixtureTransport func(*http.Request) (*http.Response, error)

func (f r2FixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func installR2S3Fixture(t *testing.T) *r2S3Fixture {
	t.Helper()
	fixture := &r2S3Fixture{objects: map[string]r2FixtureObject{}}
	previous := http.DefaultTransport
	http.DefaultTransport = r2FixtureTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != r2TestAccount+".r2.cloudflarestorage.com" {
			return previous.RoundTrip(req)
		}
		recorder := httptest.NewRecorder()
		fixture.ServeHTTP(recorder, req)
		response := recorder.Result()
		response.Request = req
		return response, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	return fixture
}

func (f *r2S3Fixture) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if !strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") || req.Header.Get("X-Amz-Security-Token") != "" {
		w.WriteHeader(403)
		return
	}
	if req.URL.Path != "/"+r2TestBucket && !strings.HasPrefix(req.URL.Path, "/"+r2TestBucket+"/") {
		w.WriteHeader(404)
		return
	}
	key := strings.TrimPrefix(req.URL.Path, "/"+r2TestBucket+"/")
	if req.URL.Query().Get("list-type") == "2" {
		var keys []string
		for key := range f.objects {
			if strings.HasPrefix(key, req.URL.Query().Get("prefix")) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		page := struct {
			XMLName  xml.Name `xml:"ListBucketResult"`
			Contents []struct {
				Key string `xml:"Key"`
			} `xml:"Contents"`
		}{}
		for _, key := range keys {
			page.Contents = append(page.Contents, struct {
				Key string `xml:"Key"`
			}{key})
		}
		_ = xml.NewEncoder(w).Encode(page)
		return
	}
	object, exists := f.objects[key]
	switch req.Method {
	case "GET", "HEAD":
		if !exists {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", object.etag)
		w.Header().Set("Content-Length", fmt.Sprint(len(object.data)))
		if req.Method == "GET" {
			_, _ = w.Write(object.data)
		}
	case "PUT":
		if req.Header.Get("If-None-Match") == "*" && exists || req.Header.Get("If-Match") != "" && (!exists || req.Header.Get("If-Match") != object.etag) {
			w.WriteHeader(412)
			return
		}
		data, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		hash := sha256.Sum256(data)
		etag := `"` + hex.EncodeToString(hash[:]) + `"`
		f.objects[key] = r2FixtureObject{bytes.Clone(data), etag}
		w.Header().Set("ETag", etag)
	case "DELETE":
		delete(f.objects, key)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func TestR2OAuthProvisionedSyncMatchesManualConfiguration(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	manualBefore := cloudFixtureConfig()
	if _, err := s.SaveCloudSyncConfig(manualBefore); err != nil {
		t.Fatal(err)
	}
	authorizeR2Fixture(t, s, urls)
	accounts, err := s.ListR2Accounts(t.Context())
	if err != nil || len(accounts) != 1 || accounts[0].ID != r2TestAccount {
		t.Fatalf("account enumeration failed: %v", err)
	}
	buckets, err := s.ListR2Buckets(t.Context(), r2TestAccount, "default")
	if err != nil || len(buckets) != 1 {
		t.Fatalf("bucket enumeration failed: %v", err)
	}
	r2, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	if s.GetCloudSyncStatus().Config.Endpoint != manualBefore.Endpoint {
		t.Fatal("choosing an OAuth bucket changed the active sync destination before Save")
	}
	r2.Prefix, r2.Passphrase = "same-library", "same client-side encryption passphrase"
	state, err := s.SaveCloudSyncConfig(r2)
	if err != nil {
		t.Fatal(err)
	}
	if state.CloudSync.Config.Provider != "r2-oauth" || !state.CloudSync.Configured || !state.CloudSync.Config.HasSecretAccessKey {
		t.Fatal("OAuth configuration was not activated")
	}
	credentials, err := s.r2ConfigForUse(t.Context(), r2)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte("synthetic-r2-token-" + credentials.AccessKeyID))
	if credentials.SecretAccessKey != hex.EncodeToString(expected[:]) {
		t.Fatal("OAuth created credentials were not derived from the API token")
	}
	control.mu.Lock()
	policy, exchanges := control.lastPolicy, control.exchanges
	verifier := control.lastVerifier
	control.mu.Unlock()
	policies, ok := policy["policies"].([]any)
	if !ok || len(policies) != 1 {
		t.Fatal("provisioned token did not have exactly one access policy")
	}
	resources := policies[0].(map[string]any)["resources"].(map[string]any)
	if len(resources) != 1 || resources["com.cloudflare.edge.r2.bucket."+r2TestAccount+"_default_"+r2TestBucket] != "*" {
		t.Fatal("provisioned token was not restricted to the chosen bucket")
	}
	if exchanges != 1 || len(verifier) < 43 {
		t.Fatal("authorization code was not exchanged with a native PKCE verifier")
	}
	public, _ := json.Marshal(state)
	for _, path := range []string{s.cloud.path, s.r2.path} {
		disk, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"synthetic-oauth-access", "synthetic-oauth-refresh", credentials.SecretAccessKey} {
			if bytes.Contains(disk, []byte(secret)) {
				t.Fatal("OAuth secrets entered JSON configuration")
			}
		}
	}
	for _, secret := range []string{"synthetic-oauth-access", "synthetic-oauth-refresh", credentials.SecretAccessKey, r2.Passphrase} {
		if bytes.Contains(public, []byte(secret)) {
			t.Fatal("OAuth secrets or encryption passphrase entered public state")
		}
	}
	if state.CloudSync.R2Config.AccessKeyID != "" || state.CloudSync.Config.AccessKeyID != "" {
		t.Fatal("managed access key was returned to the frontend")
	}
	fixture := installR2S3Fixture(t)
	if err = s.TestCloudSyncConnection(t.Context(), state.CloudSync.Config); err != nil {
		t.Fatal(err)
	}
	skillDir := t.TempDir()
	const document = "---\nname: oauth-integration\ndescription: Isolated sync verification\n---\n# OAuth content survives both connection methods\n"
	if err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := s.store.Install([]skillstore.Candidate{{Skill: skillstore.Skill{Name: "oauth-integration", Description: "Isolated sync verification"}, Path: skillDir}})
	if err != nil {
		t.Fatal(err)
	}
	preset, err := s.store.SavePreset(skillstore.Preset{Name: "OAuth first version", SkillIDs: []string{installed[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SyncCloud(t.Context()); err != nil {
		t.Fatal(err)
	}
	backups, err := s.ListCloudBackups(t.Context())
	if err != nil || len(backups) != 1 {
		t.Fatalf("OAuth backup listing failed: %v", err)
	}
	manual, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manualConfig := credentials
	manualConfig.Provider = "s3"
	manualConfig.R2ConnectionID = ""
	manualConfig.AccountID = ""
	manualConfig.Jurisdiction = ""
	if _, err = manual.SaveCloudSyncConfig(manualConfig); err != nil {
		t.Fatal(err)
	}
	manualState, err := manual.SyncCloud(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(manualState.State.Skills) != 1 || len(manualState.State.Presets) != 1 || manualState.State.Presets[0].ID != preset.ID {
		t.Fatal("manual S3 did not read the exact OAuth sync library")
	}
	loaded, err := manual.ReadSkillDocument(manualState.State.Skills[0].ID)
	if err != nil || loaded != document {
		t.Fatal("manual S3 did not restore the same encrypted skill content")
	}
	preset.Name = "Manual second version"
	if _, err = manual.store.SavePreset(preset); err != nil {
		t.Fatal(err)
	}
	if _, err = manual.SyncCloud(t.Context()); err != nil {
		t.Fatal(err)
	}
	updated, err := s.SyncCloud(t.Context())
	if err != nil || updated.State.Presets[0].Name != preset.Name {
		t.Fatalf("OAuth could not read the manual S3 update: %v", err)
	}
	if _, err = s.RestoreCloudBackup(t.Context(), backups[0].ID); err != nil {
		t.Fatal(err)
	}
	restored, err := manual.SyncCloud(t.Context())
	if err != nil || restored.State.Presets[0].Name != "OAuth first version" {
		t.Fatalf("restored history did not propagate to manual S3: %v", err)
	}
	if fixture.requests == 0 {
		t.Fatal("synchronization never used the S3 object transport")
	}
}

func TestR2OAuthProfilesSurviveSwitchAndReopen(t *testing.T) {
	s, _, backend, urls := r2TestService(t)
	manual := cloudFixtureConfig()
	manual.Provider = "s3"
	if _, err := s.SaveCloudSyncConfig(manual); err != nil {
		t.Fatal(err)
	}
	authorizeR2Fixture(t, s, urls)
	r2, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	r2.Prefix, r2.Passphrase, r2.IntervalMinutes = "oauth-profile", "OAuth profile encryption phrase", 17
	if _, err = s.SaveCloudSyncConfig(r2); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		status := s.GetCloudSyncStatus()
		if _, err = s.SaveCloudSyncConfig(status.ManualConfig); err != nil {
			t.Fatal(err)
		}
		if s.cloud.config.SecretAccessKey != manual.SecretAccessKey || s.cloud.config.Passphrase != manual.Passphrase || s.cloud.config.SessionToken != manual.SessionToken {
			t.Fatal("switching to manual S3 lost its saved credentials")
		}
		status = s.GetCloudSyncStatus()
		if !status.R2Config.HasSecretAccessKey || !status.R2Config.HasPassphrase {
			t.Fatal("the inactive OAuth profile lost its credential presence flags")
		}
		if _, err = s.SaveCloudSyncConfig(status.R2Config); err != nil {
			t.Fatal(err)
		}
		if s.cloud.config.Prefix != r2.Prefix || s.cloud.config.Passphrase != r2.Passphrase || s.cloud.config.IntervalMinutes != 17 {
			t.Fatal("switching back to OAuth lost its saved settings")
		}
	}
	reopened, err := New(s.store.Root())
	if err != nil {
		t.Fatal(err)
	}
	reopened.r2.client = s.r2.client
	reopened.r2.secrets = &r2SystemSecretStore{service: r2SecretService(s.store.Root()), backend: backend}
	status := reopened.GetCloudSyncStatus()
	if status.Config.Provider != "r2-oauth" || !status.Configured || !status.Config.HasSecretAccessKey || !status.R2Config.HasSecretAccessKey || !status.ManualConfig.HasSecretAccessKey {
		t.Fatal("reopening lost the active profile or saved credentials")
	}
	if _, err = reopened.SaveCloudSyncConfig(status.Config); err != nil {
		t.Fatal("reopened OAuth profile could not be saved without entering credentials:", err)
	}
}

func TestR2OAuthRejectsTamperedConfiguration(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	r2, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	r2.Passphrase = "isolated encryption passphrase"
	if _, err = s.SaveCloudSyncConfig(r2); err != nil {
		t.Fatal(err)
	}
	// A validation regression must fail locally rather than transmitting even
	// synthetic credentials to a user-supplied endpoint.
	previousTransport := http.DefaultTransport
	storageRequests := 0
	http.DefaultTransport = r2FixtureTransport(func(*http.Request) (*http.Response, error) {
		storageRequests++
		return nil, errors.New("unexpected storage request from invalid configuration")
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	before, _ := os.ReadFile(s.cloud.path)
	for name, change := range map[string]func(*CloudSyncConfig){
		"endpoint":     func(c *CloudSyncConfig) { c.Endpoint = "https://attacker.example" },
		"account":      func(c *CloudSyncConfig) { c.AccountID = strings.Repeat("9", 32) },
		"bucket":       func(c *CloudSyncConfig) { c.Bucket = "another-bucket" },
		"connection":   func(c *CloudSyncConfig) { c.R2ConnectionID = "forged-connection" },
		"jurisdiction": func(c *CloudSyncConfig) { c.Jurisdiction = "eu" },
		"provider":     func(c *CloudSyncConfig) { c.Provider = "unknown-provider" },
	} {
		t.Run(name, func(t *testing.T) {
			forged := r2
			change(&forged)
			if _, err := s.SaveCloudSyncConfig(forged); err == nil {
				t.Fatal("tampered managed configuration was saved")
			}
			if err := s.TestCloudSyncConnection(t.Context(), forged); err == nil {
				t.Fatal("tampered managed configuration was sent to storage")
			}
			after, _ := os.ReadFile(s.cloud.path)
			if !bytes.Equal(before, after) {
				t.Fatal("a rejected configuration modified the saved profiles")
			}
		})
	}
	control.mu.Lock()
	mints := control.mints
	control.mu.Unlock()
	if mints != 1 {
		t.Fatal("tampered configurations provisioned additional credentials")
	}
	if storageRequests != 0 {
		t.Fatal("tampered managed configuration reached the storage transport")
	}
}

func TestR2OAuthStateValidationCancellationAndTimeout(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	manual := cloudFixtureConfig()
	if _, err := s.SaveCloudSyncConfig(manual); err != nil {
		t.Fatal(err)
	}
	callback, query := beginR2Authorization(t, s, urls)
	code, err := r2Callback(t, callback, url.Values{"state": {"wrong-state"}, "code": {"must-not-exchange"}})
	if err != nil || code != 400 || !s.GetR2OAuthStatus().Authorizing {
		t.Fatal("wrong state was accepted or consumed the pending authorization")
	}
	code, err = r2Callback(t, callback, url.Values{"state": {query.Get("state"), query.Get("state")}, "code": {"must-not-exchange"}})
	if err != nil || code != 400 {
		t.Fatal("duplicate state parameters were accepted")
	}
	s.CancelR2OAuth()
	if status := s.GetR2OAuthStatus(); status.Connected || status.Authorizing {
		t.Fatal("canceled authorization remained active")
	}
	if s.GetCloudSyncStatus().Config.Endpoint != manual.Endpoint {
		t.Fatal("canceling OAuth changed the active manual destination")
	}
	control.mu.Lock()
	exchanges := control.exchanges
	control.mu.Unlock()
	if exchanges != 0 {
		t.Fatal("invalid or canceled authorization exchanged a code")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	s.r2.mu.Lock()
	s.r2.generation++
	generation := s.r2.generation
	s.r2.authorizing = true
	s.r2.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.serveR2OAuth(ctx, listener, "localhost:1", "/callback", "synthetic-state", "synthetic-verifier", s.r2.redirectURI, generation)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("expired authorization did not stop its callback listener")
	}
	if status := s.GetR2OAuthStatus(); status.Authorizing || status.LastError == "" {
		t.Fatal("authorization timeout was not reported")
	}
}

func TestR2OAuthDuplicateCallbackExchangesCodeOnce(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	control.exchangeStarted, control.exchangeRelease = make(chan struct{}, 1), make(chan struct{})
	callback, query := beginR2Authorization(t, s, urls)
	response := make(chan int, 2)
	call := func() {
		code, _ := r2Callback(t, callback, url.Values{"state": {query.Get("state")}, "code": {"single-use-code"}})
		response <- code
	}
	go call()
	select {
	case <-control.exchangeStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("exchange did not begin")
	}
	go call()
	close(control.exchangeRelease)
	for i := 0; i < 2; i++ {
		select {
		case <-response:
		case <-time.After(4 * time.Second):
			t.Fatal("duplicate callback did not finish")
		}
	}
	control.mu.Lock()
	exchanges := control.exchanges
	control.mu.Unlock()
	if exchanges != 1 {
		t.Fatal("a duplicate callback exchanged a single-use code more than once")
	}
	if !s.GetR2OAuthStatus().Connected {
		t.Fatal("the original callback did not complete authorization")
	}
}

func TestR2OAuthLegacyManualConfigurationMigration(t *testing.T) {
	root := t.TempDir()
	manual := cloudFixtureConfig()
	legacy, err := json.Marshal(savedCloudSync{Version: 1, Config: manual, LastSyncAt: "2026-10-24T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "cloud-sync-config.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	status := s.GetCloudSyncStatus()
	if !status.Configured || status.Config.Provider == "r2-oauth" || !status.ManualConfig.HasSecretAccessKey || !status.ManualConfig.HasSessionToken || status.LastSyncAt != "2026-10-24T12:00:00Z" {
		t.Fatal("legacy manual configuration was not preserved")
	}
	status.ManualConfig.IntervalMinutes = 11
	if _, err = s.SaveCloudSyncConfig(status.ManualConfig); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(s.cloud.path)
	if err != nil {
		t.Fatal(err)
	}
	var migrated savedCloudSync
	if err = json.Unmarshal(disk, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Version != 2 || migrated.ManualConfig == nil || migrated.R2Config == nil || migrated.ManualSecrets != cloudSecretsSystem {
		t.Fatal("profile migration did not keep the legacy manual profile")
	}
	if migrated.Config.SecretAccessKey != "" || migrated.ManualConfig.SecretAccessKey != "" || migrated.ManualConfig.SessionToken != "" || migrated.ManualConfig.Passphrase != "" {
		t.Fatal("legacy manual credentials stayed in the configuration file")
	}
	data, err := s.cloud.secrets.Load(cloudSecretsManualKey)
	if err != nil {
		t.Fatal(err)
	}
	var stored cloudSecrets
	if err = json.Unmarshal(data, &stored); err != nil || stored != (cloudSecrets{manual.SecretAccessKey, manual.SessionToken, manual.Passphrase}) {
		t.Fatal("profile migration changed legacy manual credentials")
	}
}

func TestR2OAuthCancellationDuringExchangeDiscardsLateAuthorization(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	control.exchangeStarted, control.exchangeRelease = make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(control.exchangeRelease) }) }
	t.Cleanup(release)
	callback, query := beginR2Authorization(t, s, urls)
	callbackDone := make(chan struct{})
	go func() {
		defer close(callbackDone)
		_, _ = r2Callback(t, callback, url.Values{"state": {query.Get("state")}, "code": {"slow-authorization-code"}})
	}()
	select {
	case <-control.exchangeStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("exchange did not begin")
	}
	canceled := make(chan R2OAuthStatus, 1)
	go func() { canceled <- s.CancelR2OAuth() }()
	select {
	case status := <-canceled:
		if status.Authorizing || status.Connected {
			t.Fatal("canceling an in-flight exchange left authorization active")
		}
	case <-time.After(300 * time.Millisecond):
		release()
		<-canceled
		t.Fatal("canceling sign-in waited for the token exchange instead of stopping it")
	}
	release()
	select {
	case <-callbackDone:
	case <-time.After(4 * time.Second):
		t.Fatal("canceled callback did not finish")
	}
	if s.GetR2OAuthStatus().Connected || s.r2.state.Authorized || len(backend.items) != 0 {
		t.Fatal("a late token response saved authorization after cancellation")
	}
}

type r2FailCredentialSaveStore struct {
	r2SecretStore
	oauthKey string
	failure  error
}

func (s r2FailCredentialSaveStore) Save(key string, data []byte) error {
	if key != s.oauthKey {
		return s.failure
	}
	return s.r2SecretStore.Save(key, data)
}

func TestR2OAuthProvisioningFailureRemovesNewTokenAndKeepsManualProfile(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	manual := cloudFixtureConfig()
	if _, err := s.SaveCloudSyncConfig(manual); err != nil {
		t.Fatal(err)
	}
	authorizeR2Fixture(t, s, urls)
	failure := errors.New("synthetic credential-store write failure")
	s.r2.secrets = r2FailCredentialSaveStore{r2SecretStore: s.r2.secrets, oauthKey: s.r2.oauthKey(), failure: failure}
	if _, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default"); !errors.Is(err, failure) {
		t.Fatal("provisioning did not report that the new credentials could not be saved")
	}
	control.mu.Lock()
	mints, deletes := control.mints, control.deletes
	control.mu.Unlock()
	if mints != 1 || deletes != 1 || len(s.r2.state.Connections) != 0 {
		t.Fatal("failed setup left an untracked remote R2 credential")
	}
	if len(backend.items) != 1 || !s.GetR2OAuthStatus().Connected || s.GetCloudSyncStatus().Config.Endpoint != manual.Endpoint {
		t.Fatal("failed setup changed the working profile or discarded the reusable OAuth authorization")
	}
}

type r2FailOnceSecretStore struct {
	r2SecretStore
	key       string
	failure   error
	remaining int
}

func (s *r2FailOnceSecretStore) Save(key string, data []byte) error {
	if key == s.key && s.remaining > 0 {
		s.remaining--
		return s.failure
	}
	return s.r2SecretStore.Save(key, data)
}

func TestR2OAuthRotatingRefreshRecoversFromSecretStoreFailure(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	key := s.r2.oauthKey()
	data, err := s.r2.secrets.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	var tokens cloudflare.TokenSet
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	tokens.ExpiresAt = time.Now().Add(-time.Minute)
	data, _ = json.Marshal(tokens)
	if err = s.r2.secrets.Save(key, data); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("synthetic rotating-token save failure")
	s.r2.secrets = &r2FailOnceSecretStore{r2SecretStore: s.r2.secrets, key: key, failure: failure, remaining: 1}
	if _, err = s.ListR2Accounts(t.Context()); !errors.Is(err, failure) {
		t.Fatal("failed refresh-token save was not reported")
	}
	control.mu.Lock()
	exchanges := control.exchanges
	control.mu.Unlock()
	if exchanges != 2 {
		t.Fatal("expired OAuth credentials did not refresh exactly once")
	}
	if _, err = s.ListR2Accounts(t.Context()); err != nil {
		t.Fatal("a transient secret-store failure prevented recovery:", err)
	}
	control.mu.Lock()
	after := control.exchanges
	control.mu.Unlock()
	if after != exchanges {
		t.Fatal("retry reused the already-consumed rotating refresh token")
	}
	if s.r2.pendingTokens != nil {
		t.Fatal("successfully saved replacement tokens remained pending")
	}
}

func TestR2OAuthCredentialRotationMetadataFailureKeepsPreviousCredentials(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	config, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	config.Passphrase = "isolated rotation encryption phrase"
	if _, err = s.SaveCloudSyncConfig(config); err != nil {
		t.Fatal(err)
	}
	s.r2.mu.Lock()
	connection := s.r2.state.Connections[0]
	credentialKey := r2ConnectionKey(connection)
	data, err := s.r2.secrets.Load(credentialKey)
	if err != nil {
		s.r2.mu.Unlock()
		t.Fatal(err)
	}
	var credentials cloudflare.Credentials
	if err = json.Unmarshal(data, &credentials); err != nil {
		s.r2.mu.Unlock()
		t.Fatal(err)
	}
	credentials.ExpiresAt = time.Now().Add(time.Minute)
	data, _ = json.Marshal(credentials)
	if err = s.r2.secrets.Save(credentialKey, data); err != nil {
		s.r2.mu.Unlock()
		t.Fatal(err)
	}
	s.r2.state.Connections[0].ExpiresAt = credentials.ExpiresAt
	if err = writeR2Metadata(s.r2.path, s.r2.state); err != nil {
		s.r2.mu.Unlock()
		t.Fatal(err)
	}
	failure := errors.New("synthetic rotation metadata write failure")
	s.r2.saveMetadata = func(string, r2OAuthState) error { return failure }
	s.r2.mu.Unlock()
	if _, err = s.r2ConfigForUse(t.Context(), config); !errors.Is(err, failure) {
		t.Fatal("rotation metadata failure was not reported")
	}
	after, err := s.r2.secrets.Load(credentialKey)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("failed rotation overwrote the previous usable credentials")
	}
	if s.r2.state.Connections[0].TokenID != connection.TokenID || r2ConnectionKey(s.r2.state.Connections[0]) != credentialKey {
		t.Fatal("failed rotation changed the active credential metadata")
	}
	control.mu.Lock()
	mints, deletes := control.mints, control.deletes
	control.mu.Unlock()
	if mints != 2 || deletes != 1 || len(backend.items) != 2 {
		t.Fatal("failed rotation did not compensate only the newly created credential")
	}
	s.r2.saveMetadata = nil
	if _, err = s.r2ConfigForUse(t.Context(), config); err != nil {
		t.Fatal("retry did not recover after a metadata write failure:", err)
	}
	if s.r2.state.Connections[0].TokenID == connection.TokenID {
		t.Fatal("successful retry did not install replacement credentials")
	}
}

func TestR2OAuthDisconnectRetriesRemoteCleanupAndPreservesManualProfile(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	manual := cloudFixtureConfig()
	if _, err := s.SaveCloudSyncConfig(manual); err != nil {
		t.Fatal(err)
	}
	authorizeR2Fixture(t, s, urls)
	if _, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default"); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.deleteStatus = http.StatusServiceUnavailable
	control.mu.Unlock()
	if _, err := s.DisconnectR2OAuth(t.Context()); err == nil {
		t.Fatal("failed remote token deletion was reported as a successful disconnect")
	}
	control.mu.Lock()
	revokes := control.revokes
	control.deleteStatus = 0
	control.mu.Unlock()
	if !s.GetR2OAuthStatus().Connected || len(s.r2.state.Connections) != 1 || len(backend.items) != 2 || revokes != 0 {
		t.Fatal("failed cleanup discarded the authorization needed for retry")
	}
	status, err := s.DisconnectR2OAuth(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.Connected || len(s.r2.state.Connections) != 0 || len(backend.items) != 0 {
		t.Fatal("disconnect did not remove remote credential metadata and local secrets")
	}
	control.mu.Lock()
	deletes, revokes := control.deletes, control.revokes
	control.mu.Unlock()
	if deletes != 2 || revokes != 1 {
		t.Fatal("disconnect did not delete the dedicated R2 token before revoking OAuth")
	}
	if s.cloud.config.Endpoint != manual.Endpoint || s.cloud.config.SecretAccessKey != manual.SecretAccessKey || s.cloud.config.Passphrase != manual.Passphrase {
		t.Fatal("disconnecting Cloudflare changed the independent manual S3 profile")
	}
}

func TestR2OAuthDiscardProtectsSavedProfilesAndCleansAbandonedCandidates(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	manual := cloudFixtureConfig()
	if _, err := s.SaveCloudSyncConfig(manual); err != nil {
		t.Fatal(err)
	}
	authorizeR2Fixture(t, s, urls)
	saved, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	saved.Passphrase = "saved profile encryption phrase"
	if _, err = s.SaveCloudSyncConfig(saved); err != nil {
		t.Fatal(err)
	}
	if err = s.DiscardR2Connection(t.Context(), saved.R2ConnectionID); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	deletes := control.deletes
	control.mu.Unlock()
	if deletes != 0 || len(backend.items) != 2 {
		t.Fatal("closing an editor revoked its just-saved active OAuth profile")
	}
	if _, err = s.SaveCloudSyncConfig(s.GetCloudSyncStatus().ManualConfig); err != nil {
		t.Fatal(err)
	}
	if err = s.DiscardR2Connection(t.Context(), saved.R2ConnectionID); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	deletes = control.deletes
	control.mu.Unlock()
	if deletes != 0 || len(backend.items) != 2 {
		t.Fatal("discarding an editor revoked the saved inactive OAuth profile")
	}
	candidate, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.deleteStatus = http.StatusServiceUnavailable
	control.mu.Unlock()
	if err = s.DiscardR2Connection(t.Context(), candidate.R2ConnectionID); err == nil {
		t.Fatal("failed candidate deletion was reported as success")
	}
	if len(s.r2.state.Connections) != 2 || len(backend.items) != 3 {
		t.Fatal("failed candidate deletion discarded cleanup information")
	}
	control.mu.Lock()
	control.deleteStatus = 0
	control.mu.Unlock()
	if err = s.DiscardR2Connection(t.Context(), candidate.R2ConnectionID); err != nil {
		t.Fatal(err)
	}
	if err = s.DiscardR2Connection(t.Context(), candidate.R2ConnectionID); err != nil {
		t.Fatal("discarding an absent candidate was not idempotent:", err)
	}
	if len(s.r2.state.Connections) != 1 || s.r2.state.Connections[0].ID != saved.R2ConnectionID || len(backend.items) != 2 {
		t.Fatal("discarding a candidate changed the saved OAuth profile")
	}
	if s.GetCloudSyncStatus().Config.Endpoint != manual.Endpoint {
		t.Fatal("discarding an OAuth candidate switched the active manual profile")
	}
}

func TestR2OAuthSavingReplacementCleansPreviousConnection(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	first, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	first.Passphrase = "first saved encryption phrase"
	if _, err = s.SaveCloudSyncConfig(first); err != nil {
		t.Fatal(err)
	}
	second, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	second.Passphrase = "second saved encryption phrase"
	if _, err = s.SaveCloudSyncConfig(second); err != nil {
		t.Fatal(err)
	}
	if len(s.r2.state.Connections) != 1 || s.r2.state.Connections[0].ID != second.R2ConnectionID || len(backend.items) != 2 {
		t.Fatal("saving a replacement left the previous managed credentials active")
	}
	control.mu.Lock()
	deletes := control.deletes
	control.mu.Unlock()
	if deletes != 1 || s.GetCloudSyncStatus().Config.R2ConnectionID != second.R2ConnectionID {
		t.Fatal("replacement setup revoked the wrong connection")
	}
}

func TestR2OAuthSaveRacingEditorCloseNeverRevokesCommittedCredentials(t *testing.T) {
	s, _, _, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	for iteration := 0; iteration < 8; iteration++ {
		candidate, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
		if err != nil {
			t.Fatal(err)
		}
		candidate.Passphrase = "racing save encryption phrase"
		s.cloud.runMu.Lock()
		saveDone, discardDone := make(chan error, 1), make(chan error, 1)
		go func() { _, err := s.SaveCloudSyncConfig(candidate); saveDone <- err }()
		go func() { discardDone <- s.DiscardR2Connection(t.Context(), candidate.R2ConnectionID) }()
		s.cloud.runMu.Unlock()
		var saveErr error
		select {
		case saveErr = <-saveDone:
		case <-time.After(4 * time.Second):
			t.Fatal("save deadlocked with editor close")
		}
		select {
		case err = <-discardDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("candidate cleanup deadlocked with save")
		}
		if saveErr == nil {
			if s.GetCloudSyncStatus().Config.R2ConnectionID != candidate.R2ConnectionID {
				t.Fatal("successful save did not select its candidate")
			}
			if _, err = s.r2ConfigForUse(t.Context(), s.cloud.config); err != nil {
				t.Fatal("closing the editor revoked successfully committed credentials:", err)
			}
		} else if s.GetCloudSyncStatus().Config.R2ConnectionID == candidate.R2ConnectionID {
			t.Fatal("a failed save activated an already-discarded candidate")
		}
	}
}

func TestR2OAuthRetiredCredentialCleanupCanRetryAfterRotation(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	config, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	config.Passphrase = "retired credential encryption phrase"
	if _, err = s.SaveCloudSyncConfig(config); err != nil {
		t.Fatal(err)
	}
	connection := s.r2.state.Connections[0]
	data, err := s.r2.secrets.Load(r2ConnectionKey(connection))
	if err != nil {
		t.Fatal(err)
	}
	var credentials cloudflare.Credentials
	if err = json.Unmarshal(data, &credentials); err != nil {
		t.Fatal(err)
	}
	credentials.ExpiresAt = time.Now().Add(time.Minute)
	data, _ = json.Marshal(credentials)
	if err = s.r2.secrets.Save(r2ConnectionKey(connection), data); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.deleteStatus = http.StatusServiceUnavailable
	control.mu.Unlock()
	renewed, err := s.r2ConfigForUse(t.Context(), config)
	if err != nil || renewed.AccessKeyID == connection.TokenID {
		t.Fatal("failed old-token cleanup prevented using newly committed credentials:", err)
	}
	if len(s.r2.state.RetiredTokens) != 1 || s.r2.state.RetiredTokens[0].TokenID != connection.TokenID || len(backend.items) != 3 {
		t.Fatal("rotation forgot the old credential after its remote deletion failed")
	}
	control.mu.Lock()
	control.deleteStatus = 0
	control.mu.Unlock()
	if _, err = s.DisconnectR2OAuth(t.Context()); err != nil {
		t.Fatal("disconnect could not retry old-token cleanup:", err)
	}
	if len(s.r2.state.RetiredTokens) != 0 || len(s.r2.state.Connections) != 0 || len(backend.items) != 0 {
		t.Fatal("disconnect left retired credentials behind")
	}
}

func TestR2OAuthDisconnectMetadataFailureDoesNotReuseRevokedAuthorization(t *testing.T) {
	s, control, backend, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	failure := errors.New("synthetic post-revocation metadata failure")
	s.r2.saveMetadata = func(string, r2OAuthState) error { return failure }
	if _, err := s.DisconnectR2OAuth(t.Context()); !errors.Is(err, failure) {
		t.Fatal("post-revocation persistence failure was not reported")
	}
	// Even if the saved access token expires before retry, remote revocation
	// already succeeded. Finishing local cleanup must not refresh it again.
	data, err := s.r2.secrets.Load(s.r2.oauthKey())
	if err != nil {
		t.Fatal(err)
	}
	var tokens cloudflare.TokenSet
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	tokens.ExpiresAt = time.Now().Add(-time.Minute)
	data, _ = json.Marshal(tokens)
	if err = s.r2.secrets.Save(s.r2.oauthKey(), data); err != nil {
		t.Fatal(err)
	}
	s.r2.saveMetadata = nil
	if _, err = s.DisconnectR2OAuth(t.Context()); err != nil {
		t.Fatal("local cleanup retried an already revoked OAuth authorization:", err)
	}
	control.mu.Lock()
	exchanges, revokes := control.exchanges, control.revokes
	control.mu.Unlock()
	if exchanges != 1 || revokes != 1 || len(backend.items) != 0 || s.GetR2OAuthStatus().Connected {
		t.Fatal("disconnect reused a revoked token or left local authorization active")
	}
}

func setR2CredentialExpiry(t *testing.T, s *Service, expiresAt time.Time) cloudflare.Credentials {
	t.Helper()
	s.r2.mu.Lock()
	defer s.r2.mu.Unlock()
	key := r2ConnectionKey(s.r2.state.Connections[0])
	data, err := s.r2.secrets.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	var credentials cloudflare.Credentials
	if err = json.Unmarshal(data, &credentials); err != nil {
		t.Fatal(err)
	}
	credentials.ExpiresAt = expiresAt
	data, _ = json.Marshal(credentials)
	if err = s.r2.secrets.Save(key, data); err != nil {
		t.Fatal(err)
	}
	return credentials
}

func TestR2OAuthRefreshableGrantRenewsCredentialsEarly(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	authorizeR2Fixture(t, s, urls)
	if !s.r2.state.Renewable {
		t.Fatal("a grant with a refresh token was not recorded as renewable")
	}
	config, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	config.Passphrase = "isolated early renewal phrase"
	if _, err = s.SaveCloudSyncConfig(config); err != nil {
		t.Fatal(err)
	}
	if _, err = s.r2ConfigForUse(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	mints := control.mints
	control.mu.Unlock()
	if mints != 1 {
		t.Fatal("fresh credentials were renewed")
	}

	// Inside the renewal window, an expired access token is refreshed and the
	// bucket credentials are replaced without a browser.
	previous := setR2CredentialExpiry(t, s, time.Now().Add(3*24*time.Hour))
	key := s.r2.oauthKey()
	data, err := s.r2.secrets.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	var tokens cloudflare.TokenSet
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	tokens.ExpiresAt = time.Now().Add(-time.Minute)
	data, _ = json.Marshal(tokens)
	if err = s.r2.secrets.Save(key, data); err != nil {
		t.Fatal(err)
	}
	renewed, err := s.r2ConfigForUse(t.Context(), config)
	if err != nil {
		t.Fatal("silent renewal failed:", err)
	}
	control.mu.Lock()
	exchanges, mints, deletes := control.exchanges, control.mints, control.deletes
	control.mu.Unlock()
	if exchanges != 2 || mints != 2 || deletes != 1 || renewed.AccessKeyID == previous.AccessKeyID || s.r2.state.Connections[0].TokenID == previous.TokenID {
		t.Fatalf("silent renewal did not refresh, rotate and retire: exchanges=%d mints=%d deletes=%d", exchanges, mints, deletes)
	}

	// A failed early renewal keeps the still-valid credentials and backs off.
	current := setR2CredentialExpiry(t, s, time.Now().Add(3*24*time.Hour))
	if err = s.r2.secrets.Save(key, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		used, err := s.r2ConfigForUse(t.Context(), config)
		if err != nil || used.AccessKeyID != current.AccessKeyID {
			t.Fatal("a failed early renewal interrupted sync with valid credentials:", err)
		}
	}
	if s.r2.renewRetryAt.IsZero() {
		t.Fatal("a failed early renewal did not back off")
	}
	setR2CredentialExpiry(t, s, time.Now().Add(time.Minute))
	if _, err = s.r2ConfigForUse(t.Context(), config); err == nil {
		t.Fatal("expiring credentials were used after renewal failed")
	}
}

func TestR2OAuthCodeOnlyGrantRenewsOnlyNearExpiry(t *testing.T) {
	s, control, _, urls := r2TestService(t)
	control.mu.Lock()
	control.omitRefresh = true
	control.mu.Unlock()
	authorizeR2Fixture(t, s, urls)
	if s.r2.state.Renewable {
		t.Fatal("a code-only grant was recorded as renewable")
	}
	config, err := s.ConfigureR2Bucket(t.Context(), r2TestAccount, r2TestBucket, "default")
	if err != nil {
		t.Fatal(err)
	}
	config.Passphrase = "isolated code-only renewal phrase"
	if _, err = s.SaveCloudSyncConfig(config); err != nil {
		t.Fatal(err)
	}
	setR2CredentialExpiry(t, s, time.Now().Add(3*24*time.Hour))
	if _, err = s.r2ConfigForUse(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	mints := control.mints
	control.mu.Unlock()
	if mints != 1 {
		t.Fatal("a code-only grant renewed credentials early")
	}
}
