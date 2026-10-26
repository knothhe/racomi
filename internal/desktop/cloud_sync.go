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

// Credentials are device-local and kept in the system credential store when it
// is usable (see cloud_secrets.go). Public status replaces them with presence flags.
type CloudSyncConfig struct {
	Provider           string `json:"provider,omitempty"`
	R2ConnectionID     string `json:"r2ConnectionId,omitempty"`
	AccountID          string `json:"accountId,omitempty"`
	Jurisdiction       string `json:"jurisdiction,omitempty"`
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
	Config       CloudSyncConfig           `json:"config"`
	ManualConfig CloudSyncConfig           `json:"manualConfig"`
	R2Config     CloudSyncConfig           `json:"r2Config"`
	R2OAuth      R2OAuthStatus             `json:"r2OAuth"`
	Configured   bool                      `json:"configured"`
	Syncing      bool                      `json:"syncing"`
	LastSyncAt   string                    `json:"lastSyncAt"`
	LastError    string                    `json:"lastError"`
	Conflicts    []skillstore.SyncConflict `json:"conflicts"`
}

type cloudSyncRuntime struct {
	mu              sync.Mutex
	runMu           sync.Mutex
	startOnce       sync.Once
	path            string
	config          CloudSyncConfig
	manualConfig    CloudSyncConfig
	r2Config        CloudSyncConfig
	lastSyncAt      string
	lastError       string
	syncing         bool
	conflicts       []skillstore.SyncConflict
	resolutionToken string
	lastAttempt     time.Time
	secrets         r2SecretStore
	manualSecrets   cloudSecretSlot
	r2Secrets       cloudSecretSlot
}

type savedCloudSync struct {
	Version      int              `json:"version"`
	Config       CloudSyncConfig  `json:"config"`
	LastSyncAt   string           `json:"lastSyncAt,omitempty"`
	ManualConfig *CloudSyncConfig `json:"manualConfig,omitempty"`
	R2Config     *CloudSyncConfig `json:"r2Config,omitempty"`
	// "system" when the profile's secrets are in the credential store.
	ManualSecrets string `json:"manualSecrets,omitempty"`
	R2Secrets     string `json:"r2Secrets,omitempty"`
}

func defaultCloudSyncConfig() CloudSyncConfig {
	return CloudSyncConfig{Region: "auto", Prefix: "racomi", PathStyle: true, IntervalMinutes: 5}
}

func newCloudSyncRuntime(root string, secrets r2SecretStore) *cloudSyncRuntime {
	manual, r2 := defaultCloudSyncConfig(), defaultCloudSyncConfig()
	r2.Provider = "r2-oauth"
	return &cloudSyncRuntime{path: filepath.Join(root, "cloud-sync-config.json"), config: manual, manualConfig: manual, r2Config: r2, conflicts: []skillstore.SyncConflict{},
		secrets: secrets, manualSecrets: cloudSecretSlot{key: cloudSecretsManualKey}, r2Secrets: cloudSecretSlot{key: cloudSecretsR2Key}}
}

func loadCloudSync(root string, secrets r2SecretStore) (*cloudSyncRuntime, error) {
	c := newCloudSyncRuntime(root, secrets)
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
	var saved savedCloudSync
	err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&saved)
	// Close before migrating: Windows cannot replace a file that is still open.
	f.Close()
	if err != nil || (saved.Version != 1 && saved.Version != 2) {
		return nil, errors.New("invalid cloud sync configuration")
	}
	c.config = saved.Config
	if saved.ManualConfig != nil {
		c.manualConfig = *saved.ManualConfig
	} else if c.config.Provider != "r2-oauth" {
		c.manualConfig = c.config
	}
	if saved.R2Config != nil {
		c.r2Config = *saved.R2Config
	} else if c.config.Provider == "r2-oauth" {
		c.r2Config = c.config
	}
	if c.config.Provider != "r2-oauth" {
		c.config.HasSecretAccessKey = false
	}
	c.config.HasSessionToken, c.config.HasPassphrase = false, false
	c.config.ClearSessionToken = false
	if c.config.IntervalMinutes < 1 {
		c.config.IntervalMinutes = 5
	}
	c.lastSyncAt = saved.LastSyncAt
	if err = os.Chmod(c.path, 0600); err != nil {
		return nil, fmt.Errorf("cannot protect cloud sync credentials: %w", err)
	}
	c.manualSecrets.stored = saved.ManualSecrets == cloudSecretsSystem
	c.r2Secrets.stored = saved.R2Secrets == cloudSecretsSystem
	c.loadSecretsLocked()
	if profileSecrets(c.manualConfig) != (cloudSecrets{}) && !c.manualSecrets.stored || profileSecrets(c.r2Config) != (cloudSecrets{}) && !c.r2Secrets.stored {
		// Move secrets that older versions left in the file. A failure keeps the
		// file as it is; the next successful save retries.
		_ = c.persistLocked(c.config, c.lastSyncAt)
	}
	return c, nil
}

// loadSecretsLocked reads stored profiles whose secrets are not in memory yet.
// The active configuration duplicates one of the profiles.
func (c *cloudSyncRuntime) loadSecretsLocked() error {
	var failure error
	for _, profile := range []struct {
		slot   *cloudSecretSlot
		config *CloudSyncConfig
		r2     bool
	}{{&c.manualSecrets, &c.manualConfig, false}, {&c.r2Secrets, &c.r2Config, true}} {
		if !profile.slot.stored || profile.slot.loadErr == nil && profileSecrets(*profile.config) != (cloudSecrets{}) {
			continue
		}
		*profile.config = profile.slot.load(c.secrets, *profile.config)
		if (c.config.Provider == "r2-oauth") == profile.r2 {
			c.config = withSecrets(c.config, profileSecrets(*profile.config))
		}
		if profile.slot.loadErr != nil && failure == nil {
			failure = errCloudSecretsUnreadable
		}
	}
	if failure != nil {
		c.lastError = failure.Error()
	} else if c.lastError == errCloudSecretsUnreadable.Error() {
		c.lastError = ""
	}
	return failure
}

// activeSecretsLocked retries unreadable stored secrets and reports whether the
// active profile has them. Only a user action reopens a credential store that
// denied access, and Cloudflare keeps requiring Connect Cloudflare for that.
func (c *cloudSyncRuntime) activeSecretsLocked(explicit bool) error {
	slot := &c.manualSecrets
	if c.config.Provider == "r2-oauth" {
		slot = &c.r2Secrets
	}
	if slot.loadErr == nil {
		return nil
	}
	if store, ok := c.secrets.(interface{ retryAccess() }); ok && explicit && slot == &c.manualSecrets && errors.Is(slot.loadErr, errR2SecretAccessDenied) {
		store.retryAccess()
	}
	c.loadSecretsLocked()
	if slot.loadErr != nil {
		return errCloudSecretsUnreadable
	}
	return nil
}

// persistLocked uses an atomic rename so interrupted saves cannot lose credentials.
func (c *cloudSyncRuntime) persistLocked(config CloudSyncConfig, lastSyncAt string) error {
	if config.Provider != "r2-oauth" {
		config.HasSecretAccessKey = false
	}
	config.HasSessionToken, config.HasPassphrase = false, false
	config.ClearSessionToken = false
	manual, r2 := c.manualConfig, c.r2Config
	if config.Provider == "r2-oauth" {
		r2 = config
	} else {
		manual = config
	}
	manual, manualStored, manualCommit, err := c.manualSecrets.prepare(c.secrets, manual)
	if err != nil {
		return err
	}
	r2, r2Stored, r2Commit, err := c.r2Secrets.prepare(c.secrets, r2)
	if err != nil {
		return err
	}
	if config.Provider == "r2-oauth" {
		config = r2
	} else {
		config = manual
	}
	saved := savedCloudSync{Version: 2, LastSyncAt: lastSyncAt}
	if manualStored {
		saved.ManualSecrets = cloudSecretsSystem
	}
	if r2Stored {
		saved.R2Secrets = cloudSecretsSystem
	}
	config, r2 = r2DiskConfig(config), r2DiskConfig(r2)
	saved.Config, saved.ManualConfig, saved.R2Config = config, &manual, &r2
	contents, err := json.MarshalIndent(saved, "", "  ")
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
	if err = os.Rename(f.Name(), c.path); err != nil {
		return err
	}
	manualCommit()
	r2Commit()
	return nil
}

func (config CloudSyncConfig) remote() cloudsync.Config {
	return cloudsync.Config{Endpoint: config.Endpoint, Region: config.Region, Bucket: config.Bucket, Prefix: config.Prefix,
		AccessKeyID: config.AccessKeyID, SecretAccessKey: config.SecretAccessKey, SessionToken: config.SessionToken,
		Passphrase: config.Passphrase, PathStyle: config.PathStyle}
}

func (config CloudSyncConfig) public() CloudSyncConfig {
	config.HasSecretAccessKey = config.SecretAccessKey != "" || config.Provider == "r2-oauth" && config.R2ConnectionID != "" && config.HasSecretAccessKey
	config.HasSessionToken = config.SessionToken != ""
	config.HasPassphrase = config.Passphrase != ""
	config.SecretAccessKey, config.SessionToken, config.Passphrase = "", "", ""
	if config.Provider == "r2-oauth" {
		config.AccessKeyID = ""
	}
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
	conflicts := append([]skillstore.SyncConflict{}, c.conflicts...)
	status := CloudSyncStatus{Config: c.config.public(), ManualConfig: c.manualConfig.public(), R2Config: c.r2Config.public(), Configured: cloudConfigReady(c.config),
		Syncing: c.syncing, LastSyncAt: c.lastSyncAt, LastError: c.lastError, Conflicts: conflicts}
	c.mu.Unlock()
	status.R2OAuth = s.GetR2OAuthStatus()
	return status
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
			"customProjectDirectories": true, "theme": true, "themeStyle": true, "locale": true, "navLayout": true, "textSize": true, "interfaceFont": true,
			"interfaceFontStyle": true, "snapshotRetentionDays": true, "snapshotMaxCount": true}
		filtered := make(map[string]json.RawMessage, len(patch))
		preferences := map[string]string{"theme": state.Settings.Theme, "themeStyle": state.Settings.ThemeStyle, "locale": state.Settings.Locale, "navLayout": state.Settings.NavLayout}
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
		if settings.SnapshotRetentionDays < 1 || settings.SnapshotMaxCount < 1 {
			return errors.New("snapshot retention must be 1 to 3650 days and 1 to 10000 snapshots")
		}
		return s.store.SaveSettings(settings)
	})
}

func (s *Service) SaveCloudSyncConfig(input CloudSyncConfig) (AppState, error) {
	var previousConnection string
	state, err := s.saveCloudSyncConfig(input, &previousConnection)
	if err != nil {
		return state, err
	}
	if input.Provider == "r2-oauth" && previousConnection != "" && previousConnection != input.R2ConnectionID {
		// The commit has released the local mutation lock. Remote cleanup cannot
		// make unrelated library edits wait, and saved profiles remain protected.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = s.DiscardR2Connection(ctx, previousConnection)
		cancel()
		if err != nil && s.r2 != nil {
			s.r2.mu.Lock()
			s.r2.lastError = "The new bucket was saved, but its previous R2 token could not be removed. Disconnect Cloudflare to retry cleanup."
			s.r2.mu.Unlock()
		}
		return s.GetState()
	}
	return state, nil
}

func (s *Service) saveCloudSyncConfig(input CloudSyncConfig, previousConnection *string) (AppState, error) {
	c := s.cloud
	// A session keeps its endpoint and credentials until its conditional commit
	// finishes. Wait before taking the local mutation lock so a slow cloud
	// request cannot make an unrelated project or library edit wait with it.
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.mu.Lock()
	c.loadSecretsLocked()
	*previousConnection = c.r2Config.R2ConnectionID
	previous := c.manualConfig
	if input.Provider == "r2-oauth" {
		previous = c.r2Config
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config, err := s.mergedCloudProfile(ctx, input, previous)
	if err != nil {
		return AppState{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c.mu.Lock()
	lastSyncAt := c.lastSyncAt
	if config.Endpoint != c.config.Endpoint || config.Bucket != c.config.Bucket || config.Prefix != c.config.Prefix || config.Passphrase != c.config.Passphrase {
		lastSyncAt = ""
	}
	if err = c.persistLocked(config, lastSyncAt); err != nil {
		c.mu.Unlock()
		return AppState{}, err
	}
	c.config, c.lastSyncAt = config, lastSyncAt
	if config.Provider == "r2-oauth" {
		c.r2Config = config
	} else {
		c.manualConfig = config
	}
	c.lastError, c.resolutionToken = "", ""
	c.conflicts = []skillstore.SyncConflict{}
	c.lastAttempt = time.Time{}
	c.mu.Unlock()
	return s.GetState()
}

func (s *Service) TestCloudSyncConnection(ctx context.Context, input CloudSyncConfig) error {
	s.cloud.runMu.Lock()
	defer s.cloud.runMu.Unlock()
	s.cloud.mu.Lock()
	s.cloud.loadSecretsLocked()
	previous := s.cloud.manualConfig
	if input.Provider == "r2-oauth" {
		previous = s.cloud.r2Config
	}
	s.cloud.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	config, err := s.mergedCloudProfile(ctx, input, previous)
	if err != nil {
		return err
	}
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
	return s.runCloudSync(ctx, nil, "", true)
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
	return s.runCloudSync(ctx, choices, "", true)
}

func (s *Service) runCloudSync(ctx context.Context, resolutions map[string]string, restoreID string, explicit bool) (AppState, error) {
	c := s.cloud
	if !c.runMu.TryLock() {
		return AppState{}, errors.New("cloud sync is already running")
	}
	defer c.runMu.Unlock()
	if s.lifecycle.stopping.Load() {
		return AppState{}, context.Canceled
	}
	// Configuration saves share runMu, keeping this session's destination stable
	// without holding the application's local mutation lock across network I/O.
	c.mu.Lock()
	if err := c.activeSecretsLocked(explicit); err != nil {
		c.lastError, c.lastAttempt = err.Error(), time.Now()
		c.mu.Unlock()
		return AppState{}, err
	}
	config := c.config
	c.syncing, c.lastError, c.lastAttempt = true, "", time.Now()
	c.mu.Unlock()
	// Only the final state check, conditional publication, and local commit take
	// the mutation lock. Reads and content transfers leave local edits available;
	// the final critical section preserves compound edits and deletion history.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var result cloudsync.Result
	config, err := s.r2ConfigForUse(ctx, config)
	if err == nil {
		err = cloudsync.ValidateConfig(config.remote())
	}
	if err != nil {
		// The ordinary completion path records credential refresh errors too.
	} else if restoreID != "" {
		result, err = cloudsync.RestoreBackupWithLocalLock(ctx, s.store, config.remote(), restoreID, &s.mu)
	} else {
		result, err = cloudsync.SyncWithLocalLock(ctx, s.store, config.remote(), resolutions, &s.mu)
	}
	// Transferred objects persist across attempts, so a large first sync that
	// reaches the per-attempt limit resumes on the next tick instead of waiting
	// a full interval.
	resume := errors.Is(err, context.DeadlineExceeded) && result.Downloaded+result.Uploaded > 0
	err = redactCloudError(err, config)
	c.mu.Lock()
	c.syncing = false
	if resume {
		c.lastAttempt = time.Time{}
	}
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
	s.cloud.runMu.Lock()
	defer s.cloud.runMu.Unlock()
	s.cloud.mu.Lock()
	config := s.cloud.config
	s.cloud.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	config, err := s.r2ConfigForUse(ctx, config)
	if err != nil {
		return nil, err
	}
	backups, err := cloudsync.ListBackups(ctx, config.remote())
	return backups, redactCloudError(err, config)
}

func (s *Service) RestoreCloudBackup(ctx context.Context, id string) (AppState, error) {
	if id == "" {
		return AppState{}, errors.New("choose a cloud backup to restore")
	}
	return s.runCloudSync(ctx, nil, id, true)
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
				due := !s.lifecycle.stopping.Load() && c.config.AutoSync && !c.syncing && len(c.conflicts) == 0 &&
					(c.lastAttempt.IsZero() || time.Since(c.lastAttempt) >= time.Duration(c.config.IntervalMinutes)*time.Minute)
				c.mu.Unlock()
				if due {
					_, _ = s.runCloudSync(ctx, nil, "", false)
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
