package desktop

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/knothhe/r2connect/keyring"
)

type memoryR2SecretBackend struct {
	items map[string][]byte
	err   error
	calls int
}

func (b *memoryR2SecretBackend) Load(service, key string) ([]byte, error) {
	b.calls++
	if b.err != nil {
		return nil, b.err
	}
	value, ok := b.items[service+"/"+key]
	if !ok {
		return nil, errR2SecretNotFound
	}
	return bytes.Clone(value), nil
}

func (b *memoryR2SecretBackend) Save(service, key string, value []byte) error {
	b.calls++
	if b.err != nil {
		return b.err
	}
	b.items[service+"/"+key] = bytes.Clone(value)
	return nil
}

func (b *memoryR2SecretBackend) Delete(service, key string) error {
	b.calls++
	if b.err != nil {
		return b.err
	}
	if _, ok := b.items[service+"/"+key]; !ok {
		return errR2SecretNotFound
	}
	delete(b.items, service+"/"+key)
	return nil
}

func testR2SecretStore(service string, backend keyring.Backend) *r2SystemSecretStore {
	return &r2SystemSecretStore{keyring.NewWithBackend(service, backend)}
}

func TestR2SecretServiceHidesDataPath(t *testing.T) {
	service := r2SecretService("/isolated/production")
	if strings.Contains(service, "/isolated/") || len(service) > 128 {
		t.Fatal("credential service exposes the data path or exceeds the native identifier limit")
	}
	if r2SecretService("/isolated/production") != service || r2SecretService("/isolated/development") == service {
		t.Fatal("credential service does not follow the data identity")
	}
}

func TestR2SecretDenialKeepsTranslatedMessage(t *testing.T) {
	backend := &memoryR2SecretBackend{items: map[string][]byte{}, err: keyring.ErrAccessDenied}
	store := testR2SecretStore("isolated-denial", backend)
	_, err := store.Load("authorization")
	if err != errR2SecretAccessDenied || !errors.Is(err, keyring.ErrAccessDenied) || !secretStoreUnusable(err) {
		t.Fatalf("credential denial lost its translated message or classification: %v", err)
	}
	if err = store.Save("authorization", []byte("synthetic")); err != errR2SecretAccessDenied || backend.calls != 1 {
		t.Fatal("credential denial did not stop automatic access")
	}
}

func TestR2OnlyExplicitNewAuthorizationResetsCredentialDenial(t *testing.T) {
	s, _, backend, urls := r2TestService(t)
	store := s.r2.secrets.(*r2SystemSecretStore)
	backend.err = errR2SecretAccessDenied
	if _, err := store.Load("authorization"); !errors.Is(err, errR2SecretAccessDenied) {
		t.Fatal("expected native denial")
	}
	backend.err = nil
	_ = s.GetR2OAuthStatus()
	_ = s.GetCloudSyncStatus()
	if _, err := store.Load("authorization"); !errors.Is(err, errR2SecretAccessDenied) || backend.calls != 1 {
		t.Fatal("status polling reset credential denial")
	}
	beginR2Authorization(t, s, urls)
	if _, err := store.Load("authorization"); !errors.Is(err, errR2SecretNotFound) || backend.calls != 2 {
		t.Fatal("explicit authorization did not allow a new credential attempt")
	}
	backend.err = errR2SecretAccessDenied
	_, _ = store.Load("authorization")
	if _, err := s.StartR2OAuth(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("authorization"); !errors.Is(err, errR2SecretAccessDenied) || backend.calls != 3 {
		t.Fatal("an already active authorization reset credential denial")
	}
}
