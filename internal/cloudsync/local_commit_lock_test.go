package cloudsync

import (
	"strings"
	"sync"
	"testing"
	"time"
)

type observedCloudCommitLock struct {
	sync.Mutex
	attempted chan struct{}
}

func (l *observedCloudCommitLock) Lock() {
	select {
	case l.attempted <- struct{}{}:
	default:
	}
	l.Mutex.Lock()
}

func TestCloudLocalCommitWaitsForCompoundApplicationEdit(t *testing.T) {
	for _, mode := range []string{"download", "upload", "restore"} {
		t.Run(mode, func(t *testing.T) {
			_, config := newFakeS3(t)
			remote, local := openStore(t), openStore(t)
			savePreset(t, remote, "", "Remote preset")
			first := syncOK(t, remote, config)
			if mode == "upload" {
				savePreset(t, local, "", "Local preset before sync")
			} else if mode == "restore" {
				syncOK(t, local, config)
				savePreset(t, remote, "", "Newer remote preset")
				syncOK(t, remote, config)
			}

			// A service mutation has read its baseline and owns its application
			// lock, but has not yet saved. Cloud must not apply between those calls.
			before := presetNames(t, local)
			lock := &observedCloudCommitLock{attempted: make(chan struct{}, 1)}
			lock.Mutex.Lock()
			var once sync.Once
			unlock := func() { once.Do(lock.Mutex.Unlock) }
			done := make(chan error, 1)
			finished := make(chan struct{})
			t.Cleanup(func() {
				unlock()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("cloud operation did not finish after local lock release")
				}
			})
			go func() {
				defer close(finished)
				var err error
				if mode == "restore" {
					_, err = RestoreBackupWithLocalLock(t.Context(), local, config, first.Revision, lock)
				} else {
					_, err = SyncWithLocalLock(t.Context(), local, config, nil, lock)
				}
				done <- err
			}()
			select {
			case <-lock.attempted:
			case err := <-done:
				t.Fatalf("cloud operation skipped the application's local commit lock: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("cloud operation did not reach its local commit")
			}
			if presetNames(t, local) != before {
				t.Fatal("cloud state was applied while a compound application edit held its lock")
			}
			savePreset(t, local, "", "Edit completed while cloud commit waited")
			unlock()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "local") {
					t.Fatalf("concurrent application edit was not detected: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cloud local commit did not resume")
			}
			if !strings.Contains(presetNames(t, local), "Edit completed while cloud commit waited") {
				t.Fatal("cloud commit overwrote the compound application edit")
			}
			syncOK(t, local, config)
			if !strings.Contains(presetNames(t, local), "Remote preset") || !strings.Contains(presetNames(t, local), "Edit completed while cloud commit waited") {
				t.Fatal("retry did not merge the remote state with the preserved application edit")
			}
		})
	}
}
