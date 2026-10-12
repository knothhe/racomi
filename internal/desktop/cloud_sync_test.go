package desktop

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	skillstore "racomi/internal/skills"
)

func cloudFixtureConfig() CloudSyncConfig {
	return CloudSyncConfig{Endpoint: "https://s3.us-east-1.amazonaws.com", Region: "us-east-1", Bucket: "racomi-sync-test", Prefix: "personal",
		AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-access-key", SessionToken: "test-session-token",
		Passphrase: "test encryption passphrase", PathStyle: true, AutoSync: false, IntervalMinutes: 5}
}

func TestCloudSyncConfigurationStaysDeviceLocalAndRedacted(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if status := s.GetCloudSyncStatus(); status.Configured || status.Config.AutoSync || len(status.Conflicts) != 0 {
		t.Fatalf("unexpected default status: %+v", status)
	}
	config := cloudFixtureConfig()
	state, err := s.SaveCloudSyncConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	status := state.CloudSync
	if !status.Configured || !status.Config.HasSecretAccessKey || !status.Config.HasSessionToken || !status.Config.HasPassphrase {
		t.Fatal("saved credential presence was not reported")
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{config.SecretAccessKey, config.SessionToken, config.Passphrase} {
		if strings.Contains(string(data), secret) {
			t.Fatal("credentials were exposed in app state")
		}
	}
	info, err := os.Stat(filepath.Join(root, "cloud-sync-config.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credentials must have 0600 permissions: %v %v", info, err)
	}
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.cloud.config.Passphrase != config.Passphrase || !reopened.GetCloudSyncStatus().Configured {
		t.Fatal("saved credentials were not loaded")
	}
	// Blank password inputs preserve the saved credential without returning it to the UI.
	public := reopened.GetCloudSyncStatus().Config
	public.IntervalMinutes = 10
	if _, err = reopened.SaveCloudSyncConfig(public); err != nil {
		t.Fatal(err)
	}
	if reopened.cloud.config.SecretAccessKey != config.SecretAccessKey || reopened.cloud.config.SessionToken != config.SessionToken || reopened.cloud.config.Passphrase != config.Passphrase {
		t.Fatal("an ordinary settings edit lost credentials")
	}
	index, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{config.SecretAccessKey, config.SessionToken, config.Passphrase} {
		if strings.Contains(string(index), secret) {
			t.Fatal("device credentials entered the portable skill state")
		}
	}
}

func TestCloudSyncRetargetDoesNotReuseStorageCredentials(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveCloudSyncConfig(cloudFixtureConfig()); err != nil {
		t.Fatal(err)
	}
	public := s.GetCloudSyncStatus().Config
	public.Endpoint = "https://other.example.com"
	if _, err = s.SaveCloudSyncConfig(public); err == nil {
		t.Fatal("changing endpoints silently reused a secret")
	}
	if s.cloud.config.Endpoint != cloudFixtureConfig().Endpoint {
		t.Fatal("a rejected save modified the configuration")
	}
	public = s.GetCloudSyncStatus().Config
	public.AccessKeyID = "replacement-access-key"
	public.SecretAccessKey = "replacement-secret-access-key"
	if _, err = s.SaveCloudSyncConfig(public); err != nil {
		t.Fatal(err)
	}
	if s.cloud.config.SessionToken != "" || s.cloud.config.Passphrase != cloudFixtureConfig().Passphrase {
		t.Fatal("a different account must not reuse its old session token; encryption key must remain")
	}
}

func TestCloudSyncSessionTokenCanBeCleared(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	config := cloudFixtureConfig()
	if _, err = s.SaveCloudSyncConfig(config); err != nil {
		t.Fatal(err)
	}
	input := s.GetCloudSyncStatus().Config
	input.ClearSessionToken = true
	input.SessionToken = "ignored-replacement-token"
	state, err := s.SaveCloudSyncConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	if state.CloudSync.Config.HasSessionToken || state.CloudSync.Config.ClearSessionToken || s.cloud.config.SessionToken != "" || s.cloud.config.ClearSessionToken {
		t.Fatal("explicit clearing did not remove the token and consume its request flag")
	}
	if s.cloud.config.SecretAccessKey != config.SecretAccessKey || s.cloud.config.Passphrase != config.Passphrase {
		t.Fatal("clearing the optional token changed another credential")
	}
	saved, err := os.ReadFile(filepath.Join(root, "cloud-sync-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "clearSessionToken") || strings.Contains(string(saved), config.SessionToken) {
		t.Fatal("saved configuration retained the cleared token or its transient request flag")
	}
	public, err := json.Marshal(state.CloudSync.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "clearSessionToken") {
		t.Fatal("public configuration returned the transient request flag")
	}
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.cloud.config.SessionToken != "" || reopened.GetCloudSyncStatus().Config.HasSessionToken {
		t.Fatal("the cleared token reappeared after reopening the application")
	}
}

func TestCloudSyncConnectionTokenClearDoesNotChangeSavedConfiguration(t *testing.T) {
	for _, clear := range []bool{false, true} {
		name := "preserve"
		if clear {
			name = "clear for test"
		}
		t.Run(name, func(t *testing.T) {
			observed := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed <- r.Header.Get("X-Amz-Security-Token")
				// Stop at the initial read, before the connection probe writes anything.
				w.WriteHeader(http.StatusForbidden)
			}))
			defer server.Close()
			root := t.TempDir()
			s, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			config := cloudFixtureConfig()
			config.Endpoint = server.URL
			if _, err = s.SaveCloudSyncConfig(config); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(root, "cloud-sync-config.json"))
			if err != nil {
				t.Fatal(err)
			}
			input := s.GetCloudSyncStatus().Config
			input.ClearSessionToken = clear
			if err = s.TestCloudSyncConnection(t.Context(), input); err == nil {
				t.Fatal("the forbidden test response was accepted")
			}
			wantToken := config.SessionToken
			if clear {
				wantToken = ""
			}
			select {
			case token := <-observed:
				if token != wantToken {
					t.Fatal("the request did not honor the session token clearing choice")
				}
			default:
				t.Fatal("the connection test did not reach the local server")
			}
			after, err := os.ReadFile(filepath.Join(root, "cloud-sync-config.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || s.cloud.config.SessionToken != config.SessionToken || s.cloud.config.ClearSessionToken || !s.GetCloudSyncStatus().Config.HasSessionToken {
				t.Fatal("testing a draft configuration changed the saved session token")
			}
		})
	}
}

func TestDamagedCloudConfigurationDoesNotBlockLocalWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cloud-sync-config.json"), []byte(`{"version":`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatalf("optional cloud configuration blocked local startup: %v", err)
	}
	state, err := s.GetState()
	if err != nil || state.CloudSync.Configured || state.CloudSync.Config.AutoSync || state.CloudSync.LastError == "" {
		t.Fatalf("damaged configuration must disable sync and report an error: %+v %v", state.CloudSync, err)
	}
	if _, err = s.SavePreset(skillstore.Preset{Name: "Local workspace remains usable"}); err != nil {
		t.Fatalf("local editing was blocked: %v", err)
	}
}

func TestGetStateDoesNotAcknowledgeSyncWithoutItsData(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	type readResult struct {
		state AppState
		err   error
	}
	done := make(chan readResult, 1)
	// A sync is publishing its completion status while a UI state read begins.
	// Hold the mutation lock too: reads must remain available during cloud I/O.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cloud.mu.Lock()
	statusLocked := true
	defer func() {
		if statusLocked {
			s.cloud.mu.Unlock()
		}
	}()
	started := make(chan struct{})
	go func() {
		close(started)
		state, err := s.GetState()
		done <- readResult{state, err}
	}()
	<-started
	// Let the read reach the held status lock. Previously it had already read
	// the old library by this point, then accepted the new completion marker.
	select {
	case <-done:
		t.Fatal("state read did not wait for the completion status")
	case <-time.After(100 * time.Millisecond):
	}
	preset, err := s.store.SavePreset(skillstore.Preset{Name: "Arrived from another device"})
	if err != nil {
		t.Fatal(err)
	}
	s.cloud.lastSyncAt = "2026-10-12T00:00:00Z"
	s.cloud.mu.Unlock()
	statusLocked = false
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.state.CloudSync.LastSyncAt != "2026-10-12T00:00:00Z" || len(result.state.State.Presets) != 1 || result.state.State.Presets[0].ID != preset.ID {
			t.Fatalf("completion marker acknowledged without its synchronized data: %+v", result.state)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("state read blocked on the mutation lock")
	}
}

func TestCloudSyncValidationAndConflictReview(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, interval := range []int{-1, 1441} {
		input := cloudFixtureConfig()
		input.IntervalMinutes = interval
		if _, err = s.SaveCloudSyncConfig(input); err == nil {
			t.Fatalf("invalid interval accepted: %d", interval)
		}
	}
	input := cloudFixtureConfig()
	input.Passphrase = "short"
	if _, err = s.SaveCloudSyncConfig(input); err == nil {
		t.Fatal("a short encryption passphrase was accepted")
	}
	if _, err = s.ResolveCloudSync(t.Context(), map[string]string{"preset:unknown": "remote"}); err == nil {
		t.Fatal("conflict resolution without a reviewed version was accepted")
	}
	redacted := redactCloudError(errors.New(strings.Join([]string{input.SecretAccessKey, input.SessionToken, input.Passphrase}, " ")), input).Error()
	for _, secret := range []string{input.SecretAccessKey, input.SessionToken, input.Passphrase} {
		if strings.Contains(redacted, secret) {
			t.Fatal("error exposed a secret")
		}
	}
}

func TestSettingsPatchPreservesPreferencesArrivingFromCloud(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.store.State()
	if err != nil {
		t.Fatal(err)
	}
	shared := skillstore.ExportShared(state)
	shared.Settings.Theme = "dark"
	shared.Settings.NavLayout = "top"
	shared.Settings.DefaultProjectDirectories = []string{"claude"}
	if err = s.store.ApplyShared(shared); err != nil {
		t.Fatal(err)
	}
	// The frontend made this language edit before receiving the cloud state.
	// Only its language patch is applied to the latest state on the service.
	next, err := s.SaveSettingsPatch(map[string]json.RawMessage{"locale": json.RawMessage(`"en"`)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if next.State.Settings.Theme != "dark" || next.State.Settings.Locale != "en" || len(next.State.Settings.DefaultProjectDirectories) != 1 || next.State.Settings.DefaultProjectDirectories[0] != "claude" {
		t.Fatal("a queued preference edit overwrote unrelated cloud settings")
	}
	if _, err = s.SaveSettingsPatch(map[string]json.RawMessage{"unknown": json.RawMessage(`true`)}, false); err == nil {
		t.Fatal("unknown settings fields accepted")
	}
	if _, err = s.SaveSettingsPatch(map[string]json.RawMessage{"locale": json.RawMessage(`null`)}, false); err == nil {
		t.Fatal("null preference patch accepted")
	}
	next, err = s.SaveSettingsPatch(map[string]json.RawMessage{"theme": json.RawMessage(`"light"`), "locale": json.RawMessage(`"system"`), "navLayout": json.RawMessage(`"side"`)}, true)
	if err != nil {
		t.Fatal(err)
	}
	if next.State.Settings.Theme != "dark" || next.State.Settings.Locale != "en" || next.State.Settings.NavLayout != "top" {
		t.Fatal("startup migration replaced synchronized preferences instead of filling missing values")
	}
	fresh, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	next, err = fresh.SaveSettingsPatch(map[string]json.RawMessage{"theme": json.RawMessage(`"light"`), "locale": json.RawMessage(`"en"`), "navLayout": json.RawMessage(`"side"`)}, true)
	if err != nil || next.State.Settings.Theme != "light" || next.State.Settings.Locale != "en" || next.State.Settings.NavLayout != "side" {
		t.Fatalf("initial migration did not fill missing local preferences: %v", err)
	}
}
