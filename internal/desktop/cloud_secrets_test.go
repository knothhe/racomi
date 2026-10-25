package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func cloudSecretsTestStore(backend *memoryR2SecretBackend) *r2SystemSecretStore {
	return &r2SystemSecretStore{service: r2SecretService("/isolated/cloud-secrets"), backend: backend}
}

func readSavedCloudSync(t *testing.T, path string) (savedCloudSync, string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved savedCloudSync
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	return saved, string(data)
}

func assertNoPlaintextSecrets(t *testing.T, contents string, config CloudSyncConfig) {
	t.Helper()
	for _, secret := range []string{config.SecretAccessKey, config.SessionToken, config.Passphrase} {
		if secret != "" && strings.Contains(contents, secret) {
			t.Fatalf("configuration file contains a plaintext secret: %s", contents)
		}
	}
}

func TestManualCloudSecretsLiveInCredentialStore(t *testing.T) {
	root := t.TempDir()
	backend := &memoryR2SecretBackend{items: map[string][]byte{}}
	c, err := loadCloudSync(root, cloudSecretsTestStore(backend))
	if err != nil {
		t.Fatal(err)
	}
	config := cloudFixtureConfig()
	c.mu.Lock()
	err = c.persistLocked(config, "")
	c.config, c.manualConfig = config, config
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	saved, contents := readSavedCloudSync(t, c.path)
	if saved.ManualSecrets != cloudSecretsSystem || saved.R2Secrets != "" {
		t.Fatalf("secret locations manual=%q r2=%q", saved.ManualSecrets, saved.R2Secrets)
	}
	assertNoPlaintextSecrets(t, contents, config)
	// Status saves after every sync must not rewrite the credential store.
	writes := backend.calls
	c.mu.Lock()
	err = c.persistLocked(config, "2026-10-25T00:00:00Z")
	c.mu.Unlock()
	if err != nil || backend.calls != writes {
		t.Fatalf("unchanged secrets were stored again: calls %d -> %d, err %v", writes, backend.calls, err)
	}
	reopened, err := loadCloudSync(root, cloudSecretsTestStore(backend))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.config.SecretAccessKey != config.SecretAccessKey || reopened.config.SessionToken != config.SessionToken || reopened.config.Passphrase != config.Passphrase || reopened.manualConfig.Passphrase != config.Passphrase {
		t.Fatal("stored secrets were not restored")
	}
	if !cloudConfigReady(reopened.config) || reopened.lastError != "" {
		t.Fatalf("restored configuration is not ready: %q", reopened.lastError)
	}
}

func TestManualCloudSecretsFallBackToFileWithoutCredentialStore(t *testing.T) {
	root := t.TempDir()
	backend := &memoryR2SecretBackend{items: map[string][]byte{}, err: errR2SecretStoreMissing}
	c, err := loadCloudSync(root, cloudSecretsTestStore(backend))
	if err != nil {
		t.Fatal(err)
	}
	config := cloudFixtureConfig()
	c.mu.Lock()
	err = c.persistLocked(config, "")
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := readSavedCloudSync(t, c.path)
	if saved.ManualSecrets != "" || saved.ManualConfig.SecretAccessKey != config.SecretAccessKey || saved.ManualConfig.Passphrase != config.Passphrase {
		t.Fatal("manual S3 did not fall back to the configuration file")
	}
	if info, err := os.Stat(c.path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("fallback file permissions: %v %v", info.Mode(), err)
	}
	// R2 cannot run without the credential store, so its passphrase never
	// falls back to plaintext.
	r2 := defaultCloudSyncConfig()
	r2.Provider, r2.Endpoint, r2.Passphrase = "r2-oauth", "https://account.r2.cloudflarestorage.com", "r2 test encryption passphrase"
	if _, _, _, err = c.r2Secrets.prepare(c.secrets, r2); !errors.Is(err, errR2SecretStoreMissing) {
		t.Fatalf("R2 passphrase without a credential store: %v", err)
	}
	// Once the store works again, the next save moves the secrets out of the file.
	backend.err = nil
	c.mu.Lock()
	err = c.persistLocked(config, "")
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	saved, contents := readSavedCloudSync(t, c.path)
	if saved.ManualSecrets != cloudSecretsSystem {
		t.Fatal("secrets stayed in the file after the credential store became available")
	}
	assertNoPlaintextSecrets(t, contents, config)
}

func TestUnreadableCloudSecretsAreKeptAndRetried(t *testing.T) {
	root := t.TempDir()
	backend := &memoryR2SecretBackend{items: map[string][]byte{}}
	c, err := loadCloudSync(root, cloudSecretsTestStore(backend))
	if err != nil {
		t.Fatal(err)
	}
	config := cloudFixtureConfig()
	c.mu.Lock()
	err = c.persistLocked(config, "")
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	backend.err = errors.New("keyring is temporarily unavailable")
	reopened, err := loadCloudSync(root, cloudSecretsTestStore(backend))
	if err != nil {
		t.Fatal(err)
	}
	if cloudConfigReady(reopened.config) || reopened.lastError != errCloudSecretsUnreadable.Error() {
		t.Fatalf("unreadable secrets were not reported: %q", reopened.lastError)
	}
	// Saving status meanwhile must keep the stored secrets referenced.
	reopened.mu.Lock()
	err = reopened.persistLocked(reopened.config, "")
	reopened.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if saved, _ := readSavedCloudSync(t, reopened.path); saved.ManualSecrets != cloudSecretsSystem {
		t.Fatal("unreadable secrets were dropped from the configuration")
	}
	backend.err = nil
	reopened.mu.Lock()
	err = reopened.activeSecretsLocked(false)
	ready, lastError := cloudConfigReady(reopened.config), reopened.lastError
	reopened.mu.Unlock()
	if err != nil || !ready || lastError != "" || reopened.config.Passphrase != config.Passphrase {
		t.Fatalf("retry did not restore secrets: err=%v ready=%v lastError=%q", err, ready, lastError)
	}
}

func TestLegacyR2PassphraseMovesToCredentialStore(t *testing.T) {
	root := t.TempDir()
	r2 := defaultCloudSyncConfig()
	r2.Provider, r2.R2ConnectionID, r2.AccountID, r2.Jurisdiction = "r2-oauth", "connection", "account", "default"
	r2.Endpoint, r2.Bucket, r2.Passphrase, r2.HasSecretAccessKey = "https://account.r2.cloudflarestorage.com", "bucket", "legacy r2 passphrase", true
	manual := defaultCloudSyncConfig()
	legacy, err := json.Marshal(savedCloudSync{Version: 2, Config: r2, ManualConfig: &manual, R2Config: &r2})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "cloud-sync-config.json")
	if err = os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	backend := &memoryR2SecretBackend{items: map[string][]byte{}}
	c, err := loadCloudSync(root, cloudSecretsTestStore(backend))
	if err != nil {
		t.Fatal(err)
	}
	saved, contents := readSavedCloudSync(t, path)
	if saved.R2Secrets != cloudSecretsSystem || saved.ManualSecrets != "" {
		t.Fatalf("legacy secrets were not migrated: r2=%q manual=%q", saved.R2Secrets, saved.ManualSecrets)
	}
	assertNoPlaintextSecrets(t, contents, r2)
	if c.config.Passphrase != r2.Passphrase || !cloudConfigReady(c.config) {
		t.Fatal("migration changed the active R2 profile")
	}
	// Disconnecting Cloudflare resets the profile and removes its stored item.
	c.mu.Lock()
	reset := defaultCloudSyncConfig()
	reset.Provider = "r2-oauth"
	c.r2Config = reset
	err = c.persistLocked(reset, "")
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.secrets.Load(cloudSecretsR2Key); !errors.Is(err, errR2SecretNotFound) {
		t.Fatalf("disconnected R2 passphrase is still stored: %v", err)
	}
	if saved, _ := readSavedCloudSync(t, path); saved.R2Secrets != "" {
		t.Fatal("configuration still references the removed R2 secrets")
	}
}
