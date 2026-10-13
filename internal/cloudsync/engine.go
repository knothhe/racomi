package cloudsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"racomi/internal/skills"
)

const formatVersion = 1
const maxManifestBytes = 32 << 20

var revisionPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var verifiedProviders sync.Map

type Result struct {
	Conflicts       []skills.SyncConflict `json:"conflicts"`
	Uploaded        int                   `json:"uploaded"`
	Downloaded      int                   `json:"downloaded"`
	Revision        string                `json:"revision"`
	LastSyncAt      string                `json:"lastSyncAt"`
	ResolutionToken string                `json:"resolutionToken,omitempty"`
}
type BackupInfo struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"createdAt"`
	SkillCount  int    `json:"skillCount"`
	PresetCount int    `json:"presetCount"`
}
type manifest struct {
	Version   int                `json:"version"`
	ID        string             `json:"id"`
	Parent    string             `json:"parent"`
	CreatedAt string             `json:"createdAt"`
	State     skills.SharedState `json:"state"`
}
type head struct {
	Version  int    `json:"version"`
	Revision string `json:"revision"`
}
type checkpoint struct {
	Version  int                `json:"version"`
	Revision string             `json:"revision"`
	State    skills.SharedState `json:"state"`
}
type session struct {
	remote *s3Client
	crypto *codec
	prefix string
}

func newSession(config Config) (*session, error) {
	remote, err := newClient(config)
	if err != nil {
		return nil, err
	}
	encryption, err := newCodec(remote.config)
	if err != nil {
		return nil, err
	}
	return &session{remote, encryption, remote.config.Prefix + "/v1/"}, nil
}
func (s *session) objectKey(hash string) string { return s.prefix + "objects/" + hash + ".enc" }
func (s *session) manifestKey(id string) string { return s.prefix + "manifests/" + id + ".enc" }
func (s *session) headKey() string              { return s.prefix + "head.enc" }
func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid sync manifest")
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return errors.New("unexpected trailing sync manifest data")
	}
	return nil
}
func (s *session) readManifest(ctx context.Context, id string) (manifest, error) {
	if !revisionPattern.MatchString(id) {
		return manifest{}, errors.New("invalid sync revision ID")
	}
	key := s.manifestKey(id)
	encrypted, _, err := s.remote.get(ctx, key, maxManifestBytes+1024)
	if err != nil {
		return manifest{}, err
	}
	plain, err := s.crypto.open(key, encrypted)
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	if err = decodeJSON(plain, &m); err != nil {
		return m, err
	}
	if m.Version != formatVersion || m.ID != id || m.Parent != "" && !revisionPattern.MatchString(m.Parent) {
		return m, errors.New("unsupported or invalid sync manifest")
	}
	if _, err = time.Parse(time.RFC3339Nano, m.CreatedAt); err != nil {
		return m, errors.New("invalid backup creation time")
	}
	if len(m.State.Skills) > 10000 {
		return m, errors.New("sync manifest contains too many skills")
	}
	for _, skill := range m.State.Skills {
		if !hashPattern.MatchString(skill.ContentHash) {
			return m, errors.New("invalid skill content hash in sync manifest")
		}
	}
	return m, nil
}
func (s *session) readHead(ctx context.Context) (manifest, string, error) {
	encrypted, etag, err := s.remote.get(ctx, s.headKey(), 4096)
	if statusIs(err, 404) {
		return manifest{State: skills.EmptyShared()}, "", nil
	}
	if err != nil {
		return manifest{}, "", err
	}
	if etag == "" {
		return manifest{}, "", errors.New("S3 server did not provide an ETag; safe synchronization is unavailable")
	}
	plain, err := s.crypto.open(s.headKey(), encrypted)
	if err != nil {
		return manifest{}, "", err
	}
	var h head
	if err = decodeJSON(plain, &h); err != nil {
		return manifest{}, "", err
	}
	if h.Version != formatVersion || !revisionPattern.MatchString(h.Revision) {
		return manifest{}, "", errors.New("unsupported sync head")
	}
	m, err := s.readManifest(ctx, h.Revision)
	return m, etag, err
}
func checkpointPath(store *skills.Store, config Config) string {
	return filepath.Join(store.Root(), "cloud-sync", sumHex([]byte(namespace(config)))+".json")
}
func safeCheckpointDir(store *skills.Store) error {
	dir := filepath.Join(store.Root(), "cloud-sync")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe cloud sync checkpoint directory")
	}
	return nil
}
func loadCheckpoint(store *skills.Store, config Config) (checkpoint, error) {
	result := checkpoint{Version: formatVersion, State: skills.EmptyShared()}
	if err := safeCheckpointDir(store); err != nil {
		return result, err
	}
	path := checkpointPath(store, config)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxManifestBytes {
		return result, errors.New("invalid local sync checkpoint")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	if err = decodeJSON(data, &result); err != nil {
		return result, err
	}
	if result.Version != formatVersion || result.Revision != "" && !revisionPattern.MatchString(result.Revision) {
		return result, errors.New("unsupported local sync checkpoint")
	}
	return result, nil
}
func saveCheckpoint(store *skills.Store, config Config, revision string, state skills.SharedState) error {
	data, err := json.Marshal(checkpoint{formatVersion, revision, state})
	if err != nil {
		return err
	}
	if err = safeCheckpointDir(store); err != nil {
		return err
	}
	path := checkpointPath(store, config)
	file, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}
func sharedEqual(a, b skills.SharedState) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func currentShared(store *skills.Store) (skills.SharedState, error) {
	state, err := store.State()
	if err != nil {
		return skills.SharedState{}, err
	}
	return skills.ExportShared(state), nil
}
func resolutionToken(base, local skills.SharedState, revision string) string {
	data, _ := json.Marshal(struct {
		Base, Local skills.SharedState
		Revision    string
	}{base, local, revision})
	return sumHex(data)
}

func (s *session) downloadObjects(ctx context.Context, store *skills.Store, state skills.SharedState) (int, error) {
	downloaded := 0
	seen := map[string]bool{}
	parent := filepath.Join(store.Root(), "objects")
	info, err := os.Lstat(parent)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("unsafe skill objects directory")
	}
	for _, skill := range state.Skills {
		hash := skill.ContentHash
		if !hashPattern.MatchString(hash) {
			return downloaded, errors.New("invalid skill content hash")
		}
		if seen[hash] {
			continue
		}
		seen[hash] = true
		destination := filepath.Join(parent, hash)
		if _, err := os.Lstat(destination); err == nil {
			actual, err := skills.HashDir(destination)
			if err != nil || actual != hash {
				return downloaded, errors.New("existing local skill object failed verification")
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return downloaded, err
		}
		key := s.objectKey(hash)
		encrypted, _, err := s.remote.get(ctx, key, maxArchiveBytes+1024)
		if err != nil {
			return downloaded, err
		}
		plain, err := s.crypto.open(key, encrypted)
		if err != nil {
			return downloaded, err
		}
		stage, err := os.MkdirTemp(parent, ".cloud-incoming-")
		if err != nil {
			return downloaded, err
		}
		err = extractObject(plain, stage, hash)
		if err == nil {
			err = os.Rename(stage, destination)
		}
		os.RemoveAll(stage)
		if err != nil {
			return downloaded, err
		}
		downloaded++
	}
	return downloaded, nil
}
func (s *session) uploadObjects(ctx context.Context, store *skills.Store, state skills.SharedState) (int, error) {
	uploaded := 0
	seen := map[string]bool{}
	for _, skill := range state.Skills {
		hash := skill.ContentHash
		if !hashPattern.MatchString(hash) {
			return uploaded, errors.New("invalid skill content hash")
		}
		if seen[hash] {
			continue
		}
		seen[hash] = true
		key := s.objectKey(hash)
		_, _, err := s.remote.request(ctx, "HEAD", key, nil, nil, nil, 4096)
		if err == nil {
			continue
		}
		if !statusIs(err, 404) {
			return uploaded, err
		}
		plain, err := archiveObject(filepath.Join(store.Root(), "objects", hash), hash)
		if err != nil {
			return uploaded, err
		}
		encrypted, err := s.crypto.seal(key, plain)
		if err != nil {
			return uploaded, err
		}
		_, err = s.remote.put(ctx, key, encrypted, "", true)
		if statusIs(err, 412) {
			// A competing create can have won, but a concurrent delete can
			// remove it again. Verify existence before committing references.
			if _, _, verifyErr := s.remote.request(ctx, "HEAD", key, nil, nil, nil, 4096); verifyErr != nil {
				return uploaded, errors.New("concurrently uploaded skill object could not be verified; retry synchronization")
			}
			continue
		}
		// S3's 409 ConditionalRequestConflict can be caused by a concurrent
		// deletion. It does not prove that the object exists, so abort rather
		// than publish a manifest with a potentially missing content object.
		if err != nil {
			return uploaded, err
		}
		uploaded++
	}
	return uploaded, nil
}
func (s *session) commit(ctx context.Context, state skills.SharedState, parent, etag string) (manifest, error) {
	id, err := randomID()
	if err != nil {
		return manifest{}, err
	}
	m := manifest{formatVersion, id, parent, time.Now().UTC().Format(time.RFC3339Nano), state}
	plain, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	if len(plain) > maxManifestBytes {
		return m, errors.New("sync manifest exceeds 32 MiB")
	}
	key := s.manifestKey(id)
	encrypted, err := s.crypto.seal(key, plain)
	if err != nil {
		return m, err
	}
	if _, err = s.remote.put(ctx, key, encrypted, "", true); err != nil {
		return m, err
	}
	plain, _ = json.Marshal(head{formatVersion, id})
	encrypted, err = s.crypto.seal(s.headKey(), plain)
	if err != nil {
		return m, err
	}
	_, err = s.remote.put(ctx, s.headKey(), encrypted, etag, etag == "")
	return m, err
}

// Sync merges against the last locally committed common state. Conflicts leave
// both indices unchanged. Resolution choices (local or remote) must include
// __token from the result that displayed those precise versions.
// The caller must serialize application mutations while Sync runs.
func Sync(ctx context.Context, store *skills.Store, config Config, resolutions map[string]string) (Result, error) {
	releaseContent := store.SuspendContentCleanup()
	defer releaseContent()
	result := Result{Conflicts: []skills.SyncConflict{}}
	s, err := newSession(config)
	if err != nil {
		return result, err
	}
	base, err := loadCheckpoint(store, config)
	if err != nil {
		return result, err
	}
	local, err := currentShared(store)
	if err != nil {
		return result, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		remote, etag, err := s.readHead(ctx)
		if err != nil {
			return result, err
		}
		if attempt == 0 {
			if err = s.verifyProvider(ctx); err != nil {
				return result, err
			}
		}
		if remote.ID == "" && base.Revision != "" {
			return result, errors.New("remote sync head is missing; refusing to resurrect a deleted sync space")
		}
		token := resolutionToken(base.State, local, remote.ID)
		choices := map[string]string{}
		for k, v := range resolutions {
			if k != "__token" {
				choices[k] = v
			}
		}
		if len(choices) > 0 && resolutions["__token"] != token {
			return result, errors.New("sync conflict versions changed; run synchronization again before resolving")
		}
		merged, conflicts, err := skills.MergeShared(base.State, local, remote.State, choices)
		if err != nil {
			return result, err
		}
		if len(conflicts) > 0 {
			result.Conflicts = conflicts
			result.Revision = remote.ID
			result.ResolutionToken = token
			return result, nil
		}
		count, err := s.downloadObjects(ctx, store, merged)
		result.Downloaded += count
		if err != nil {
			return result, err
		}
		if err = store.ValidateShared(merged); err != nil {
			return result, fmt.Errorf("validate synchronized library: %w", err)
		}
		current, err := currentShared(store)
		if err != nil {
			return result, err
		}
		if !sharedEqual(current, local) {
			return result, errors.New("local library changed during sync; retry synchronization")
		}
		if remote.ID != "" && sharedEqual(merged, remote.State) {
			if !sharedEqual(local, merged) {
				if err = store.ApplySharedIfCurrent(merged, local); err != nil {
					return result, err
				}
			}
			if err = saveCheckpoint(store, config, remote.ID, merged); err != nil {
				return result, err
			}
			result.Revision = remote.ID
			result.LastSyncAt = time.Now().UTC().Format(time.RFC3339Nano)
			return result, nil
		}
		count, err = s.uploadObjects(ctx, store, merged)
		result.Uploaded += count
		if err != nil {
			return result, err
		}
		current, err = currentShared(store)
		if err != nil {
			return result, err
		}
		if !sharedEqual(current, local) {
			return result, errors.New("local library changed during sync; retry synchronization")
		}
		committed, err := s.commit(ctx, merged, remote.ID, etag)
		if statusIs(err, 412, 409) {
			if len(choices) > 0 {
				return result, errors.New("remote library changed while resolving conflicts; run synchronization again")
			}
			continue
		}
		if err != nil {
			return result, err
		}
		if err = store.ApplySharedIfCurrent(merged, local); err != nil {
			return result, fmt.Errorf("remote sync committed but local application failed; retry synchronization: %w", err)
		}
		if err = saveCheckpoint(store, config, committed.ID, merged); err != nil {
			return result, err
		}
		result.Revision = committed.ID
		result.LastSyncAt = time.Now().UTC().Format(time.RFC3339Nano)
		return result, nil
	}
	return result, errors.New("remote library kept changing; retry synchronization")
}

// TestConnection verifies read/list/write/delete access and, critically, that
// the provider enforces both create-only and matching-ETag writes.
func TestConnection(ctx context.Context, config Config) error {
	s, err := newSession(config)
	if err != nil {
		return err
	}
	if _, _, err = s.readHead(ctx); err != nil {
		return err
	}
	return s.probe(ctx)
}

func (s *session) verifyProvider(ctx context.Context) error {
	c := s.remote.config
	identity := sumHex([]byte(namespace(c) + "\n" + c.Region + "\n" + c.AccessKeyID + "\n" + c.SecretAccessKey + "\n" + c.SessionToken + fmt.Sprint(c.PathStyle)))
	if _, ok := verifiedProviders.Load(identity); ok {
		return nil
	}
	if err := s.probe(ctx); err != nil {
		return err
	}
	verifiedProviders.Store(identity, true)
	return nil
}

func (s *session) probe(ctx context.Context) error {
	if _, err := s.remote.list(ctx, s.prefix+"probes/"); err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	key := s.prefix + "probes/" + id
	data := []byte("racomi S3 conditional-write probe")
	etag, err := s.remote.put(ctx, key, data, "", true)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, _ = s.remote.request(cleanup, "DELETE", key, nil, nil, nil, 4096)
	}()
	if etag == "" {
		return errors.New("S3 provider returned no ETag for conditional writes")
	}
	if _, err = s.remote.put(ctx, key, []byte("must not overwrite"), "", true); !statusIs(err, 412) {
		return errors.New("S3 provider does not enforce If-None-Match; safe synchronization is unavailable")
	}
	if _, err = s.remote.put(ctx, key, []byte("must not overwrite"), `"invalid-etag"`, false); !statusIs(err, 412) {
		return errors.New("S3 provider does not enforce If-Match; safe synchronization is unavailable")
	}
	updated := []byte("racomi conditional-write probe verified")
	if _, err = s.remote.put(ctx, key, updated, etag, false); err != nil {
		return err
	}
	actual, _, err := s.remote.get(ctx, key, 4096)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, updated) {
		return errors.New("S3 probe read verification failed")
	}
	_, _, err = s.remote.request(ctx, "DELETE", key, nil, nil, nil, 4096)
	return err
}

// ListBackups follows only committed history. Failed conditional commits may
// leave immutable orphan manifests, which are intentionally excluded.
func ListBackups(ctx context.Context, config Config) ([]BackupInfo, error) {
	s, err := newSession(config)
	if err != nil {
		return nil, err
	}
	current, _, err := s.readHead(ctx)
	if err != nil {
		return nil, err
	}
	result := []BackupInfo{}
	seen := map[string]bool{}
	for current.ID != "" {
		if seen[current.ID] {
			return nil, errors.New("invalid or excessive remote backup history")
		}
		seen[current.ID] = true
		result = append(result, BackupInfo{current.ID, current.CreatedAt, len(current.State.Skills), len(current.State.Presets)})
		if len(result) >= 100 {
			break
		}
		if current.Parent == "" {
			break
		}
		current, err = s.readManifest(ctx, current.Parent)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// RestoreBackup publishes the selected historical library as a new revision.
// It never rewinds/deletes history, and a concurrent remote update cancels the
// restore rather than being overwritten. The caller serializes local mutations.
func RestoreBackup(ctx context.Context, store *skills.Store, config Config, id string) (Result, error) {
	releaseContent := store.SuspendContentCleanup()
	defer releaseContent()
	result := Result{Conflicts: []skills.SyncConflict{}}
	s, err := newSession(config)
	if err != nil {
		return result, err
	}
	target, err := s.readManifest(ctx, id)
	if err != nil {
		return result, err
	}
	current, etag, err := s.readHead(ctx)
	if err != nil {
		return result, err
	}
	if current.ID == "" {
		return result, errors.New("cannot restore into a missing sync space")
	}
	if err = s.verifyProvider(ctx); err != nil {
		return result, err
	}
	// Require a revision in the committed ancestry, rather than accepting an
	// arbitrary object ID left behind by a failed or concurrent upload.
	ancestor := current
	found := false
	seen := map[string]bool{}
	for ancestor.ID != "" {
		if ancestor.ID == id {
			found = true
			break
		}
		if seen[ancestor.ID] || len(seen) >= 10000 {
			return result, errors.New("invalid backup history")
		}
		seen[ancestor.ID] = true
		if ancestor.Parent == "" {
			break
		}
		ancestor, err = s.readManifest(ctx, ancestor.Parent)
		if err != nil {
			return result, err
		}
	}
	if !found {
		return result, errors.New("backup is not in committed synchronization history")
	}
	local, err := currentShared(store)
	if err != nil {
		return result, err
	}
	count, err := s.downloadObjects(ctx, store, target.State)
	result.Downloaded = count
	if err != nil {
		return result, err
	}
	if err = store.ValidateShared(target.State); err != nil {
		return result, err
	}
	// History may outlive an externally deleted content object. A valid local
	// copy can repair that object before the restored revision references it.
	count, err = s.uploadObjects(ctx, store, target.State)
	result.Uploaded = count
	if err != nil {
		return result, err
	}
	latest, err := currentShared(store)
	if err != nil {
		return result, err
	}
	if !sharedEqual(local, latest) {
		return result, errors.New("local library changed during backup restore; retry")
	}
	committed, err := s.commit(ctx, target.State, current.ID, etag)
	if statusIs(err, 409, 412) {
		return result, errors.New("remote library changed during restore; review the latest library before restoring again")
	}
	if err != nil {
		return result, err
	}
	if err = store.ApplySharedIfCurrent(target.State, local); err != nil {
		return result, err
	}
	if err = saveCheckpoint(store, config, committed.ID, target.State); err != nil {
		return result, err
	}
	result.Revision = committed.ID
	result.LastSyncAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}
