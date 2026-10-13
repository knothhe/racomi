package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"racomi/internal/cloudsync"
	skillstore "racomi/internal/skills"
)

// Credentials are device-local. Public status always replaces them with presence flags.
type CloudSyncConfig struct {
	Endpoint           string `json:"endpoint"`
	Region             string `json:"region"`
	Bucket             string `json:"bucket"`
	Prefix             string `json:"prefix"`
	AccessKeyID        string `json:"accessKeyId"`
	SecretAccessKey    string `json:"secretAccessKey"`
	SessionToken       string `json:"sessionToken"`
	ClearSessionToken  bool   `json:"clearSessionToken,omitempty"` // Request-only; never persisted or returned.
	Passphrase         string `json:"passphrase"`
	PathStyle          bool   `json:"pathStyle"`
	AutoSync           bool   `json:"autoSync"`
	IntervalMinutes    int    `json:"intervalMinutes"`
	HasSecretAccessKey bool   `json:"hasSecretAccessKey"`
	HasSessionToken    bool   `json:"hasSessionToken"`
	HasPassphrase      bool   `json:"hasPassphrase"`
}

type CloudSyncStatus struct {
	Config     CloudSyncConfig           `json:"config"`
	Configured bool                      `json:"configured"`
	Syncing    bool                      `json:"syncing"`
	LastSyncAt string                    `json:"lastSyncAt"`
	LastError  string                    `json:"lastError"`
	Conflicts  []skillstore.SyncConflict `json:"conflicts"`
}

type cloudSyncRuntime struct {
	mu              sync.Mutex
	runMu           sync.Mutex
	startOnce       sync.Once
	path            string
	config          CloudSyncConfig
	lastSyncAt      string
	lastError       string
	syncing         bool
	conflicts       []skillstore.SyncConflict
	resolutionToken string
	lastAttempt     time.Time
}

type savedCloudSync struct {
	Version    int             `json:"version"`
	Config     CloudSyncConfig `json:"config"`
	LastSyncAt string          `json:"lastSyncAt,omitempty"`
}

func defaultCloudSyncConfig() CloudSyncConfig {
	return CloudSyncConfig{Region: "auto", Prefix: "racomi", PathStyle: true, IntervalMinutes: 5}
}

func newCloudSyncRuntime(root string) *cloudSyncRuntime {
	return &cloudSyncRuntime{path: filepath.Join(root, "cloud-sync-config.json"), config: defaultCloudSyncConfig(), conflicts: []skillstore.SyncConflict{}}
}

func loadCloudSync(root string) (*cloudSyncRuntime, error) {
	c := newCloudSyncRuntime(root)
	info, err := os.Lstat(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read cloud sync configuration: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, errors.New("cloud sync configuration must be a regular file smaller than 64 KiB")
	}
	f, err := os.Open(c.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var saved savedCloudSync
	if err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&saved); err != nil || saved.Version != 1 {
		return nil, errors.New("invalid cloud sync configuration")
	}
	c.config = saved.Config
	c.config.HasSecretAccessKey, c.config.HasSessionToken, c.config.HasPassphrase = false, false, false
	c.config.ClearSessionToken = false
	if c.config.IntervalMinutes < 1 {
		c.config.IntervalMinutes = 5
	}
	c.lastSyncAt = saved.LastSyncAt
	if err = os.Chmod(c.path, 0600); err != nil {
		return nil, fmt.Errorf("cannot protect cloud sync credentials: %w", err)
	}
	return c, nil
}

// persistLocked uses an atomic rename so interrupted saves cannot lose credentials.
func (c *cloudSyncRuntime) persistLocked(config CloudSyncConfig, lastSyncAt string) error {
	config.HasSecretAccessKey, config.HasSessionToken, config.HasPassphrase = false, false, false
	config.ClearSessionToken = false
	contents, err := json.MarshalIndent(savedCloudSync{Version: 1, Config: config, LastSyncAt: lastSyncAt}, "", "  ")
	if err != nil {
		return err
	}
	if len(contents) > 64<<10 {
		return errors.New("cloud sync configuration exceeds 64 KiB")
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), ".cloud-sync-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(contents); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), c.path)
}

func (config CloudSyncConfig) remote() cloudsync.Config {
	return cloudsync.Config{Endpoint: config.Endpoint, Region: config.Region, Bucket: config.Bucket, Prefix: config.Prefix,
		AccessKeyID: config.AccessKeyID, SecretAccessKey: config.SecretAccessKey, SessionToken: config.SessionToken,
		Passphrase: config.Passphrase, PathStyle: config.PathStyle}
}

func (config CloudSyncConfig) public() CloudSyncConfig {
	config.HasSecretAccessKey = config.SecretAccessKey != ""
	config.HasSessionToken = config.SessionToken != ""
	config.HasPassphrase = config.Passphrase != ""
	config.SecretAccessKey, config.SessionToken, config.Passphrase = "", "", ""
	config.ClearSessionToken = false
	return config
}

func mergedCloudConfig(input, previous CloudSyncConfig) (CloudSyncConfig, error) {
	input.Endpoint = strings.TrimRight(strings.TrimSpace(input.Endpoint), "/")
	input.Region = strings.TrimSpace(input.Region)
	input.Bucket = strings.TrimSpace(input.Bucket)
	input.Prefix = strings.Trim(strings.TrimSpace(input.Prefix), "/")
	input.AccessKeyID = strings.TrimSpace(input.AccessKeyID)
	if input.Region == "" {
		input.Region = "auto"
	}
	if input.Prefix == "" {
		input.Prefix = "racomi"
	}
	if input.IntervalMinutes == 0 {
		input.IntervalMinutes = 5
	}
	if input.IntervalMinutes < 1 || input.IntervalMinutes > 1440 {
		return CloudSyncConfig{}, errors.New("cloud sync interval must be between 1 and 1440 minutes")
	}
	identityChanged := input.Endpoint != previous.Endpoint || input.AccessKeyID != previous.AccessKeyID
	if input.SecretAccessKey == "" && !identityChanged {
		input.SecretAccessKey = previous.SecretAccessKey
	}
	if input.ClearSessionToken {
		input.SessionToken = ""
	} else if input.SessionToken == "" && !identityChanged {
		input.SessionToken = previous.SessionToken
	}
	if input.Passphrase == "" {
		input.Passphrase = previous.Passphrase
	}
	input.HasSecretAccessKey, input.HasSessionToken, input.HasPassphrase = false, false, false
	input.ClearSessionToken = false
	if identityChanged && input.SecretAccessKey == "" {
		return CloudSyncConfig{}, errors.New("enter the secret access key again after changing the endpoint or access key ID")
	}
	if err := cloudsync.ValidateConfig(input.remote()); err != nil {
		return CloudSyncConfig{}, err
	}
	return input, nil
}

func (s *Service) GetCloudSyncStatus() CloudSyncStatus {
	if s.cloud == nil {
		return CloudSyncStatus{Config: defaultCloudSyncConfig(), Conflicts: []skillstore.SyncConflict{}}
	}
	c := s.cloud
	c.mu.Lock()
	defer c.mu.Unlock()
	conflicts := append([]skillstore.SyncConflict{}, c.conflicts...)
	return CloudSyncStatus{Config: c.config.public(), Configured: c.config.Endpoint != "" && c.config.Bucket != "" && c.config.SecretAccessKey != "" && c.config.Passphrase != "",
		Syncing: c.syncing, LastSyncAt: c.lastSyncAt, LastError: c.lastError, Conflicts: conflicts}
}

// SaveSettingsPatch rebases explicit UI edits onto the latest local settings.
// A full settings object queued behind a background sync could otherwise undo
// unrelated preferences that just arrived from another device.
func (s *Service) SaveSettingsPatch(patch map[string]json.RawMessage, onlyMissing bool) (AppState, error) {
	return s.mutate(func() error {
		state, err := s.store.State()
		if err != nil {
			return err
		}
		allowed := map[string]bool{"deploymentMode": true, "defaultProjectDirectories": true, "defaultGlobalDirectories": true,
			"customProjectDirectories": true, "theme": true, "locale": true, "navLayout": true, "textSize": true, "interfaceFont": true}
		filtered := make(map[string]json.RawMessage, len(patch))
		preferences := map[string]string{"theme": state.Settings.Theme, "locale": state.Settings.Locale, "navLayout": state.Settings.NavLayout}
		for field, value := range patch {
			_, preference := preferences[field]
			if !allowed[field] || strings.TrimSpace(string(value)) == "null" || (onlyMissing && !preference) {
				return errors.New("invalid settings patch field")
			}
			if !onlyMissing || preferences[field] == "" {
				filtered[field] = value
			}
		}
		data, err := json.Marshal(filtered)
		if err != nil {
			return err
		}
		settings := state.Settings
		if err = json.Unmarshal(data, &settings); err != nil {
			return errors.New("invalid settings patch")
		}
		return s.store.SaveSettings(settings)
	})
}

func (s *Service) SaveCloudSyncConfig(input CloudSyncConfig) (AppState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cloud
	c.mu.Lock()
	config, err := mergedCloudConfig(input, c.config)
	if err != nil {
		c.mu.Unlock()
		return AppState{}, err
	}
	lastSyncAt := c.lastSyncAt
	if config.Endpoint != c.config.Endpoint || config.Bucket != c.config.Bucket || config.Prefix != c.config.Prefix || config.Passphrase != c.config.Passphrase {
		lastSyncAt = ""
	}
	if err = c.persistLocked(config, lastSyncAt); err != nil {
		c.mu.Unlock()
		return AppState{}, err
	}
	c.config, c.lastSyncAt = config, lastSyncAt
	c.lastError, c.resolutionToken = "", ""
	c.conflicts = []skillstore.SyncConflict{}
	c.lastAttempt = time.Time{}
	c.mu.Unlock()
	return s.GetState()
}

func (s *Service) TestCloudSyncConnection(ctx context.Context, input CloudSyncConfig) error {
	s.cloud.mu.Lock()
	config, err := mergedCloudConfig(input, s.cloud.config)
	s.cloud.mu.Unlock()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return redactCloudError(cloudsync.TestConnection(ctx, config.remote()), config)
}

func redactCloudError(err error, config CloudSyncConfig) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, secret := range []string{config.SecretAccessKey, config.SessionToken, config.Passphrase} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return errors.New(message)
}

func (s *Service) SyncCloud(ctx context.Context) (AppState, error) {
	return s.runCloudSync(ctx, nil, "")
}

func (s *Service) ResolveCloudSync(ctx context.Context, resolutions map[string]string) (AppState, error) {
	s.cloud.mu.Lock()
	if s.cloud.resolutionToken == "" || len(resolutions) != len(s.cloud.conflicts) {
		s.cloud.mu.Unlock()
		return AppState{}, errors.New("review all cloud sync conflicts before continuing")
	}
	choices := make(map[string]string, len(resolutions)+1)
	for _, conflict := range s.cloud.conflicts {
		choice := resolutions[conflict.Key]
		if choice != "local" && choice != "remote" {
			s.cloud.mu.Unlock()
			return AppState{}, errors.New("choose the local or remote version for every cloud sync conflict")
		}
		choices[conflict.Key] = choice
	}
	choices["__token"] = s.cloud.resolutionToken
	s.cloud.mu.Unlock()
	return s.runCloudSync(ctx, choices, "")
}

func (s *Service) runCloudSync(ctx context.Context, resolutions map[string]string, restoreID string) (AppState, error) {
	c := s.cloud
	if !c.runMu.TryLock() {
		return AppState{}, errors.New("cloud sync is already running")
	}
	defer c.runMu.Unlock()
	// Always take the mutation lock before reading configuration. A queued sync
	// must not commit to an endpoint replaced by a concurrent configuration save.
	s.mu.Lock()
	defer s.mu.Unlock()
	c.mu.Lock()
	config := c.config
	if err := cloudsync.ValidateConfig(config.remote()); err != nil {
		c.mu.Unlock()
		return AppState{}, err
	}
	c.syncing, c.lastError, c.lastAttempt = true, "", time.Now()
	c.mu.Unlock()
	// Keep application writes serialized until the remote conditional commit and
	// local commit have completed. Reads and status polling remain available.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var result cloudsync.Result
	var err error
	if restoreID != "" {
		result, err = cloudsync.RestoreBackup(ctx, s.store, config.remote(), restoreID)
	} else {
		result, err = cloudsync.Sync(ctx, s.store, config.remote(), resolutions)
	}
	err = redactCloudError(err, config)
	c.mu.Lock()
	c.syncing = false
	if err != nil {
		c.lastError = err.Error()
	} else {
		c.conflicts, c.resolutionToken = result.Conflicts, result.ResolutionToken
		if c.conflicts == nil {
			c.conflicts = []skillstore.SyncConflict{}
		}
		if len(c.conflicts) == 0 {
			c.lastSyncAt = result.LastSyncAt
			if c.lastSyncAt == "" {
				c.lastSyncAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			if persistErr := c.persistLocked(config, c.lastSyncAt); persistErr != nil {
				err = fmt.Errorf("cloud sync completed but its status could not be saved: %w", persistErr)
				c.lastError = err.Error()
			}
		}
	}
	c.mu.Unlock()
	if err != nil {
		return AppState{}, err
	}
	return s.GetState()
}

func (s *Service) ListCloudBackups(ctx context.Context) ([]cloudsync.BackupInfo, error) {
	s.cloud.mu.Lock()
	config := s.cloud.config
	s.cloud.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	backups, err := cloudsync.ListBackups(ctx, config.remote())
	return backups, redactCloudError(err, config)
}

func (s *Service) RestoreCloudBackup(ctx context.Context, id string) (AppState, error) {
	if id == "" {
		return AppState{}, errors.New("choose a cloud backup to restore")
	}
	return s.runCloudSync(ctx, nil, id)
}

func (s *Service) startCloudSync(ctx context.Context) {
	if s.cloud == nil {
		return
	}
	s.cloud.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			// The initial attempt also brings a newly opened device up to date.
			for {
				c := s.cloud
				c.mu.Lock()
				due := c.config.AutoSync && !c.syncing && len(c.conflicts) == 0 &&
					(c.lastAttempt.IsZero() || time.Since(c.lastAttempt) >= time.Duration(c.config.IntervalMinutes)*time.Minute)
				c.mu.Unlock()
				if due {
					_, _ = s.SyncCloud(ctx)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}
