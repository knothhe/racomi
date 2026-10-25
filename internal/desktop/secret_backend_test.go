package desktop

import (
	"bytes"
	"sync"
)

// Services created by tests keep credentials in memory, never in the user's
// credential store.
func init() { defaultSecretBackend = &lockedMemorySecretBackend{items: map[string][]byte{}} }

type lockedMemorySecretBackend struct {
	mu    sync.Mutex
	items map[string][]byte
}

func (b *lockedMemorySecretBackend) load(service, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	value, ok := b.items[service+"/"+key]
	if !ok {
		return nil, errR2SecretNotFound
	}
	return bytes.Clone(value), nil
}

func (b *lockedMemorySecretBackend) save(service, key string, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items[service+"/"+key] = bytes.Clone(value)
	return nil
}

func (b *lockedMemorySecretBackend) delete(service, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.items[service+"/"+key]; !ok {
		return errR2SecretNotFound
	}
	delete(b.items, service+"/"+key)
	return nil
}
