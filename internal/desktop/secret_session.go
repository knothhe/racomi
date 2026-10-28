package desktop

import (
	"bytes"
	"crypto/sha256"
	"os"
	"sync"

	"github.com/knothhe/r2connect/keyring"
)

// secretSessionBackend sits below keyring.Store so even cache hits remain
// subject to the SDK's store-wide access-denied latch and validation.
// It never caches writes: migration and rotation readbacks reach native storage.
type secretSessionBackend struct {
	mu         sync.Mutex
	backend    keyring.Backend
	statePath  string
	stateHash  [sha256.Size]byte
	stateKnown bool
	items      map[string]map[string][]byte
}

func (b *secretSessionBackend) clearLocked() {
	for _, items := range b.items {
		for _, value := range items {
			clear(value)
		}
	}
	b.items = nil
}

func (b *secretSessionBackend) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clearLocked()
}

// Compare public SDK metadata on each read. Failed reads invalidate the session
// too; direct edits to Keychain alone require reconnecting or restarting.
func (b *secretSessionBackend) checkState() bool {
	data, err := os.ReadFile(b.statePath)
	if err != nil && !os.IsNotExist(err) {
		b.clearLocked()
		b.stateKnown = false
		return false
	}
	hash := sha256.Sum256(data)
	if !b.stateKnown || hash != b.stateHash {
		b.clearLocked()
	}
	b.stateHash, b.stateKnown = hash, true
	return true
}

func (b *secretSessionBackend) Load(service, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cacheable := b.checkState()
	if value, ok := b.items[service][key]; ok {
		return bytes.Clone(value), nil
	}
	value, err := b.backend.Load(service, key)
	if err != nil {
		// Any failure drops successful earlier reads; errors are never cached.
		b.clearLocked()
		clear(value)
		return nil, err
	}
	if cacheable && len(value) > 0 && len(value) <= keyring.MaxValueBytes {
		if b.items == nil {
			b.items = make(map[string]map[string][]byte)
		}
		if b.items[service] == nil {
			b.items[service] = make(map[string][]byte)
		}
		b.items[service][key] = bytes.Clone(value)
	}
	return value, nil
}

func (b *secretSessionBackend) Save(service, key string, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clearLocked()
	return b.backend.Save(service, key, value)
}

func (b *secretSessionBackend) Delete(service, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clearLocked()
	return b.backend.Delete(service, key)
}
