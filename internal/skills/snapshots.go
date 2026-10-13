package skills

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SuspendContentCleanup protects objects staged outside the store's index
// transaction, such as a cloud download or ZIP import. The returned release
// must run after the index commit (or failure). Handles for one root share it.
func (l *Store) SuspendContentCleanup() func() {
	l.mu.Lock()
	l.mu.contentHolds++
	l.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.mu.contentHolds--
			l.mu.Unlock()
		})
	}
}

// The caller holds the store lock. Read and validate every manifest before
// deleting anything, so damaged history cannot trigger content reclamation.
func (l *Store) pruneSnapshots(current State, now time.Time, reclaim bool) ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(filepath.Join(l.root, "snapshots"))
	if err != nil {
		return nil, err
	}
	type savedSnapshot struct {
		info    SnapshotInfo
		created time.Time
		objects map[string]bool
	}
	objectReferences := func(state State) map[string]bool {
		result := map[string]bool{}
		for _, skill := range state.Skills {
			result[skill.ContentHash] = true
		}
		for _, target := range state.Targets {
			for _, deployment := range target.Deployments {
				result[deployment.ContentHash] = true
			}
		}
		return result
	}
	saved := []savedSnapshot{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var file snapshotFile
		if err = readJSON(filepath.Join(l.root, "snapshots", entry.Name()), &file); err != nil {
			return nil, err
		}
		if file.Info.ID+".json" != entry.Name() || !safeName.MatchString(file.Info.ID) {
			return nil, errors.New("invalid snapshot identity")
		}
		created, parseErr := time.Parse(time.RFC3339Nano, file.Info.CreatedAt)
		if parseErr != nil {
			return nil, errors.New("invalid snapshot creation time")
		}
		file.State = migrateState(cloneState(file.State))
		if err = l.validate(file.State); err != nil {
			return nil, err
		}
		saved = append(saved, savedSnapshot{file.Info, created, objectReferences(file.State)})
	}
	sort.Slice(saved, func(i, j int) bool {
		if saved[i].created.Equal(saved[j].created) {
			return saved[i].info.ID > saved[j].info.ID
		}
		return saved[i].created.After(saved[j].created)
	})
	settings := normalizeSettings(current.Settings)
	cutoff := now.Add(-time.Duration(settings.SnapshotRetentionDays) * 24 * time.Hour)
	result := []SnapshotInfo{}
	referenced := objectReferences(current)
	for i, snapshot := range saved {
		if i >= settings.SnapshotMaxCount || !snapshot.created.After(cutoff) {
			if err = os.Remove(filepath.Join(l.root, "snapshots", snapshot.info.ID+".json")); err != nil {
				return nil, err
			}
			continue
		}
		result = append(result, snapshot.info)
		for hash := range snapshot.objects {
			referenced[hash] = true
		}
	}
	if reclaim && l.mu.contentHolds == 0 {
		objects, readErr := os.ReadDir(filepath.Join(l.root, "objects"))
		if readErr != nil {
			return nil, readErr
		}
		for _, object := range objects {
			if !object.IsDir() || !hashPattern.MatchString(object.Name()) || referenced[object.Name()] {
				continue
			}
			if err = os.RemoveAll(filepath.Join(l.root, "objects", object.Name())); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}
