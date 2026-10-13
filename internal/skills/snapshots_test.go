package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeDatedSnapshot(t *testing.T, store *Store, name string, created time.Time) {
	t.Helper()
	saved := snapshotFile{Info: SnapshotInfo{ID: name, CreatedAt: created.Format(time.RFC3339Nano)}, State: state(t, store)}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(filepath.Join(store.Root(), "snapshots", name+".json"), data); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotRetentionDefaultsAndBothLimits(t *testing.T) {
	store, _ := newStore(t)
	settings := state(t, store).Settings
	if settings.SnapshotRetentionDays != 7 || settings.SnapshotMaxCount != 100 {
		t.Fatal("missing default retention", settings)
	}
	now := time.Now().UTC()
	for i := 0; i < 103; i++ {
		writeDatedSnapshot(t, store, fmt.Sprintf("snapshot-%03d", i), now.Add(-time.Duration(i)*time.Minute))
	}
	writeDatedSnapshot(t, store, "snapshot-expired", now.Add(-8*24*time.Hour))
	manual, err := store.Snapshot("manual")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Root(), "snapshots"))
	if err != nil || len(entries) != 100 {
		t.Fatalf("creation did not enforce count: %d, %v", len(entries), err)
	}
	list, err := store.Snapshots()
	if err != nil || len(list) != 100 || list[0].ID != manual.ID {
		t.Fatalf("newest snapshots not retained: %+v, %v", list, err)
	}
	missing(t, filepath.Join(store.Root(), "snapshots", "snapshot-expired.json"))
	settings.SnapshotRetentionDays, settings.SnapshotMaxCount = 1, 2
	if err = store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if list, err = store.Snapshots(); err != nil || len(list) != 2 {
		t.Fatalf("settings did not prune immediately: %+v, %v", list, err)
	}
	if err = store.SaveSettings(Settings{DeploymentMode: DeploymentCopy, DefaultProjectDirectories: []string{"agents"}, DefaultGlobalDirectories: []string{"codex-user"}}); err != nil {
		t.Fatal(err)
	}
	if got := state(t, store).Settings; got.SnapshotRetentionDays != 1 || got.SnapshotMaxCount != 2 {
		t.Fatal("legacy client erased retention", got)
	}
}

func TestSnapshotRetentionAgeBoundaryAndStartup(t *testing.T) {
	store, _ := newStore(t)
	now := time.Now().UTC()
	writeDatedSnapshot(t, store, "snapshot-at-limit", now.Add(-7*24*time.Hour))
	writeDatedSnapshot(t, store, "snapshot-within-limit", now.Add(-7*24*time.Hour+time.Second))
	store.mu.Lock()
	current, err := store.readState()
	if err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	list, err := store.pruneSnapshots(current, now, true)
	store.mu.Unlock()
	if err != nil || len(list) != 1 || list[0].ID != "snapshot-within-limit" {
		t.Fatalf("wrong age boundary: %+v, %v", list, err)
	}
	writeDatedSnapshot(t, store, "snapshot-expired", now.Add(-8*24*time.Hour))
	if _, err = Open(store.Root()); err != nil {
		t.Fatal(err)
	}
	missing(t, filepath.Join(store.Root(), "snapshots", "snapshot-expired.json"))
}

func TestSnapshotCleanupReclaimsOnlyUnusedContentAndRestoreKeepsRecoveryPoint(t *testing.T) {
	for _, deployed := range []bool{false, true} {
		t.Run(fmt.Sprint("deployed=", deployed), func(t *testing.T) {
			store, base := newStore(t)
			a := install(t, store, candidate(t, base, "versioned", "first"))[0]
			if deployed {
				preset := savePreset(t, store, "preset", a)
				target := saveTarget(t, store, base, preset)
				mustApply(t, store, target)
			}
			b := install(t, store, candidate(t, base, "versioned", "second"))[0]
			c := install(t, store, candidate(t, base, "versioned", "third"))[0]
			settings := state(t, store).Settings
			settings.SnapshotMaxCount = 1
			if err := store.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			list, err := store.Snapshots()
			if err != nil || len(list) != 1 {
				t.Fatal(list, err)
			}
			for _, skill := range []Skill{b, c} {
				if _, err = os.Stat(filepath.Join(store.Root(), "objects", skill.ContentHash)); err != nil {
					t.Fatal("deleted live or retained snapshot content", err)
				}
			}
			if deployed {
				if _, err = os.Stat(filepath.Join(store.Root(), "objects", a.ContentHash)); err != nil {
					t.Fatal("deleted deployed content", err)
				}
			} else {
				missing(t, filepath.Join(store.Root(), "objects", a.ContentHash))
			}
			if err = store.RestoreSnapshot(list[0].ID); err != nil {
				t.Fatal("could not restore retained snapshot", err)
			}
			if got := state(t, store).Skills[0].ContentHash; got != b.ContentHash {
				t.Fatal("restored wrong version", got)
			}
			list, err = store.Snapshots()
			if err != nil || len(list) != 1 || list[0].Reason != "before restoring skills" {
				t.Fatal("restore omitted current recovery point", list, err)
			}
			if err = store.RestoreSnapshot(list[0].ID); err != nil || state(t, store).Skills[0].ContentHash != c.ContentHash {
				t.Fatal("could not undo restore", err)
			}
		})
	}
}

func TestSnapshotPruningDoesNotDeleteReusedStagedContent(t *testing.T) {
	store, base := newStore(t)
	settings := state(t, store).Settings
	settings.SnapshotMaxCount = 1
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	a := install(t, store, candidate(t, base, "reused", "first"))[0]
	install(t, store, candidate(t, base, "reused", "second"))
	install(t, store, candidate(t, base, "reused", "first"))
	if _, err := store.Snapshots(); err != nil {
		t.Fatal(err)
	}
	if hash, err := HashDir(filepath.Join(store.Root(), "objects", a.ContentHash)); err != nil || hash != a.ContentHash {
		t.Fatal("snapshot cleanup removed reused incoming content", hash, err)
	}
}

func TestDamagedSnapshotPreventsDeletion(t *testing.T) {
	store, _ := newStore(t)
	writeDatedSnapshot(t, store, "snapshot-expired", time.Now().Add(-8*24*time.Hour))
	path := filepath.Join(store.Root(), "snapshots", "snapshot-damaged.json")
	if err := os.WriteFile(path, []byte(`{"info":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshots(); err == nil {
		t.Fatal("damaged snapshot was ignored")
	}
	if _, err := os.Stat(filepath.Join(store.Root(), "snapshots", "snapshot-expired.json")); err != nil {
		t.Fatal("pruning ran before validating history", err)
	}
}

func TestContentCleanupWaitsForExternalTransactionsAcrossStoreHandles(t *testing.T) {
	store, base := newStore(t)
	settings := state(t, store).Settings
	settings.SnapshotMaxCount = 1
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	a := install(t, store, candidate(t, base, "staged", "first"))[0]
	install(t, store, candidate(t, base, "staged", "second"))
	release := store.SuspendContentCleanup()
	defer release()
	if _, err := store.Snapshot("prune old content"); err != nil {
		t.Fatal(err)
	}
	second, err := Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond := second.SuspendContentCleanup()
	defer releaseSecond()
	release()
	release() // Release is safe to call twice without ending another transaction.
	if _, err = store.Snapshots(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(store.Root(), "objects", a.ContentHash)); err != nil {
		t.Fatal("cleanup deleted content during an external transaction", err)
	}
	releaseSecond()
	if _, err = store.Snapshots(); err != nil {
		t.Fatal(err)
	}
	missing(t, filepath.Join(store.Root(), "objects", a.ContentHash))
}
