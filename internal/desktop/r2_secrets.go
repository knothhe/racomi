package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"unicode/utf8"
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
	errR2SecretNotFound     = errors.New("Cloudflare credentials are not saved on this device")
	errR2SecretUnavailable  = errors.New("the operating-system credential store is unavailable on this platform")
	errR2SecretAccessDenied = errors.New("Cloudflare credential access was denied. Automatic retries have stopped. Connect Cloudflare again to retry.")
)

const maxR2SecretBytes = 64 << 10

type r2SecretBackend interface {
	load(service, key string) ([]byte, error)
	save(service, key string, value []byte) error
	delete(service, key string) error
}

type r2SystemSecretStore struct {
	mu             sync.Mutex
	service        string
	backend        r2SecretBackend
	accessDenied   bool
	onAccessDenied func()
}

// A native denial applies to the whole store, including cleanup writes and
// deletes. Serialize native calls so queued requests cannot reopen the prompt
// after the first request has already been denied.
func (s *r2SystemSecretStore) access(operation func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accessDenied {
		return errR2SecretAccessDenied
	}
	err := operation()
	if errors.Is(err, errR2SecretAccessDenied) {
		s.accessDenied = true
		if s.onAccessDenied != nil {
			s.onAccessDenied()
		}
		return errR2SecretAccessDenied
	}
	return err
}

// Only an explicit Connect Cloudflare action may reopen access. Polling,
// background sync, compensation and status reads must never call this method.
func (s *r2SystemSecretStore) retryAccess() {
	s.mu.Lock()
	s.accessDenied = false
	s.mu.Unlock()
}

// defaultSecretBackend is replaced in tests so they never reach the user's
// credential store.
var defaultSecretBackend r2SecretBackend = r2NativeSecretBackend{}

func newR2SecretStore(namespace string) r2SecretStore {
	return &r2SystemSecretStore{service: r2SecretService(namespace), backend: defaultSecretBackend}
}

// secretStoreUnusable reports that this device cannot keep secrets in its
// credential store, as opposed to a transient failure.
func secretStoreUnusable(err error) bool {
	return errors.Is(err, errR2SecretUnavailable) || errors.Is(err, errR2SecretAccessDenied) || errors.Is(err, errR2SecretStoreMissing)
}

func r2SecretService(namespace string) string {
	// The full data-directory identity keeps Dev, production and isolated
	// libraries independent without putting the user's filesystem path in the
	// system keychain's service name.
	sum := sha256.Sum256([]byte(namespace))
	return "com.knothhe.racomi.r2-oauth." + hex.EncodeToString(sum[:])
}

func validR2SecretKey(key string) bool {
	return key != "" && len(key) <= 128 && utf8.ValidString(key) && !strings.ContainsRune(key, '\x00')
}

func (s *r2SystemSecretStore) Load(key string) ([]byte, error) {
	if !validR2SecretKey(key) {
		return nil, errors.New("invalid Cloudflare credential identifier")
	}
	var value []byte
	err := s.access(func() error {
		var err error
		value, err = s.backend.load(s.service, key)
		return err
	})
	if err != nil {
		clear(value)
		return nil, err
	}
	if len(value) == 0 || len(value) > maxR2SecretBytes {
		clear(value)
		return nil, errors.New("invalid saved Cloudflare credentials")
	}
	return value, nil
}

func (s *r2SystemSecretStore) Save(key string, value []byte) error {
	if !validR2SecretKey(key) {
		return errors.New("invalid Cloudflare credential identifier")
	}
	if len(value) == 0 || len(value) > maxR2SecretBytes {
		return errors.New("Cloudflare credentials must contain between 1 and 65536 bytes")
	}
	return s.access(func() error { return s.backend.save(s.service, key, value) })
}

func (s *r2SystemSecretStore) Delete(key string) error {
	if !validR2SecretKey(key) {
		return errors.New("invalid Cloudflare credential identifier")
	}
	err := s.access(func() error { return s.backend.delete(s.service, key) })
	if errors.Is(err, errR2SecretNotFound) {
		return nil
	}
	return err
}
