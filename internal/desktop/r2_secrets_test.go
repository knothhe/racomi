package desktop

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

type memoryR2SecretBackend struct {
	items map[string][]byte
	err   error
	calls int
}

func (b *memoryR2SecretBackend) load(service, key string) ([]byte, error) {
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

func (b *memoryR2SecretBackend) save(service, key string, value []byte) error {
	b.calls++
	if b.err != nil {
		return b.err
	}
	b.items[service+"/"+key] = bytes.Clone(value)
	return nil
}

func (b *memoryR2SecretBackend) delete(service, key string) error {
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

func TestR2SecretStoreIsolationAndLifecycle(t *testing.T) {
	backend := &memoryR2SecretBackend{items: make(map[string][]byte)}
	production := &r2SystemSecretStore{service: r2SecretService("/isolated/production"), backend: backend}
	development := &r2SystemSecretStore{service: r2SecretService("/isolated/development"), backend: backend}
	if strings.Contains(production.service, "/isolated/") || len(production.service) > 128 {
		t.Fatal("credential service exposes the data path or exceeds the native identifier limit")
	}
	if r2SecretService("/isolated/production") != production.service {
		t.Fatal("credential service changed for the same data identity")
	}
	secret := []byte("synthetic OAuth credential")
	if err := production.Save("authorization", secret); err != nil {
		t.Fatal(err)
	}
	clear(secret)
	if _, err := development.Load("authorization"); !errors.Is(err, errR2SecretNotFound) {
		t.Fatal("different libraries shared credentials")
	}
	loaded, err := production.Load("authorization")
	if err != nil || string(loaded) != "synthetic OAuth credential" {
		t.Fatal("saved credentials did not round-trip")
	}
	clear(loaded)
	loaded, err = production.Load("authorization")
	if err != nil || string(loaded) != "synthetic OAuth credential" {
		t.Fatal("mutating a read buffer changed the saved credential")
	}
	if err := production.Delete("authorization"); err != nil {
		t.Fatal(err)
	}
	if err := production.Delete("authorization"); err != nil {
		t.Fatal("deleting an already absent credential should be idempotent")
	}
	if _, err := production.Load("authorization"); !errors.Is(err, errR2SecretNotFound) {
		t.Fatal("deleted credentials were still available")
	}
}

func TestR2SecretStoreValidatesBeforeNativeAccess(t *testing.T) {
	backend := &memoryR2SecretBackend{items: make(map[string][]byte)}
	store := &r2SystemSecretStore{service: r2SecretService("test"), backend: backend}
	for _, key := range []string{"", "bad\x00key", strings.Repeat("x", 129), string([]byte{0xff})} {
		if _, err := store.Load(key); err == nil {
			t.Fatal("invalid read identifier was accepted")
		}
		if err := store.Save(key, []byte("synthetic")); err == nil {
			t.Fatal("invalid write identifier was accepted")
		}
		if err := store.Delete(key); err == nil {
			t.Fatal("invalid delete identifier was accepted")
		}
	}
	for _, value := range [][]byte{nil, {}, make([]byte, maxR2SecretBytes+1)} {
		if err := store.Save("authorization", value); err == nil {
			t.Fatal("invalid credential size was accepted")
		}
	}
	if backend.calls != 0 {
		t.Fatal("invalid input reached the operating-system credential store")
	}
}

func TestR2SecretStorePropagatesNativeFailure(t *testing.T) {
	failure := errors.New("synthetic credential store failure")
	backend := &memoryR2SecretBackend{items: make(map[string][]byte), err: failure}
	store := &r2SystemSecretStore{service: r2SecretService("test"), backend: backend}
	if _, err := store.Load("authorization"); !errors.Is(err, failure) {
		t.Fatal("load failure was hidden")
	}
	if err := store.Save("authorization", []byte("synthetic")); !errors.Is(err, failure) {
		t.Fatal("save failure was hidden")
	}
	if err := store.Delete("authorization"); !errors.Is(err, failure) {
		t.Fatal("delete failure was hidden")
	}
	if len(backend.items) != 0 {
		t.Fatal("failed write persisted a credential")
	}
	backend.err = nil
	backend.items[store.service+"/authorization"] = make([]byte, maxR2SecretBytes+1)
	if _, err := store.Load("authorization"); err == nil {
		t.Fatal("oversized saved credentials were accepted")
	}
}

func TestR2SecretDenialStopsReadsWritesAndCleanupUntilExplicitRetry(t *testing.T) {
	for _, deniedOperation := range []string{"read", "save", "delete"} {
		t.Run(deniedOperation, func(t *testing.T) {
			backend := &memoryR2SecretBackend{items: map[string][]byte{}, err: errR2SecretAccessDenied}
			store := &r2SystemSecretStore{service: "isolated-denial-test", backend: backend}
			denials := 0
			store.onAccessDenied = func() { denials++ }
			operations := map[string]func() error{
				"read":   func() error { _, err := store.Load("authorization"); return err },
				"save":   func() error { return store.Save("new-authorization", []byte("synthetic")) },
				"delete": func() error { return store.Delete("cleanup") },
			}
			if err := operations[deniedOperation](); !errors.Is(err, errR2SecretAccessDenied) {
				t.Fatal("native refusal was not preserved")
			}
			backend.err = nil
			for i := 0; i < 10; i++ {
				for _, operation := range operations {
					if err := operation(); !errors.Is(err, errR2SecretAccessDenied) {
						t.Fatal("automatic access resumed after refusal")
					}
				}
			}
			if backend.calls != 1 || denials != 1 || len(backend.items) != 0 {
				t.Fatalf("denied access reached native store again: calls=%d notifications=%d", backend.calls, denials)
			}
			store.retryAccess()
			if err := store.Save("authorization", []byte("synthetic")); err != nil || backend.calls != 2 {
				t.Fatal("explicit retry did not restore access")
			}
		})
	}
}

func TestR2ConcurrentDeniedReadsOnlyPromptOnce(t *testing.T) {
	backend := &memoryR2SecretBackend{items: map[string][]byte{}, err: errR2SecretAccessDenied}
	store := &r2SystemSecretStore{service: "isolated-concurrent-denial", backend: backend}
	var requests sync.WaitGroup
	for i := 0; i < 20; i++ {
		requests.Go(func() {
			if _, err := store.Load("authorization"); !errors.Is(err, errR2SecretAccessDenied) {
				t.Error("concurrent request ignored the refusal")
			}
		})
	}
	requests.Wait()
	if backend.calls != 1 {
		t.Fatalf("concurrent requests opened %d native prompts", backend.calls)
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
