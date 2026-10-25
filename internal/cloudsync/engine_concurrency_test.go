package cloudsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"racomi/internal/skills"
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

func TestObjectTransfersRunInParallel(t *testing.T) {
	fake, config := newFakeS3(t)
	fake.objectDelay = 20 * time.Millisecond
	a, b := openStore(t), openStore(t)
	for i := range 2 * transferConcurrency {
		addSkill(t, a, fmt.Sprintf("parallel-%02d", i))
	}
	if result := syncOK(t, a, config); result.Uploaded != 2*transferConcurrency {
		t.Fatalf("uploaded=%d", result.Uploaded)
	}
	if peak := fake.maxInFlight.Swap(0); peak < 2 || peak > transferConcurrency {
		t.Fatalf("upload overlap=%d, want 2..%d", peak, transferConcurrency)
	}
	if result := syncOK(t, b, config); result.Downloaded != 2*transferConcurrency {
		t.Fatalf("downloaded=%d", result.Downloaded)
	}
	if peak := fake.maxInFlight.Load(); peak < 2 || peak > transferConcurrency {
		t.Fatalf("download overlap=%d, want 2..%d", peak, transferConcurrency)
	}
}

func TestFailedDownloadKeepsCompletedObjectsForRetry(t *testing.T) {
	fake, config := newFakeS3(t)
	a, b := openStore(t), openStore(t)
	var failing string
	for i := range 2 * transferConcurrency {
		failing = addSkill(t, a, fmt.Sprintf("retry-%02d", i)).ContentHash
	}
	syncOK(t, a, config)
	fake.mu.Lock()
	fake.failGet = failing
	fake.mu.Unlock()
	first, err := Sync(context.Background(), b, config, nil)
	if err == nil {
		t.Fatal("sync succeeded despite a failing object download")
	}
	if _, statErr := os.Lstat(filepath.Join(b.Root(), "objects", failing)); !os.IsNotExist(statErr) {
		t.Fatalf("failed object was installed: %v", statErr)
	}
	if skillNames(t, b) != 0 {
		t.Fatal("partial download was committed to the library")
	}
	fake.mu.Lock()
	fake.failGet = ""
	fake.mu.Unlock()
	second := syncOK(t, b, config)
	if first.Downloaded+second.Downloaded != 2*transferConcurrency || second.Downloaded == 2*transferConcurrency {
		t.Fatalf("downloaded first=%d retry=%d", first.Downloaded, second.Downloaded)
	}
	if skillNames(t, b) != 2*transferConcurrency {
		t.Fatalf("skills=%d after retry", skillNames(t, b))
	}
}

// The desktop scheduler resumes immediately after a deadline that made
// progress, so the deadline must surface unwrapped alongside the count.
func TestDeadlineReportsCompletedDownloads(t *testing.T) {
	fake, config := newFakeS3(t)
	a, b := openStore(t), openStore(t)
	for i := range 4 * transferConcurrency {
		addSkill(t, a, fmt.Sprintf("deadline-%02d", i))
	}
	syncOK(t, a, config)
	fake.mu.Lock()
	fake.stallGetsAfter = transferConcurrency
	fake.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := Sync(ctx, b, config, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want deadline exceeded", err)
	}
	if result.Downloaded != transferConcurrency {
		t.Fatalf("downloaded=%d before the deadline", result.Downloaded)
	}
}

func skillNames(t *testing.T, store *skills.Store) int {
	t.Helper()
	state, err := currentShared(store)
	if err != nil {
		t.Fatal(err)
	}
	return len(state.Skills)
}
