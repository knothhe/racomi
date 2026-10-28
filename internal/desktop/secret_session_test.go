package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/knothhe/r2connect/keyring"
)

func TestSecretSessionConcurrentReadsAndInvalidation(t *testing.T) {
	native := &memoryR2SecretBackend{items: map[string][]byte{"service/key": []byte("first")}}
	state := filepath.Join(t.TempDir(), "state.json")
	backend := &secretSessionBackend{backend: native, statePath: state}
	store := keyring.NewWithBackend("service", backend)
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			value, err := store.Load("key")
			if err != nil || string(value) != "first" {
				t.Errorf("load: %q, %v", value, err)
			}
			clear(value) // Callers cannot erase the session's copy.
		}()
	}
	workers.Wait()
	if native.calls != 1 {
		t.Fatalf("concurrent reads reached native storage %d times", native.calls)
	}
	if err := store.Save("key", []byte("rotated")); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load("key")
	if err != nil || string(value) != "rotated" || native.calls != 3 {
		t.Fatal("rotation did not force native readback")
	}
	native.items["service/key"] = []byte("external")
	if err := os.WriteFile(state, []byte("changed metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err = store.Load("key")
	if err != nil || string(value) != "external" || native.calls != 4 {
		t.Fatal("external SDK state change did not invalidate cached credentials")
	}
	if err := store.Delete("key"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("key"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := store.Load("key"); !errors.Is(err, keyring.ErrNotFound) || native.calls != 7 {
		t.Fatal("missing secrets were cached")
	}
}

func TestSecretSessionDenialBlocksCachedKeys(t *testing.T) {
	native := &memoryR2SecretBackend{items: map[string][]byte{"service/key": []byte("synthetic")}}
	backend := &secretSessionBackend{backend: native, statePath: filepath.Join(t.TempDir(), "state.json")}
	store := &r2SystemSecretStore{Store: keyring.NewWithBackend("service", backend), session: backend}
	if _, err := store.Load("key"); err != nil {
		t.Fatal(err)
	}
	cached := backend.items["service"]["key"]
	native.err = keyring.ErrAccessDenied
	if _, err := store.Load("another"); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatal(err)
	}
	for _, v := range cached {
		if v != 0 {
			t.Fatal("cached bytes were not erased")
		}
	}
	native.err = nil
	if _, err := store.Load("key"); !errors.Is(err, keyring.ErrAccessDenied) || native.calls != 2 {
		t.Fatal("cached credentials bypassed denial")
	}
	store.retryAccess()
	if _, err := store.Load("key"); err != nil || native.calls != 3 {
		t.Fatal("explicit retry did not read native storage")
	}
	store.clearSession()
	if _, err := store.Load("key"); err != nil || native.calls != 4 {
		t.Fatal("session clear did not force native read")
	}
	// Reconnecting also drops a successful session when no denial occurred.
	native.items["service/key"] = []byte("reconnected")
	store.retryAccess()
	value, err := store.Load("key")
	if err != nil || string(value) != "reconnected" || native.calls != 5 {
		t.Fatal("explicit reconnect reused the previous successful session")
	}
}

func TestSecretSessionIsolationAndUnreadableMetadata(t *testing.T) {
	native := &memoryR2SecretBackend{items: map[string][]byte{
		"dev/key": []byte("dev"), "release/key": []byte("release"),
	}}
	state := filepath.Join(t.TempDir(), "state")
	backend := &secretSessionBackend{backend: native, statePath: state}
	for _, service := range []string{"dev", "release", "dev"} {
		value, err := backend.Load(service, "key")
		if err != nil || string(value) != service {
			t.Fatalf("service isolation failed: %q, %v", value, err)
		}
	}
	if native.calls != 2 {
		t.Fatal("isolated service did not reuse successful read")
	}
	// A directory is unreadable as metadata even when tests run as root.
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := backend.Load("dev", "key"); err != nil {
			t.Fatal(err)
		}
	}
	if native.calls != 4 || len(backend.items) != 0 {
		t.Fatal("unreadable metadata allowed stale session reuse")
	}
}
