package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/loopback"
	"racomi/internal/buildinfo"
)

// Set by the distributor with -X, or by the PoC environment. This is a public
// identifier; the application never needs a client secret.
var cloudflareOAuthClientID string

const r2OAuthRedirectURI = "http://localhost:18796/oauth/cloudflare/callback"

const r2OAuthInvalidScopeMessage = "Cloudflare rejected the requested permissions (invalid_scope). This build's OAuth client is not allowed to request them; its Cloudflare OAuth client settings must allow the Refresh Token grant and every requested scope."

const r2OAuthDeniedMessage = "Cloudflare authorization was denied. Retry, and on the Cloudflare page click Edit Permissions and turn on Account API Tokens Write before you authorize."

// The callback page the browser shows after sign-in.
var r2OAuthPages = loopback.Messages{
	Connected:     "Cloudflare is connected. You can close this page and return to racomi to choose a private bucket.",
	Denied:        "Cloudflare sign-in was denied. Return to racomi to retry, and turn on Account API Tokens Write under Edit Permissions before you authorize.",
	InvalidScope:  "Cloudflare rejected the permissions racomi requested. Return to racomi for details.",
	NotAuthorized: "Cloudflare sign-in was not authorized. Return to racomi to retry.",
	Canceled:      "Cloudflare sign-in was canceled. Return to racomi to retry.",
	Failed:        "Cloudflare sign-in could not be saved. Return to racomi for details.",
}

var r2WarningMessages = map[r2connect.Warning]string{
	r2connect.WarningUnusedToken:          "An unused R2 token could not be removed. Disconnect Cloudflare to retry cleanup.",
	r2connect.WarningRetiredToken:         "R2 credentials were renewed, but an older expiring token could not be removed. Disconnect Cloudflare to retry cleanup.",
	r2connect.WarningAuthorizationCleanup: "Cloudflare is connected, but an older local authorization could not be removed. Disconnect to retry cleanup.",
}

type R2OAuthStatus struct {
	Available   bool   `json:"available"`
	Connected   bool   `json:"connected"`
	Authorizing bool   `json:"authorizing"`
	LastError   string `json:"lastError"`
}

// r2OAuthRuntime adapts the r2connect Manager to racomi's sign-in status. The
// Manager calls back into it for warnings, so r.mu is never held while calling
// the Manager.
type r2OAuthRuntime struct {
	mu              sync.Mutex
	workers         sync.WaitGroup // Callback servers must stop before service cleanup.
	path            string
	client          *r2connect.Client
	secrets         r2SecretStore
	manager         *r2connect.Manager
	registrationErr error
	lastError       string
	authorizing     bool
	generation      uint64
	cancel          context.CancelFunc
	flow            *loopback.Flow
	openURL         func(string) error
	redirectURI     string
	timeout         time.Duration
	diagnostics     *r2DiagnosticLog
}

func newR2OAuthRuntime(root string) *r2OAuthRuntime {
	return newR2OAuthRuntimeWithRegistration(root, cloudflareRegistration)
}

func newR2OAuthRuntimeWithRegistration(root string, registration []byte) *r2OAuthRuntime {
	r := &r2OAuthRuntime{path: filepath.Join(root, "r2-oauth-state.json"), redirectURI: r2OAuthRedirectURI, timeout: 5 * time.Minute,
		diagnostics: newR2DiagnosticLog(root, buildinfo.Current().Channel == "dev")}
	fallbackID := ""
	if saved, err := r2connect.LoadState(r2connect.FileStateStore{Path: r.path}); err == nil {
		fallbackID = saved.ClientID
	}
	config, err := loadR2Registration(registration, fallbackID, cloudflareOAuthClientID, cloudflareOAuthRedirectURL, os.Getenv)
	if err == nil {
		r.redirectURI = config.RedirectURL
	}
	r.registrationErr = err
	r.use(r2connect.NewClient(config.ClientID), newR2SecretStore(root))
	result := "ready"
	if r.unavailable() != nil {
		result = "initialization_failed"
	}
	r.diagnostics.write(r2DiagnosticEvent{Stage: "initialized", Result: result, ClientConfigured: diagnosticBool(config.ClientID != "")})
	return r
}

// use connects the runtime to a client and credential store and reloads the
// saved authorization. Tests call it to substitute isolated fixtures.
func (r *r2OAuthRuntime) use(client *r2connect.Client, secrets r2SecretStore) {
	r.client, r.secrets = client, secrets
	if store, ok := secrets.(*r2SystemSecretStore); ok {
		store.OnAccessDenied = func() {
			r.diagnostics.write(r2DiagnosticEvent{Stage: "credential_access_denied", Result: "automatic_retries_stopped"})
		}
	}
	client.Diagnostic = func(event r2connect.DiagnosticEvent) {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "cloudflare_api", API: &event})
	}
	r.manager = r2connect.NewManager(client, r2connect.ManagerOptions{Secrets: secrets, State: r2connect.FileStateStore{Path: r.path}, TokenNamePrefix: "racomi-",
		OnWarning: func(warning r2connect.Warning) {
			r.mu.Lock()
			r.lastError = r2WarningMessages[warning]
			r.mu.Unlock()
		}})
	r.lastError = ""
	if r.manager.LoadError() != nil {
		r.lastError = "Cannot read Cloudflare authorization metadata; the original file has been preserved."
	} else if r.registrationErr != nil {
		r.lastError = r.registrationErr.Error()
	}
}

// unavailable reports why Cloudflare management cannot run in this build or
// library: unreadable saved metadata, or an invalid registration.
func (r *r2OAuthRuntime) unavailable() error {
	if err := r.manager.LoadError(); err != nil {
		return err
	}
	return r.registrationErr
}

func (s *Service) GetR2OAuthStatus() R2OAuthStatus {
	if s.r2 == nil {
		return R2OAuthStatus{LastError: "Cloudflare connection is unavailable."}
	}
	r := s.r2
	unavailable := r.unavailable()
	authorized := r.manager.Status().Authorized
	r.mu.Lock()
	defer r.mu.Unlock()
	status := R2OAuthStatus{Available: r.client.ClientID != "" && unavailable == nil, Connected: unavailable == nil && authorized, Authorizing: r.authorizing, LastError: r.lastError}
	if r.client.ClientID == "" && status.LastError == "" {
		status.LastError = "Cloudflare sign-in is not configured in this build. You can use manual S3 configuration."
	}
	return status
}

// r2FlowResult maps a finished sign-in to its diagnostic result and the error
// shown in status.
func r2FlowResult(err error) (result, message string) {
	var oauthErr *loopback.OAuthError
	switch {
	case err == nil:
		return "completed", ""
	case errors.Is(err, loopback.ErrDenied):
		return "denied", r2OAuthDeniedMessage
	case errors.Is(err, loopback.ErrInvalidScope):
		return "failed", r2OAuthInvalidScopeMessage
	case errors.As(err, &oauthErr):
		return "failed", "Cloudflare sign-in was not authorized. You can retry."
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout", "Cloudflare sign-in timed out or was canceled. You can retry."
	case errors.Is(err, context.Canceled):
		return "canceled", ""
	case errors.Is(err, loopback.ErrCallbackStopped):
		return "failed", "The Cloudflare sign-in callback stopped. Retry sign-in."
	}
	return "failed", err.Error()
}

func (s *Service) StartR2OAuth() (R2OAuthStatus, error) {
	r := s.r2
	if r == nil {
		return R2OAuthStatus{}, errors.New("Cloudflare connection is unavailable.")
	}
	unavailable, clientErr := r.unavailable(), r.manager.CheckClient()
	r.mu.Lock()
	r.diagnostics.write(r2DiagnosticEvent{Stage: "start_requested", Generation: r.generation})
	if unavailable != nil || r.client.ClientID == "" {
		r.mu.Unlock()
		status := s.GetR2OAuthStatus()
		return status, errors.New(status.LastError)
	}
	if r.authorizing {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "start_reused", Generation: r.generation})
		r.mu.Unlock()
		return s.GetR2OAuthStatus(), nil
	}
	if clientErr != nil {
		r.mu.Unlock()
		return s.GetR2OAuthStatus(), clientErr
	}
	generation := r.generation + 1
	flow, err := loopback.Start(r.client, loopback.Options{
		RedirectURI: r.redirectURI,
		Messages:    r2OAuthPages,
		// Credential-store I/O runs outside r.mu, so cancellation and status
		// polling stay available. The Manager commits nothing once ctx ends.
		Accept: func(ctx context.Context, tokens r2connect.TokenSet) error {
			err := r.manager.Authorize(ctx, tokens)
			if err == nil {
				r.diagnostics.write(r2DiagnosticEvent{Stage: "authorization_saved", Generation: generation, Result: "success"})
			}
			return err
		},
		Done: func(err error) {
			result, message := r2FlowResult(err)
			r.mu.Lock()
			current := generation == r.generation
			if current {
				r.authorizing = false
				// A successful sign-in keeps any cleanup warning raised while it saved.
				if result != "completed" {
					r.lastError = message
				} else if r.lastError != "" {
					result = "completed_with_cleanup_warning"
				}
			}
			r.mu.Unlock()
			r.diagnostics.write(r2DiagnosticEvent{Stage: "finished", Generation: generation, Result: result})
		},
		Diagnostic: func(event loopback.Event) {
			r.diagnostics.write(r2DiagnosticEvent{Stage: event.Stage, Generation: generation, Result: event.Result,
				MethodMatches: event.MethodMatches, HostMatches: event.HostMatches, PathMatches: event.PathMatches,
				StatePresent: event.StatePresent, StateMatches: event.StateMatches, CodePresent: event.CodePresent,
				OAuthErrorPresent: event.OAuthErrorPresent, OAuthError: event.OAuthError})
		},
	})
	if err != nil {
		r.mu.Unlock()
		if errors.Is(err, loopback.ErrListen) {
			r.diagnostics.write(r2DiagnosticEvent{Stage: "listen_failed", Generation: r.generation, Result: "listen_error"})
			return R2OAuthStatus{}, errors.New("The Cloudflare callback port is in use. Close the other sign-in window and retry.")
		}
		if errors.Is(err, loopback.ErrRedirect) {
			return R2OAuthStatus{}, errors.New("Invalid Cloudflare callback configuration.")
		}
		return R2OAuthStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	r.generation = generation
	r.diagnostics.write(r2DiagnosticEvent{Stage: "listen_started", Generation: generation})
	r.cancel, r.flow, r.authorizing, r.lastError = cancel, flow, true, ""
	openURL := r.openURL
	if store, ok := r.secrets.(interface{ retryAccess() }); ok {
		store.retryAccess()
	}
	r.workers.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.workers.Done()
		defer cancel()
		_, _ = flow.Wait(ctx)
		r.mu.Lock()
		if generation == r.generation {
			r.cancel, r.flow = nil, nil
		}
		r.mu.Unlock()
	}()
	if openURL == nil {
		err = errors.New("Cannot open the browser for Cloudflare sign-in.")
	} else {
		err = openURL(flow.URL())
	}
	if err != nil {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "browser_open_failed", Generation: generation})
		s.CancelR2OAuth()
		return s.GetR2OAuthStatus(), err
	}
	r.diagnostics.write(r2DiagnosticEvent{Stage: "browser_opened", Generation: generation})
	return s.GetR2OAuthStatus(), nil
}

func (s *Service) CancelR2OAuth() R2OAuthStatus {
	if s.r2 == nil {
		return s.GetR2OAuthStatus()
	}
	r := s.r2
	r.mu.Lock()
	if r.authorizing || r.flow != nil {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "cancel_requested", Generation: r.generation})
	}
	r.generation++
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.flow != nil {
		_ = r.flow.Close()
		r.flow = nil
	}
	r.authorizing = false
	r.lastError = ""
	r.mu.Unlock()
	return s.GetR2OAuthStatus()
}

func (s *Service) ListR2Accounts(ctx context.Context) ([]r2connect.Account, error) {
	r := s.r2
	if r == nil {
		return nil, errors.New("Cloudflare connection is unavailable.")
	}
	if err := r.unavailable(); err != nil {
		return nil, err
	}
	return r.manager.Accounts(ctx)
}

func (s *Service) ListR2Buckets(ctx context.Context, accountID, jurisdiction string) ([]r2connect.Bucket, error) {
	r := s.r2
	if r == nil {
		return nil, errors.New("Cloudflare connection is unavailable.")
	}
	if err := r.unavailable(); err != nil {
		return nil, err
	}
	return r.manager.Buckets(ctx, accountID, jurisdiction)
}

func (s *Service) ConfigureR2Bucket(ctx context.Context, accountID, bucket, jurisdiction string) (CloudSyncConfig, error) {
	r := s.r2
	if r == nil {
		return CloudSyncConfig{}, errors.New("Cloudflare connection is unavailable.")
	}
	endpoint, err := r2connect.Endpoint(accountID, jurisdiction)
	if err != nil {
		return CloudSyncConfig{}, err
	}
	if err = r.unavailable(); err != nil {
		return CloudSyncConfig{}, err
	}
	connection, err := r.manager.Connect(ctx, accountID, bucket, jurisdiction)
	if err != nil {
		return CloudSyncConfig{}, err
	}
	config := defaultCloudSyncConfig()
	config.Provider, config.R2ConnectionID, config.AccountID, config.Jurisdiction = "r2-oauth", connection.ID, accountID, jurisdiction
	config.Endpoint, config.Bucket, config.HasSecretAccessKey = endpoint, bucket, true
	return config, nil
}

func (s *Service) r2ConfigForUse(ctx context.Context, config CloudSyncConfig) (CloudSyncConfig, error) {
	if config.Provider != "r2-oauth" {
		return config, nil
	}
	endpoint, err := r2connect.Endpoint(config.AccountID, config.Jurisdiction)
	if err != nil {
		return config, err
	}
	if config.Endpoint != endpoint || config.Region != "auto" || !config.PathStyle {
		return config, errors.New("R2 endpoint and addressing must match the selected Cloudflare bucket.")
	}
	if s.r2 == nil {
		return config, errors.New("Cloudflare connection is unavailable.")
	}
	credentials, err := s.r2.manager.Credentials(ctx, r2connect.Connection{ID: config.R2ConnectionID, AccountID: config.AccountID, Bucket: config.Bucket, Jurisdiction: config.Jurisdiction})
	if err != nil {
		return config, err
	}
	config.AccessKeyID, config.SecretAccessKey, config.SessionToken = credentials.AccessKeyID, credentials.SecretAccessKey, ""
	return config, nil
}

func r2DiskConfig(config CloudSyncConfig) CloudSyncConfig {
	if config.Provider == "r2-oauth" {
		config.HasSecretAccessKey = config.HasSecretAccessKey || config.SecretAccessKey != ""
		config.AccessKeyID, config.SecretAccessKey, config.SessionToken = "", "", ""
	}
	return config
}

func cloudConfigReady(config CloudSyncConfig) bool {
	return config.Endpoint != "" && config.Bucket != "" && config.Passphrase != "" && (config.SecretAccessKey != "" || config.Provider == "r2-oauth" && config.R2ConnectionID != "" && config.HasSecretAccessKey)
}

func (s *Service) mergedCloudProfile(ctx context.Context, input, previous CloudSyncConfig) (CloudSyncConfig, error) {
	if input.Provider != "" && input.Provider != "s3" && input.Provider != "r2-oauth" {
		return input, errors.New("Choose Cloudflare R2 or manual S3 configuration.")
	}
	if input.Provider != "r2-oauth" {
		input.R2ConnectionID, input.AccountID, input.Jurisdiction = "", "", ""
		return mergedCloudConfig(input, previous)
	}
	input.Region, input.PathStyle = "auto", true
	input, err := s.r2ConfigForUse(ctx, input)
	if err != nil {
		return input, err
	}
	input, err = mergedCloudConfig(input, previous)
	if err == nil {
		input.HasSecretAccessKey = true
	}
	return input, err
}

func (s *Service) DisconnectR2OAuth(ctx context.Context) (R2OAuthStatus, error) {
	if s.r2 == nil || s.cloud == nil {
		return s.GetR2OAuthStatus(), errors.New("Cloudflare connection is unavailable.")
	}
	c := s.cloud
	c.runMu.Lock()
	defer c.runMu.Unlock()
	s.CancelR2OAuth()
	r := s.r2
	if err := r.unavailable(); err != nil {
		return s.GetR2OAuthStatus(), err
	}
	if err := r.manager.Disconnect(ctx); err != nil {
		if errors.Is(err, r2connect.ErrCleanupPending) {
			err = errors.New("Reconnect Cloudflare to remove the remaining racomi R2 tokens.")
		}
		return s.GetR2OAuthStatus(), err
	}
	r.mu.Lock()
	r.lastError = ""
	r.mu.Unlock()
	c.mu.Lock()
	r2 := defaultCloudSyncConfig()
	r2.Provider = "r2-oauth"
	previousR2 := c.r2Config
	c.r2Config = r2
	config, lastSyncAt := c.config, c.lastSyncAt
	if config.Provider == "r2-oauth" {
		config, lastSyncAt = r2, ""
	}
	err := c.persistLocked(config, lastSyncAt)
	if err != nil {
		c.r2Config = previousR2
	} else {
		c.config, c.lastSyncAt = config, lastSyncAt
		if config.Provider == "r2-oauth" {
			c.conflicts = nil
			c.resolutionToken = ""
			c.lastAttempt = time.Time{}
		}
	}
	c.mu.Unlock()
	return s.GetR2OAuthStatus(), err
}
