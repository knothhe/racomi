package cloudsync

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDeletionOfUnsyncedPresetIsPreservedOnRetry(t *testing.T) {
	fake, config := newFakeS3(t)
	store := openStore(t)
	syncOK(t, store, config)
	preset := savePreset(t, store, "", "Delete during commit")
	var localLock sync.Mutex
	deleted := make(chan error, 1)
	fake.mu.Lock()
	fake.beforeHeadPut = func() {
		// An editor takes the same application mutation lock as the sync.
		// If it is free during the remote commit, perform the deletion now
		// to expose the stale-checkpoint resurrection on the next retry.
		if localLock.TryLock() {
			deleted <- store.DeletePreset(preset.ID)
			localLock.Unlock()
			return
		}
		go func() {
			localLock.Lock()
			defer localLock.Unlock()
			deleted <- store.DeletePreset(preset.ID)
		}()
	}
	fake.mu.Unlock()
	if _, err := SyncWithLocalLock(context.Background(), store, config, nil, &localLock); err != nil {
		t.Fatalf("sync failed before the queued local edit could run: %v", err)
	}
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatalf("delete preset: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local editor remained blocked after synchronization")
	}
	if names := presetNames(t, store); names != "" {
		t.Fatalf("deleted preset exists before retry: %s", names)
	}
	syncOK(t, store, config)
	if names := presetNames(t, store); names != "" {
		t.Fatalf("retry resurrected deleted preset: %s", names)
	}
}
