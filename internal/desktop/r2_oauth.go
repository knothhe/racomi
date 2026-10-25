package desktop

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"racomi/internal/buildinfo"
	"racomi/internal/cloudflare"
)

// Set by the distributor with -X, or by the PoC environment. This is a public
// identifier; the application never needs a client secret.
var cloudflareOAuthClientID string

const r2OAuthRedirectURI = "http://localhost:18796/oauth/cloudflare/callback"

// R2 credentials last 30 days. With a refresh token they are renewed silently
// once inside the renewal window; otherwise only just before they expire.
const (
	r2CredentialLifetime  = 30 * 24 * time.Hour
	r2SilentRenewalWindow = 7 * 24 * time.Hour
	r2ExpiryMargin        = 5 * time.Minute
	r2RenewalRetryDelay   = time.Hour
)

const r2OAuthInvalidScopeMessage = "Cloudflare rejected the requested permissions (invalid_scope). This build's OAuth client is not allowed to request them; its Cloudflare OAuth client settings must allow the Refresh Token grant and every requested scope."

const r2OAuthDeniedMessage = "Cloudflare authorization was denied. Retry, and on the Cloudflare page click Edit Permissions and turn on Account API Tokens Write before you authorize."

type R2OAuthStatus struct {
	Available   bool   `json:"available"`
	Connected   bool   `json:"connected"`
	Authorizing bool   `json:"authorizing"`
	LastError   string `json:"lastError"`
}

type r2Connection struct {
	ID            string    `json:"id"`
	AccountID     string    `json:"accountId"`
	Bucket        string    `json:"bucket"`
	Jurisdiction  string    `json:"jurisdiction"`
	TokenID       string    `json:"tokenId"`
	ExpiresAt     time.Time `json:"expiresAt"`
	CredentialKey string    `json:"credentialKey,omitempty"`
}

type r2RetiredToken struct {
	AccountID     string `json:"accountId"`
	TokenID       string `json:"tokenId"`
	CredentialKey string `json:"credentialKey,omitempty"`
}

type r2OAuthState struct {
	Version            int              `json:"version"`
	ClientID           string           `json:"clientId"`
	Authorized         bool             `json:"authorized"`
	Connections        []r2Connection   `json:"connections"`
	RetiredTokens      []r2RetiredToken `json:"retiredTokens,omitempty"`
	OAuthCredentialKey string           `json:"oauthCredentialKey,omitempty"`
	// Renewable records that the saved grant has a refresh token, so sync can
	// decide on early renewal without reading the OAuth secret.
	Renewable        bool     `json:"renewable,omitempty"`
	RetiredOAuthKeys []string `json:"retiredOAuthKeys,omitempty"`
}

type r2OAuthRuntime struct {
	mu          sync.Mutex
	workers     sync.WaitGroup // Callback servers must stop before service cleanup.
	path        string
	client      *cloudflare.Client
	secrets     r2SecretStore
	state       r2OAuthState
	loadError   error
	lastError   string
	authorizing bool
	generation  uint64
	cancel      context.CancelFunc
	listener    net.Listener
	openURL     func(string) error
	redirectURI string
	diagnostics *r2DiagnosticLog
	// A rotating refresh token must survive a transient Keychain write failure
	// without reusing its now-invalid predecessor on the next attempt.
	pendingTokens *cloudflare.TokenSet
	// Metadata writes can fail after an external mutation. Keep compensation
	// records in memory until a subsequent successful write can persist them.
	pendingRetired       []r2RetiredToken
	saveMetadata         func(string, r2OAuthState) error
	authorizationRevoked bool
	// Failed early renewals wait before retrying while credentials stay valid.
	renewRetryAt time.Time
}

func newR2OAuthRuntime(root string) *r2OAuthRuntime {
	return newR2OAuthRuntimeWithRegistration(root, cloudflareRegistration)
}

func newR2OAuthRuntimeWithRegistration(root string, registration []byte) *r2OAuthRuntime {
	r := &r2OAuthRuntime{path: filepath.Join(root, "r2-oauth-state.json"), secrets: newR2SecretStore(root), redirectURI: r2OAuthRedirectURI,
		diagnostics: newR2DiagnosticLog(root, buildinfo.Current().Channel == "dev"),
		state:       r2OAuthState{Version: 1, Connections: []r2Connection{}}}
	info, err := os.Lstat(r.path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 64<<10 {
			err = errors.New("invalid Cloudflare authorization metadata")
		} else {
			var data []byte
			data, err = os.ReadFile(r.path)
			if err == nil {
				err = json.Unmarshal(data, &r.state)
			}
			if err == nil {
				err = validateR2Metadata(r.state)
			}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	r.loadError = err
	if err != nil {
		r.lastError = "Cannot read Cloudflare authorization metadata; the original file has been preserved."
	}
	fallbackID := ""
	if err == nil {
		fallbackID = r.state.ClientID
	}
	config, registrationErr := loadR2Registration(registration, fallbackID, cloudflareOAuthClientID, cloudflareOAuthRedirectURL, os.Getenv)
	if registrationErr != nil && r.loadError == nil {
		r.loadError, r.lastError = registrationErr, registrationErr.Error()
	}
	r.client = cloudflare.NewClient(config.ClientID)
	if store, ok := r.secrets.(*r2SystemSecretStore); ok {
		store.onAccessDenied = func() {
			r.diagnostics.write(r2DiagnosticEvent{Stage: "credential_access_denied", Result: "automatic_retries_stopped"})
		}
	}
	r.client.Diagnostic = func(event cloudflare.DiagnosticEvent) {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "cloudflare_api", API: &event})
	}
	if registrationErr == nil {
		r.redirectURI = config.RedirectURL
	}
	result := "ready"
	if r.loadError != nil {
		result = "initialization_failed"
	}
	r.diagnostics.write(r2DiagnosticEvent{Stage: "initialized", Result: result, ClientConfigured: diagnosticBool(config.ClientID != "")})
	return r
}

func writeR2Metadata(path string, state r2OAuthState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return errors.New("Cloudflare authorization metadata is too large")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".r2-oauth-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (r *r2OAuthRuntime) saveLocked(next r2OAuthState) error {
	if r.loadError != nil {
		return r.loadError
	}
	next.RetiredTokens = append([]r2RetiredToken{}, next.RetiredTokens...)
	for _, retired := range r.pendingRetired {
		next.RetiredTokens = appendRetiredToken(next.RetiredTokens, retired)
	}
	write := r.saveMetadata
	if write == nil {
		write = writeR2Metadata
	}
	if err := write(r.path, next); err != nil {
		return err
	}
	r.state = next
	r.pendingRetired = nil
	return nil
}

func (r *r2OAuthRuntime) oauthKey() string {
	if r.state.OAuthCredentialKey != "" {
		return r.state.OAuthCredentialKey
	}
	return "oauth-" + r.client.ClientID
}

func r2ConnectionKey(connection r2Connection) string {
	if connection.CredentialKey != "" {
		return connection.CredentialKey
	}
	return "connection-" + connection.ID
}

func appendRetiredToken(tokens []r2RetiredToken, token r2RetiredToken) []r2RetiredToken {
	for _, item := range tokens {
		if item.AccountID == token.AccountID && item.TokenID == token.TokenID && item.CredentialKey == token.CredentialKey {
			return tokens
		}
	}
	return append(tokens, token)
}

func validateR2Metadata(state r2OAuthState) error {
	if state.Version != 1 {
		return errors.New("unsupported Cloudflare authorization metadata")
	}
	if state.Authorized && (state.ClientID == "" || len(state.ClientID) > 256 || strings.ContainsAny(state.ClientID, " \t\r\n")) {
		return errors.New("invalid Cloudflare authorization metadata")
	}
	if state.OAuthCredentialKey != "" && (!validR2SecretKey(state.OAuthCredentialKey) || !strings.HasPrefix(state.OAuthCredentialKey, "oauth-")) {
		return errors.New("invalid Cloudflare authorization metadata")
	}
	for _, key := range state.RetiredOAuthKeys {
		if !validR2SecretKey(key) || !strings.HasPrefix(key, "oauth-") {
			return errors.New("invalid Cloudflare authorization cleanup metadata")
		}
	}
	seen := make(map[string]bool)
	for _, connection := range state.Connections {
		if len(connection.ID) < 12 || len(connection.ID) > 80 || strings.ContainsAny(connection.ID, " /\\\x00\r\n\t") || seen[connection.ID] || connection.Bucket == "" || connection.ExpiresAt.IsZero() || !r2TokenIDValid(connection.TokenID) || !validR2SecretKey(r2ConnectionKey(connection)) || !strings.HasPrefix(r2ConnectionKey(connection), "connection-") {
			return errors.New("invalid Cloudflare connection metadata")
		}
		if _, err := cloudflare.R2Endpoint(connection.AccountID, connection.Jurisdiction); err != nil {
			return errors.New("invalid Cloudflare connection metadata")
		}
		seen[connection.ID] = true
	}
	for _, retired := range state.RetiredTokens {
		if !r2TokenIDValid(retired.TokenID) || retired.CredentialKey != "" && (!validR2SecretKey(retired.CredentialKey) || !strings.HasPrefix(retired.CredentialKey, "connection-")) {
			return errors.New("invalid Cloudflare credential cleanup metadata")
		}
		if _, err := cloudflare.R2Endpoint(retired.AccountID, "default"); err != nil {
			return errors.New("invalid Cloudflare credential cleanup metadata")
		}
	}
	return nil
}

func r2TokenIDValid(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'f') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func (r *r2OAuthRuntime) tokensLocked(ctx context.Context) (cloudflare.TokenSet, error) {
	var tokens cloudflare.TokenSet
	if r.loadError != nil {
		return tokens, r.loadError
	}
	if !r.state.Authorized || r.state.ClientID != r.client.ClientID {
		return tokens, errors.New("Connect Cloudflare before choosing a bucket.")
	}
	if r.pendingTokens != nil {
		tokens = *r.pendingTokens
		if err := r.saveTokensLocked(tokens); err != nil {
			return cloudflare.TokenSet{}, err
		}
	} else {
		data, err := r.secrets.Load(r.oauthKey())
		if err != nil {
			return tokens, err
		}
		err = json.Unmarshal(data, &tokens)
		clear(data)
		if err != nil || tokens.AccessToken == "" || tokens.ExpiresAt.IsZero() {
			return cloudflare.TokenSet{}, errors.New("Saved Cloudflare authorization is invalid. Reconnect Cloudflare.")
		}
	}
	remaining := time.Until(tokens.ExpiresAt)
	if tokens.RefreshToken == "" {
		if remaining <= 0 {
			return cloudflare.TokenSet{}, errors.New("Cloudflare authorization expired. Connect Cloudflare again to manage R2 credentials.")
		}
	} else if remaining <= time.Minute {
		next, err := r.client.Refresh(ctx, tokens)
		if err != nil {
			return cloudflare.TokenSet{}, err
		}
		r.pendingTokens = &next
		if err = r.saveTokensLocked(next); err != nil {
			return cloudflare.TokenSet{}, err
		}
		tokens = next
	}
	if !tokens.HasScopes(cloudflare.RequiredScopes()...) {
		return cloudflare.TokenSet{}, errors.New("Cloudflare authorization is missing required permissions. Reconnect and grant the requested permissions.")
	}
	return tokens, nil
}

func (r *r2OAuthRuntime) saveTokensLocked(tokens cloudflare.TokenSet) error {
	data, err := json.Marshal(tokens)
	if err == nil {
		err = r.secrets.Save(r.oauthKey(), data)
	}
	clear(data)
	if err != nil {
		return err
	}
	r.pendingTokens = nil
	return nil
}

func (s *Service) GetR2OAuthStatus() R2OAuthStatus {
	if s.r2 == nil {
		return R2OAuthStatus{LastError: "Cloudflare connection is unavailable."}
	}
	r := s.r2
	r.mu.Lock()
	defer r.mu.Unlock()
	status := R2OAuthStatus{Available: r.client.ClientID != "" && r.loadError == nil, Connected: r.loadError == nil && r.state.Authorized && r.state.ClientID == r.client.ClientID, Authorizing: r.authorizing, LastError: r.lastError}
	if r.client.ClientID == "" && status.LastError == "" {
		status.LastError = "Cloudflare sign-in is not configured in this build. You can use manual S3 configuration."
	}
	return status
}

func (s *Service) StartR2OAuth() (R2OAuthStatus, error) {
	r := s.r2
	if r == nil {
		return R2OAuthStatus{}, errors.New("Cloudflare connection is unavailable.")
	}
	r.mu.Lock()
	r.diagnostics.write(r2DiagnosticEvent{Stage: "start_requested", Generation: r.generation})
	if r.loadError != nil || r.client.ClientID == "" {
		r.mu.Unlock()
		status := s.GetR2OAuthStatus()
		return status, errors.New(status.LastError)
	}
	if r.authorizing {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "start_reused", Generation: r.generation})
		r.mu.Unlock()
		return s.GetR2OAuthStatus(), nil
	}
	if r.state.ClientID != "" && r.state.ClientID != r.client.ClientID && (len(r.state.Connections) > 0 || len(r.state.RetiredTokens) > 0 || len(r.pendingRetired) > 0) {
		r.mu.Unlock()
		return s.GetR2OAuthStatus(), errors.New("The OAuth client ID changed. Restore the original client ID and disconnect its R2 buckets before using a different OAuth client.")
	}
	state, verifier, err := cloudflare.NewPKCE()
	if err != nil {
		r.mu.Unlock()
		return R2OAuthStatus{}, err
	}
	u, err := r2CallbackURL(r.redirectURI)
	if err != nil {
		r.mu.Unlock()
		return R2OAuthStatus{}, errors.New("Invalid Cloudflare callback configuration.")
	}
	listener, err := listenR2Callback(u)
	if err != nil {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "listen_failed", Generation: r.generation, Result: "listen_error"})
		r.mu.Unlock()
		return R2OAuthStatus{}, errors.New("The Cloudflare callback port is in use. Close the other sign-in window and retry.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	r.generation++
	generation := r.generation
	r.diagnostics.write(r2DiagnosticEvent{Stage: "listen_started", Generation: generation})
	r.cancel, r.listener, r.authorizing, r.lastError = cancel, listener, true, ""
	redirectURI := r.redirectURI
	authURL, err := r.client.AuthorizationURL(redirectURI, state, verifier)
	if err != nil {
		cancel()
		listener.Close()
		r.authorizing = false
		r.cancel, r.listener = nil, nil
		r.mu.Unlock()
		return R2OAuthStatus{}, err
	}
	openURL := r.openURL
	if store, ok := r.secrets.(interface{ retryAccess() }); ok {
		store.retryAccess()
	}
	r.workers.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.workers.Done()
		s.serveR2OAuth(ctx, listener, u.Host, u.Path, state, verifier, redirectURI, generation)
	}()
	if openURL == nil {
		err = errors.New("Cannot open the browser for Cloudflare sign-in.")
	} else {
		err = openURL(authURL)
	}
	if err != nil {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "browser_open_failed", Generation: generation})
		s.CancelR2OAuth()
		return s.GetR2OAuthStatus(), err
	}
	r.diagnostics.write(r2DiagnosticEvent{Stage: "browser_opened", Generation: generation})
	return s.GetR2OAuthStatus(), nil
}

func (s *Service) serveR2OAuth(ctx context.Context, listener net.Listener, host, path, state, verifier, redirectURI string, generation uint64) {
	r := s.r2
	if r == nil {
		listener.Close()
		return
	}
	var once sync.Once
	finished := make(chan struct{})
	finish := func(result, message string) {
		once.Do(func() {
			r.diagnostics.write(r2DiagnosticEvent{Stage: "finished", Generation: generation, Result: result})
			r.mu.Lock()
			if generation == r.generation {
				r.authorizing = false
				r.lastError = message
			}
			r.mu.Unlock()
			close(finished)
		})
	}
	finishCancellation := func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			finish("timeout", "Cloudflare sign-in timed out or was canceled. You can retry.")
			return
		}
		finish("canceled", "")
	}
	defer func() {
		r.mu.Lock()
		if generation == r.generation {
			if r.cancel != nil {
				r.cancel()
				r.cancel = nil
			}
			r.listener = nil
		}
		r.mu.Unlock()
		_ = listener.Close()
		r.diagnostics.write(r2DiagnosticEvent{Stage: "listener_closed", Generation: generation})
	}()
	consumed := false // Protected by r.mu; one code exchange per authorization.
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		q := req.URL.Query()
		stateMatches := len(q["state"]) == 1 && subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) == 1
		if req.Method != "GET" || req.Host != host || req.URL.Path != path {
			r.diagnostics.write(r2DiagnosticEvent{Stage: "callback_rejected", Generation: generation, Result: "invalid_request"})
			http.Error(w, "Invalid callback.", 400)
			return
		}
		if !stateMatches {
			r.diagnostics.write(r2DiagnosticEvent{Stage: "callback_rejected", Generation: generation, Result: "invalid_state"})
			http.Error(w, "Invalid authorization state.", 400)
			return
		}
		r.mu.Lock()
		if !r.authorizing || generation != r.generation || consumed || ctx.Err() != nil {
			r.mu.Unlock()
			r.diagnostics.write(r2DiagnosticEvent{Stage: "callback_rejected", Generation: generation, Result: "inactive"})
			http.Error(w, "Sign-in is no longer active.", 409)
			return
		}
		if q.Get("error") != "" {
			consumed = true
			r.mu.Unlock()
			// Cloudflare reports both Cancel and an incomplete grant as access_denied;
			// its consent page leaves the sensitive API token scope off by default.
			if q.Get("error") == "access_denied" {
				finish("denied", r2OAuthDeniedMessage)
				io.WriteString(w, "Cloudflare sign-in was denied. Return to racomi to retry, and turn on Account API Tokens Write under Edit Permissions before you authorize.")
				return
			}
			if q.Get("error") == "invalid_scope" {
				finish("failed", r2OAuthInvalidScopeMessage)
				io.WriteString(w, "Cloudflare rejected the permissions racomi requested. Return to racomi for details.")
				return
			}
			finish("failed", "Cloudflare sign-in was not authorized. You can retry.")
			io.WriteString(w, "Cloudflare sign-in was not authorized. Return to racomi to retry.")
			return
		}
		if len(q["code"]) != 1 || q.Get("code") == "" || len(q.Get("code")) > 8192 {
			r.mu.Unlock()
			r.diagnostics.write(r2DiagnosticEvent{Stage: "callback_rejected", Generation: generation, Result: "invalid_code"})
			http.Error(w, "Missing authorization code.", 400)
			return
		}
		consumed = true
		client := r.client
		previousAccounts := make(map[string]bool)
		for _, connection := range r.state.Connections {
			previousAccounts[connection.AccountID] = true
		}
		for _, retired := range append(append([]r2RetiredToken{}, r.state.RetiredTokens...), r.pendingRetired...) {
			previousAccounts[retired.AccountID] = true
		}
		r.mu.Unlock()
		// Network and credential-store I/O leave cancellation and status polling
		// available. Every late result is checked again before metadata commits.
		r.diagnostics.write(r2DiagnosticEvent{Stage: "code_exchange_started", Generation: generation})
		tokens, err := client.ExchangeCode(ctx, q.Get("code"), redirectURI, verifier)
		exchangeResult := "success"
		if err != nil {
			exchangeResult = "failed"
		}
		r.diagnostics.write(r2DiagnosticEvent{Stage: "code_exchange_finished", Generation: generation, Result: exchangeResult})
		if err == nil && !tokens.HasScopes(cloudflare.RequiredScopes()...) {
			err = errors.New("Cloudflare authorization is missing required permissions. Reconnect and grant the requested permissions.")
		}
		if err == nil && len(previousAccounts) > 0 {
			var accounts []cloudflare.Account
			accounts, err = client.Accounts(ctx, tokens.AccessToken)
			for _, account := range accounts {
				delete(previousAccounts, account.ID)
			}
			if err == nil && len(previousAccounts) > 0 {
				err = errors.New("Include every previously connected Cloudflare account when reconnecting, or disconnect its R2 buckets first.")
			}
		}
		newKey := ""
		if err == nil {
			var random string
			random, _, err = cloudflare.NewPKCE()
			if err == nil {
				newKey = "oauth-" + random
			}
		}
		if err == nil {
			var data []byte
			data, err = json.Marshal(tokens)
			if err == nil {
				err = r.secrets.Save(newKey, data)
			}
			clear(data)
		}
		completionMessage := ""
		r.mu.Lock()
		canceled := generation != r.generation || ctx.Err() != nil || !r.authorizing
		if err == nil && !canceled {
			oldKey := r.oauthKey()
			next := r.state
			next.ClientID, next.Authorized, next.OAuthCredentialKey, next.Renewable = client.ClientID, true, newKey, tokens.RefreshToken != ""
			next.RetiredOAuthKeys = append(append([]string{}, next.RetiredOAuthKeys...), oldKey)
			err = r.saveLocked(next)
			if err == nil {
				r.pendingTokens = nil
				r.authorizationRevoked = false
				if cleanupErr := r.cleanupOAuthKeysLocked(); cleanupErr != nil {
					completionMessage = "Cloudflare is connected, but an older local authorization could not be removed. Disconnect to retry cleanup."
				}
			}
		}
		r.mu.Unlock()
		if err != nil || canceled {
			if newKey != "" {
				_ = r.secrets.Delete(newKey)
			}
			discardR2Authorization(client, tokens)
			if canceled {
				finishCancellation()
				http.Error(w, "Cloudflare sign-in was canceled. Return to racomi to retry.", 409)
				return
			}
			finish("failed", err.Error())
			http.Error(w, "Cloudflare sign-in could not be saved. Return to racomi for details.", 400)
			return
		}
		r.diagnostics.write(r2DiagnosticEvent{Stage: "authorization_saved", Generation: generation, Result: "success"})
		completionResult := "completed"
		if completionMessage != "" {
			completionResult = "completed_with_cleanup_warning"
		}
		finish(completionResult, completionMessage)
		io.WriteString(w, "Cloudflare is connected. You can close this page and return to racomi to choose a private bucket.")
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		stateMatches := len(q["state"]) == 1 && subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) == 1
		r.diagnostics.write(r2DiagnosticEvent{Stage: "callback_received", Generation: generation,
			MethodMatches: diagnosticBool(req.Method == "GET"), HostMatches: diagnosticBool(req.Host == host), PathMatches: diagnosticBool(req.URL.Path == path),
			StatePresent: diagnosticBool(len(q["state"]) > 0), StateMatches: diagnosticBool(stateMatches),
			CodePresent: diagnosticBool(q.Get("code") != ""), OAuthErrorPresent: diagnosticBool(q.Get("error") != ""), OAuthError: r2OAuthErrorCode(q.Get("error"))})
		mux.ServeHTTP(w, req)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, MaxHeaderBytes: 16 << 10}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if ctx.Err() != nil {
				return // CancelR2OAuth closes the listener after canceling ctx.
			}
			r.diagnostics.write(r2DiagnosticEvent{Stage: "listener_error", Generation: generation})
			finish("failed", "The Cloudflare sign-in callback stopped. Retry sign-in.")
		}
	}()
	select {
	case <-ctx.Done():
		result := "canceled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result = "timeout"
		}
		r.diagnostics.write(r2DiagnosticEvent{Stage: "context_done", Generation: generation, Result: result})
		finishCancellation()
	case <-finished:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	_ = server.Close()
	<-serverDone
}

func discardR2Authorization(client *cloudflare.Client, tokens cloudflare.TokenSet) {
	token := tokens.RefreshToken
	if token == "" {
		token = tokens.AccessToken
	}
	if token == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = client.Revoke(ctx, token)
	}()
}

func (r *r2OAuthRuntime) cleanupOAuthKeysLocked() error {
	for len(r.state.RetiredOAuthKeys) > 0 {
		key := r.state.RetiredOAuthKeys[0]
		if key == r.oauthKey() {
			return errors.New("refusing to remove the active Cloudflare authorization")
		}
		if err := r.secrets.Delete(key); err != nil {
			return err
		}
		next := r.state
		next.RetiredOAuthKeys = append([]string{}, next.RetiredOAuthKeys[1:]...)
		if err := r.saveLocked(next); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CancelR2OAuth() R2OAuthStatus {
	if s.r2 == nil {
		return s.GetR2OAuthStatus()
	}
	r := s.r2
	r.mu.Lock()
	if r.authorizing || r.listener != nil {
		r.diagnostics.write(r2DiagnosticEvent{Stage: "cancel_requested", Generation: r.generation})
	}
	r.generation++
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.listener != nil {
		_ = r.listener.Close()
		r.listener = nil
	}
	r.authorizing = false
	r.lastError = ""
	r.mu.Unlock()
	return s.GetR2OAuthStatus()
}

func (s *Service) ListR2Accounts(ctx context.Context) ([]cloudflare.Account, error) {
	r := s.r2
	if r == nil {
		return nil, errors.New("Cloudflare connection is unavailable.")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tokens, err := r.tokensLocked(ctx)
	if err != nil {
		return nil, err
	}
	return r.client.Accounts(ctx, tokens.AccessToken)
}

func (s *Service) ListR2Buckets(ctx context.Context, accountID, jurisdiction string) ([]cloudflare.Bucket, error) {
	r := s.r2
	if r == nil {
		return nil, errors.New("Cloudflare connection is unavailable.")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tokens, err := r.tokensLocked(ctx)
	if err != nil {
		return nil, err
	}
	return r.client.Buckets(ctx, tokens.AccessToken, accountID, jurisdiction)
}

func (s *Service) ConfigureR2Bucket(ctx context.Context, accountID, bucket, jurisdiction string) (CloudSyncConfig, error) {
	r := s.r2
	if r == nil {
		return CloudSyncConfig{}, errors.New("Cloudflare connection is unavailable.")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	endpoint, err := cloudflare.R2Endpoint(accountID, jurisdiction)
	if err != nil {
		return CloudSyncConfig{}, err
	}
	tokens, err := r.tokensLocked(ctx)
	if err != nil {
		return CloudSyncConfig{}, err
	}
	// Verify selection with Cloudflare; never provision for a forged UI bucket.
	buckets, err := r.client.Buckets(ctx, tokens.AccessToken, accountID, jurisdiction)
	if err != nil {
		return CloudSyncConfig{}, err
	}
	found := false
	for _, candidate := range buckets {
		if candidate.Name == bucket {
			found = true
			break
		}
	}
	if !found {
		return CloudSyncConfig{}, errors.New("Choose a bucket available in this Cloudflare account and jurisdiction.")
	}
	if len(r.state.Connections) >= 16 {
		return CloudSyncConfig{}, errors.New("Too many pending Cloudflare connections. Disconnect Cloudflare to clean up before reconnecting.")
	}
	id, _, err := cloudflare.NewPKCE()
	if err != nil {
		return CloudSyncConfig{}, err
	}
	// Every version gets its own Keychain item. A failed metadata commit must
	// never replace the secret used by the previous, still-active connection.
	credentials, err := r.client.CreateR2Credentials(ctx, tokens.AccessToken, accountID, jurisdiction, bucket, "racomi-"+id[:12], time.Now().Add(r2CredentialLifetime))
	if err != nil {
		return CloudSyncConfig{}, err
	}
	key := "connection-" + id + "-" + credentials.TokenID
	data, err := json.Marshal(credentials)
	if err == nil {
		err = r.secrets.Save(key, data)
	}
	clear(data)
	if err == nil {
		err = ctx.Err()
	}
	connection := r2Connection{ID: id, AccountID: accountID, Bucket: bucket, Jurisdiction: jurisdiction, TokenID: credentials.TokenID, ExpiresAt: credentials.ExpiresAt, CredentialKey: key}
	if err == nil {
		next := r.state
		next.Connections = append(append([]r2Connection{}, next.Connections...), connection)
		err = r.saveLocked(next)
	}
	if err != nil {
		r.compensateCredentialLocked(tokens.AccessToken, r2RetiredToken{AccountID: accountID, TokenID: credentials.TokenID, CredentialKey: key})
		return CloudSyncConfig{}, err
	}
	config := defaultCloudSyncConfig()
	config.Provider, config.R2ConnectionID, config.AccountID, config.Jurisdiction = "r2-oauth", id, accountID, jurisdiction
	config.Endpoint, config.Bucket, config.HasSecretAccessKey = endpoint, bucket, true
	return config, nil
}

func (r *r2OAuthRuntime) rememberRetiredLocked(token r2RetiredToken) {
	next := r.state
	next.RetiredTokens = appendRetiredToken(append([]r2RetiredToken{}, next.RetiredTokens...), token)
	if err := r.saveLocked(next); err != nil {
		r.pendingRetired = appendRetiredToken(r.pendingRetired, token)
	}
}

// A canceled request still compensates external mutations. If cleanup cannot
// complete, retain the token identifier so Disconnect can retry it later.
func (r *r2OAuthRuntime) compensateCredentialLocked(accessToken string, token r2RetiredToken) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := r.client.DeleteToken(ctx, accessToken, token.AccountID, token.TokenID)
	if err == nil && token.CredentialKey != "" {
		err = r.secrets.Delete(token.CredentialKey)
	}
	if err != nil {
		r.rememberRetiredLocked(token)
		r.lastError = "An unused R2 token could not be removed. Disconnect Cloudflare to retry cleanup."
	}
}

func (r *r2OAuthRuntime) cleanupRetiredLocked(ctx context.Context, accessToken string) error {
	if len(r.pendingRetired) > 0 {
		if err := r.saveLocked(r.state); err != nil {
			return err
		}
	}
	for len(r.state.RetiredTokens) > 0 {
		token := r.state.RetiredTokens[0]
		if err := r.client.DeleteToken(ctx, accessToken, token.AccountID, token.TokenID); err != nil {
			return err
		}
		if token.CredentialKey != "" {
			if err := r.secrets.Delete(token.CredentialKey); err != nil {
				return err
			}
		}
		next := r.state
		next.RetiredTokens = append([]r2RetiredToken{}, next.RetiredTokens[1:]...)
		if err := r.saveLocked(next); err != nil {
			return err
		}
	}
	return nil
}

func (r *r2OAuthRuntime) credentialsLocked(ctx context.Context, config CloudSyncConfig) (cloudflare.Credentials, error) {
	var credentials cloudflare.Credentials
	if r.loadError != nil {
		return credentials, r.loadError
	}
	index := -1
	for i, c := range r.state.Connections {
		if c.ID == config.R2ConnectionID && c.AccountID == config.AccountID && c.Bucket == config.Bucket && c.Jurisdiction == config.Jurisdiction {
			index = i
			break
		}
	}
	if index < 0 {
		return credentials, errors.New("Choose a Cloudflare bucket before saving or syncing.")
	}
	connection := r.state.Connections[index]
	data, err := r.secrets.Load(r2ConnectionKey(connection))
	if err != nil {
		return credentials, err
	}
	err = json.Unmarshal(data, &credentials)
	clear(data)
	if err != nil || credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" || credentials.TokenID != connection.TokenID || credentials.ExpiresAt.IsZero() {
		return cloudflare.Credentials{}, errors.New("Saved R2 credentials are invalid. Reconnect the bucket.")
	}
	remaining := time.Until(credentials.ExpiresAt)
	if remaining > r2ExpiryMargin && (!r.state.Renewable || remaining > r2SilentRenewalWindow || time.Now().Before(r.renewRetryAt)) {
		return credentials, nil
	}
	renewed, err := r.renewCredentialsLocked(ctx, config, index, connection)
	if err != nil && remaining > r2ExpiryMargin {
		// Early renewal is best effort; current credentials keep sync working.
		r.renewRetryAt = time.Now().Add(r2RenewalRetryDelay)
		return credentials, nil
	}
	if err == nil {
		r.renewRetryAt = time.Time{}
	}
	return renewed, err
}

func (r *r2OAuthRuntime) renewCredentialsLocked(ctx context.Context, config CloudSyncConfig, index int, connection r2Connection) (cloudflare.Credentials, error) {
	tokens, err := r.tokensLocked(ctx)
	if err != nil {
		return cloudflare.Credentials{}, err
	}
	credentials, err := r.client.CreateR2Credentials(ctx, tokens.AccessToken, config.AccountID, config.Jurisdiction, config.Bucket, "racomi-"+config.R2ConnectionID[:12], time.Now().Add(r2CredentialLifetime))
	if err != nil {
		return cloudflare.Credentials{}, err
	}
	key := "connection-" + connection.ID + "-" + credentials.TokenID
	data, err := json.Marshal(credentials)
	if err == nil {
		err = r.secrets.Save(key, data)
	}
	clear(data)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		next := r.state
		next.Connections = append([]r2Connection{}, next.Connections...)
		next.Connections[index].TokenID, next.Connections[index].ExpiresAt, next.Connections[index].CredentialKey = credentials.TokenID, credentials.ExpiresAt, key
		next.RetiredTokens = appendRetiredToken(append([]r2RetiredToken{}, next.RetiredTokens...), r2RetiredToken{AccountID: connection.AccountID, TokenID: connection.TokenID, CredentialKey: r2ConnectionKey(connection)})
		err = r.saveLocked(next)
	}
	if err != nil {
		r.compensateCredentialLocked(tokens.AccessToken, r2RetiredToken{AccountID: config.AccountID, TokenID: credentials.TokenID, CredentialKey: key})
		return cloudflare.Credentials{}, err
	}
	if err = r.cleanupRetiredLocked(ctx, tokens.AccessToken); err != nil {
		r.lastError = "R2 credentials were renewed, but an older expiring token could not be removed. Disconnect Cloudflare to retry cleanup."
	}
	return credentials, nil
}

func (s *Service) r2ConfigForUse(ctx context.Context, config CloudSyncConfig) (CloudSyncConfig, error) {
	if config.Provider != "r2-oauth" {
		return config, nil
	}
	endpoint, err := cloudflare.R2Endpoint(config.AccountID, config.Jurisdiction)
	if err != nil {
		return config, err
	}
	if config.Endpoint != endpoint || config.Region != "auto" || !config.PathStyle {
		return config, errors.New("R2 endpoint and addressing must match the selected Cloudflare bucket.")
	}
	if s.r2 == nil {
		return config, errors.New("Cloudflare connection is unavailable.")
	}
	s.r2.mu.Lock()
	defer s.r2.mu.Unlock()
	credentials, err := s.r2.credentialsLocked(ctx, config)
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
	r.mu.Lock()
	if r.loadError != nil {
		err := r.loadError
		r.mu.Unlock()
		return s.GetR2OAuthStatus(), err
	}
	if r.state.Authorized && !r.authorizationRevoked {
		tokens, err := r.tokensLocked(ctx)
		if err != nil {
			r.mu.Unlock()
			return s.GetR2OAuthStatus(), err
		}
		for len(r.state.Connections) > 0 {
			connection := r.state.Connections[0]
			if err = r.client.DeleteToken(ctx, tokens.AccessToken, connection.AccountID, connection.TokenID); err != nil {
				r.mu.Unlock()
				return s.GetR2OAuthStatus(), fmt.Errorf("Could not remove a racomi R2 token; authorization was kept so cleanup can be retried: %w", err)
			}
			// Keep the record until both remote and local deletion are complete.
			// A metadata failure is safe to retry: both deletions are idempotent.
			if err = r.secrets.Delete(r2ConnectionKey(connection)); err != nil {
				r.mu.Unlock()
				return s.GetR2OAuthStatus(), err
			}
			next := r.state
			next.Connections = append([]r2Connection{}, next.Connections[1:]...)
			if err = r.saveLocked(next); err != nil {
				r.mu.Unlock()
				return s.GetR2OAuthStatus(), err
			}
		}
		if err = r.cleanupRetiredLocked(ctx, tokens.AccessToken); err != nil {
			r.mu.Unlock()
			return s.GetR2OAuthStatus(), fmt.Errorf("Could not remove an older racomi R2 token; authorization was kept so cleanup can be retried: %w", err)
		}
		if err = r.cleanupOAuthKeysLocked(); err != nil {
			r.mu.Unlock()
			return s.GetR2OAuthStatus(), err
		}
		revocationToken := tokens.RefreshToken
		if revocationToken == "" {
			revocationToken = tokens.AccessToken
		}
		if err = r.client.Revoke(ctx, revocationToken); err != nil {
			r.mu.Unlock()
			return s.GetR2OAuthStatus(), err
		}
		r.authorizationRevoked = true
	}
	if len(r.state.Connections) > 0 || len(r.state.RetiredTokens) > 0 || len(r.pendingRetired) > 0 {
		r.mu.Unlock()
		return s.GetR2OAuthStatus(), errors.New("Reconnect Cloudflare to remove the remaining racomi R2 tokens.")
	}
	// If remote revocation succeeded but a local write failed, retries complete
	// locally instead of trying to refresh an already revoked refresh token.
	next := r.state
	next.Authorized = false
	err := r.saveLocked(next)
	if err == nil {
		err = r.cleanupOAuthKeysLocked()
	}
	if err == nil {
		err = r.secrets.Delete(r.oauthKey())
	}
	if err == nil {
		r.pendingTokens = nil
		r.authorizationRevoked = false
		r.lastError = ""
	}
	r.mu.Unlock()
	if err != nil {
		return s.GetR2OAuthStatus(), err
	}
	c.mu.Lock()
	r2 := defaultCloudSyncConfig()
	r2.Provider = "r2-oauth"
	previousR2 := c.r2Config
	c.r2Config = r2
	config, lastSyncAt := c.config, c.lastSyncAt
	if config.Provider == "r2-oauth" {
		config, lastSyncAt = r2, ""
	}
	err = c.persistLocked(config, lastSyncAt)
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
