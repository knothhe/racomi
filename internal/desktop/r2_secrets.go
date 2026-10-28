package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/knothhe/r2connect/keyring"
)

// OAuth credentials are intentionally separate from the portable library and
// the JSON cloud-sync configuration. Tests inject a store instead of accessing
// the user's operating-system credential store.
type r2SecretStore interface {
	Load(key string) ([]byte, error)
	Save(key string, value []byte) error
	Delete(key string) error
}

var (
	errR2SecretNotFound    = keyring.ErrNotFound
	errR2SecretUnavailable = keyring.ErrUnavailable
	// The frontend translates this exact message; it still matches
	// keyring.ErrAccessDenied.
	errR2SecretAccessDenied error = r2SecretDenial{}
)

type r2SecretDenial struct{}

func (r2SecretDenial) Error() string {
	return "Cloudflare credential access was denied. Automatic retries have stopped. Connect Cloudflare again to retry."
}

func (r2SecretDenial) Unwrap() error { return keyring.ErrAccessDenied }

func r2SecretError(err error) error {
	if errors.Is(err, keyring.ErrAccessDenied) {
		return errR2SecretAccessDenied
	}
	return err
}

// r2SystemSecretStore is the system credential store shared by Cloudflare
// authorization and the sync profiles, including its access-denied state.
type r2SystemSecretStore struct {
	*keyring.Store
	session *secretSessionBackend
}

// defaultSecretBackend is replaced in tests so they never reach the user's
// credential store.
var defaultSecretBackend = keyring.Native("racomi credentials")

func newR2SecretStore(namespace string) r2SecretStore {
	session := &secretSessionBackend{backend: defaultSecretBackend, statePath: filepath.Join(namespace, "r2-oauth-state.json")}
	return &r2SystemSecretStore{Store: keyring.NewWithBackend(r2SecretService(namespace), session), session: session}
}

func (s *r2SystemSecretStore) Load(key string) ([]byte, error) {
	value, err := s.Store.Load(key)
	return value, r2SecretError(err)
}

func (s *r2SystemSecretStore) Save(key string, value []byte) error {
	return r2SecretError(s.Store.Save(key, value))
}

func (s *r2SystemSecretStore) Delete(key string) error {
	return r2SecretError(s.Store.Delete(key))
}

func (s *r2SystemSecretStore) clearSession() {
	if s.session != nil {
		s.session.clear()
	}
}

// Only an explicit connection, disconnection or manual-profile retry may reopen
// access. Polling, background sync and compensation must never call this method.
func (s *r2SystemSecretStore) retryAccess() {
	s.RetryAccess()
	// RetryAccess waits for any in-flight Store operation. Clear afterwards so
	// that operation cannot repopulate the old session while we wait for it.
	s.clearSession()
}

// secretStoreUnusable reports that this device cannot keep secrets in its
// credential store, as opposed to a transient failure.
func secretStoreUnusable(err error) bool { return keyring.Unusable(err) }

func r2SecretService(namespace string) string {
	// The full data-directory identity keeps Dev, production and isolated
	// libraries independent without putting the user's filesystem path in the
	// system keychain's service name.
	sum := sha256.Sum256([]byte(namespace))
	return "com.knothhe.racomi.r2-oauth." + hex.EncodeToString(sum[:])
}
