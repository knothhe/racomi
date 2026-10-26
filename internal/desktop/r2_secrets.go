package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

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
type r2SystemSecretStore struct{ *keyring.Store }

// defaultSecretBackend is replaced in tests so they never reach the user's
// credential store.
var defaultSecretBackend = keyring.Native("racomi credentials")

func newR2SecretStore(namespace string) r2SecretStore {
	return &r2SystemSecretStore{keyring.NewWithBackend(r2SecretService(namespace), defaultSecretBackend)}
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

// Only an explicit Connect Cloudflare action may reopen access. Polling,
// background sync, compensation and status reads must never call this method.
func (s *r2SystemSecretStore) retryAccess() { s.RetryAccess() }

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
