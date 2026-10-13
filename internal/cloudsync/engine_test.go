package cloudsync

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"racomi/internal/skills"
)

type fakeObject struct {
	data []byte
	etag string
}
type fakeS3 struct {
	mu               sync.Mutex
	objects          map[string]fakeObject
	beforeHeadPut    func()
	failPut          string
	objectPutStatus  int
	ignoreConditions bool
	headWrites       int
}

func newFakeS3(t *testing.T) (*fakeS3, Config) {
	t.Helper()
	fake := &fakeS3{objects: map[string]fakeObject{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, Config{Endpoint: server.URL, Region: "auto", Bucket: "test-bucket", Prefix: "library", AccessKeyID: "example-id", SecretAccessKey: "example-secret", Passphrase: "this is a strong test encryption passphrase", PathStyle: true}
}
func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		w.WriteHeader(403)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
	if r.Method == "PUT" && strings.HasSuffix(key, "head.enc") {
		f.mu.Lock()
		hook := f.beforeHeadPut
		f.beforeHeadPut = nil
		f.mu.Unlock()
		if hook != nil {
			hook()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Query().Get("list-type") == "2" {
		prefix := r.URL.Query().Get("prefix")
		var names []string
		for key := range f.objects {
			if strings.HasPrefix(key, prefix) {
				names = append(names, key)
			}
		}
		sort.Strings(names)
		page := listPage{}
		for _, name := range names {
			page.Contents = append(page.Contents, struct {
				Key string `xml:"Key"`
			}{name})
		}
		data, _ := xml.Marshal(page)
		w.Write(data)
		return
	}
	object, exists := f.objects[key]
	switch r.Method {
	case "GET", "HEAD":
		if !exists {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", object.etag)
		w.Header().Set("Content-Length", fmt.Sprint(len(object.data)))
		if r.Method == "GET" {
			w.Write(object.data)
		}
	case "PUT":
		if f.objectPutStatus != 0 && strings.Contains(key, "/objects/") {
			w.WriteHeader(f.objectPutStatus)
			return
		}
		if f.failPut != "" && strings.Contains(key, f.failPut) {
			w.WriteHeader(500)
			return
		}
		if !f.ignoreConditions && (r.Header.Get("If-None-Match") == "*" && exists || r.Header.Get("If-Match") != "" && (!exists || r.Header.Get("If-Match") != object.etag)) {
			w.WriteHeader(412)
			return
		}
		data, _ := io.ReadAll(r.Body)
		etag := `"` + sumHex(data) + `"`
		f.objects[key] = fakeObject{data, etag}
		w.Header().Set("ETag", etag)
		if strings.HasSuffix(key, "head.enc") {
			f.headWrites++
		}
	case "DELETE":
		delete(f.objects, key)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}
func openStore(t *testing.T) *skills.Store {
	t.Helper()
	store, err := skills.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func addSkill(t *testing.T, store *skills.Store, name string) skills.Skill {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Sync testing\n---\n# "+name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	installed, err := store.Install([]skills.Candidate{{Skill: skills.Skill{Name: name, Description: "Sync testing"}, Path: dir}})
	if err != nil {
		t.Fatal(err)
	}
	return installed[0]
}
func savePreset(t *testing.T, store *skills.Store, id, name string) skills.Preset {
	t.Helper()
	preset, err := store.SavePreset(skills.Preset{ID: id, Name: name, SkillIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return preset
}
func syncOK(t *testing.T, store *skills.Store, cfg Config) Result {
	t.Helper()
	result, err := Sync(context.Background(), store, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) > 0 {
		t.Fatalf("unexpected conflicts: %#v", result.Conflicts)
	}
	return result
}

func TestSnapshotReadsKeepIncomingCloudContentUntilCommit(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(fmt.Sprint("restore=", restore), func(t *testing.T) {
			fake, cfg := newFakeS3(t)
			a, b := openStore(t), openStore(t)
			skill := addSkill(t, a, "incoming")
			first := syncOK(t, a, cfg)
			if restore {
				if err := a.RemoveSkills([]string{skill.ID}); err != nil {
					t.Fatal(err)
				}
				syncOK(t, a, cfg)
				syncOK(t, b, cfg)
			} else {
				savePreset(t, b, "", "Local preset")
			}
			checked := make(chan error, 1)
			fake.mu.Lock()
			fake.beforeHeadPut = func() {
				_, err := b.Snapshots()
				checked <- err
			}
			fake.mu.Unlock()
			var err error
			if restore {
				_, err = RestoreBackup(context.Background(), b, cfg, first.Revision)
			} else {
				_, err = Sync(context.Background(), b, cfg, nil)
			}
			if err != nil {
				t.Fatal("snapshot read disrupted cloud commit", err)
			}
			select {
			case err = <-checked:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("snapshot read did not run between download and commit")
			}
			if _, err = b.Snapshots(); err != nil {
				t.Fatal(err)
			}
			if actual, err := skills.HashDir(filepath.Join(b.Root(), "objects", skill.ContentHash)); err != nil || actual != skill.ContentHash {
				t.Fatal("incoming cloud content was reclaimed", actual, err)
			}
		})
	}
}
func presetNames(t *testing.T, store *skills.Store) string {
	t.Helper()
	state, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, preset := range state.Presets {
		names = append(names, preset.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func TestBidirectionalSyncAndDeletionAcrossRestart(t *testing.T) {
	fake, cfg := newFakeS3(t)
	a, b := openStore(t), openStore(t)
	skill := addSkill(t, a, "example")
	savePreset(t, a, "", "Shared")
	first := syncOK(t, a, cfg)
	if first.Uploaded != 1 {
		t.Fatalf("uploaded=%d", first.Uploaded)
	}
	second := syncOK(t, b, cfg)
	if second.Downloaded != 1 {
		t.Fatalf("downloaded=%d", second.Downloaded)
	}
	if actual, err := skills.HashDir(filepath.Join(b.Root(), "objects", skill.ContentHash)); err != nil || actual != skill.ContentHash {
		t.Fatalf("download verification %s %v", actual, err)
	}
	savePreset(t, a, "", "From A")
	savePreset(t, b, "", "From B")
	syncOK(t, a, cfg)
	syncOK(t, b, cfg)
	syncOK(t, a, cfg)
	if got := presetNames(t, a); got != "From A,From B,Shared" {
		t.Fatal(got)
	}
	if err := a.RemoveSkill(skill.ID); err != nil {
		t.Fatal(err)
	}
	syncOK(t, a, cfg)
	// Reopening the device must retain its common baseline: an offline old
	// version cannot resurrect the skill removed by the other device.
	b, err := skills.Open(b.Root())
	if err != nil {
		t.Fatal(err)
	}
	savePreset(t, b, "", "Offline preset")
	syncOK(t, b, cfg)
	state, _ := b.State()
	if len(state.Skills) != 0 {
		t.Fatal("deleted skill resurrected")
	}
	syncOK(t, a, cfg)
	if presetNames(t, a) != "From A,From B,Offline preset,Shared" {
		t.Fatal(presetNames(t, a))
	}
	fake.mu.Lock()
	writes := fake.headWrites
	fake.mu.Unlock()
	beforeSnapshots, _ := a.Snapshots()
	syncOK(t, a, cfg)
	afterSnapshots, _ := a.Snapshots()
	if len(beforeSnapshots) != len(afterSnapshots) {
		t.Fatal("no-op sync created another local snapshot")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.headWrites != writes {
		t.Fatal("no-op sync created another backup")
	}
	for key, object := range fake.objects {
		if strings.Contains(key, "/probes/") {
			continue
		}
		if bytes.Contains(object.data, []byte("SKILL.md")) || bytes.Contains(object.data, []byte("Offline preset")) {
			t.Fatalf("plaintext leaked in %s", key)
		}
	}
}

func TestConflictsRequireCurrentResolutionToken(t *testing.T) {
	fake, cfg := newFakeS3(t)
	a, b := openStore(t), openStore(t)
	preset := savePreset(t, a, "", "Original")
	syncOK(t, a, cfg)
	syncOK(t, b, cfg)
	savePreset(t, a, preset.ID, "Changed A")
	savePreset(t, b, preset.ID, "Changed B")
	syncOK(t, a, cfg)
	fake.mu.Lock()
	before := fake.headWrites
	fake.mu.Unlock()
	result, err := Sync(context.Background(), b, cfg, nil)
	if err != nil || len(result.Conflicts) != 1 || result.ResolutionToken == "" {
		t.Fatalf("conflict result %#v, %v", result, err)
	}
	if presetNames(t, b) != "Changed B" {
		t.Fatal("conflict changed local library")
	}
	fake.mu.Lock()
	after := fake.headWrites
	fake.mu.Unlock()
	if before != after {
		t.Fatal("conflict changed remote head")
	}
	key := result.Conflicts[0].Key
	choices := map[string]string{key: "remote", "__token": result.ResolutionToken}
	savePreset(t, b, preset.ID, "Changed again B")
	if _, err = Sync(context.Background(), b, cfg, choices); err == nil {
		t.Fatal("stale local resolution accepted")
	}
	result, err = Sync(context.Background(), b, cfg, nil)
	if err != nil || len(result.Conflicts) != 1 {
		t.Fatal(err)
	}
	choices["__token"] = result.ResolutionToken
	savePreset(t, a, preset.ID, "Changed again A")
	syncOK(t, a, cfg)
	if _, err = Sync(context.Background(), b, cfg, choices); err == nil {
		t.Fatal("stale remote resolution accepted")
	}
	result, err = Sync(context.Background(), b, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	choices["__token"] = result.ResolutionToken
	resolved, err := Sync(context.Background(), b, cfg, choices)
	if err != nil || len(resolved.Conflicts) > 0 {
		t.Fatal(err)
	}
	if presetNames(t, b) != "Changed again A" {
		t.Fatal(presetNames(t, b))
	}
}

func TestConcurrentCASMergesAndHistoryRestore(t *testing.T) {
	fake, cfg := newFakeS3(t)
	a, b := openStore(t), openStore(t)
	savePreset(t, a, "", "Initial")
	first := syncOK(t, a, cfg)
	syncOK(t, b, cfg)
	savePreset(t, a, "", "From A")
	savePreset(t, b, "", "From B")
	var concurrentErr error
	fake.mu.Lock()
	fake.beforeHeadPut = func() { _, concurrentErr = Sync(context.Background(), b, cfg, nil) }
	fake.mu.Unlock()
	syncOK(t, a, cfg)
	if concurrentErr != nil {
		t.Fatal(concurrentErr)
	}
	syncOK(t, b, cfg)
	if presetNames(t, a) != "From A,From B,Initial" || presetNames(t, b) != presetNames(t, a) {
		t.Fatal("CAS retry lost changes")
	}
	history, err := ListBackups(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history should exclude failed CAS orphan: %#v", history)
	}
	restored, err := RestoreBackup(context.Background(), a, cfg, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Revision == first.Revision || presetNames(t, a) != "Initial" {
		t.Fatal("restore must publish a new revision")
	}
	syncOK(t, b, cfg)
	if presetNames(t, b) != "Initial" {
		t.Fatal("restoration did not synchronize")
	}
	history, err = ListBackups(context.Background(), cfg)
	if err != nil || len(history) != 4 {
		t.Fatalf("restored history %#v %v", history, err)
	}
}

func TestWrongKeyCorruptionAndMaliciousArchivesLeaveIndicesUnchanged(t *testing.T) {
	fake, cfg := newFakeS3(t)
	a := openStore(t)
	skill := addSkill(t, a, "example")
	syncOK(t, a, cfg)
	fake.mu.Lock()
	writes := fake.headWrites
	fake.mu.Unlock()
	wrong := cfg
	wrong.Passphrase = "a different encryption passphrase"
	if _, err := Sync(context.Background(), openStore(t), wrong, nil); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("wrong key: %v", err)
	}
	key := cfg.Prefix + "/v1/objects/" + skill.ContentHash + ".enc"
	fake.mu.Lock()
	original := fake.objects[key]
	damaged := append([]byte{}, original.data...)
	damaged[len(damaged)-1] ^= 1
	fake.objects[key] = fakeObject{damaged, original.etag}
	fake.mu.Unlock()
	b := openStore(t)
	if _, err := Sync(context.Background(), b, cfg, nil); err == nil {
		t.Fatal("damaged object accepted")
	}
	state, _ := b.State()
	if len(state.Skills) != 0 {
		t.Fatal("damage changed local index")
	}
	var zipBytes bytes.Buffer
	writer := zip.NewWriter(&zipBytes)
	file, _ := writer.Create("../escaped")
	file.Write([]byte("malicious"))
	writer.Close()
	encryption, _ := newCodec(cfg)
	malicious, _ := encryption.seal(key, zipBytes.Bytes())
	fake.mu.Lock()
	fake.objects[key] = fakeObject{malicious, original.etag}
	fake.mu.Unlock()
	if _, err := Sync(context.Background(), openStore(t), cfg, nil); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("malicious archive accepted: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.headWrites != writes {
		t.Fatal("failed download changed remote index")
	}
}

func TestInterruptedCommitAndRemoteHeadDeletion(t *testing.T) {
	fake, cfg := newFakeS3(t)
	a := openStore(t)
	savePreset(t, a, "", "First")
	syncOK(t, a, cfg)
	savePreset(t, a, "", "Second")
	fake.mu.Lock()
	writes := fake.headWrites
	fake.failPut = "head.enc"
	fake.mu.Unlock()
	if _, err := Sync(context.Background(), a, cfg, nil); err == nil {
		t.Fatal("interrupted commit succeeded")
	}
	b := openStore(t)
	syncOK(t, b, cfg)
	if presetNames(t, b) != "First" {
		t.Fatal("incomplete commit became visible")
	}
	fake.mu.Lock()
	fake.failPut = ""
	if writes != fake.headWrites {
		t.Fatal("failed commit changed head")
	}
	fake.mu.Unlock()
	syncOK(t, a, cfg)
	fake.mu.Lock()
	delete(fake.objects, cfg.Prefix+"/v1/head.enc")
	fake.mu.Unlock()
	if _, err := Sync(context.Background(), a, cfg, nil); err == nil {
		t.Fatal("deleted sync space resurrected")
	}
}

func TestConnectionRequiresConditionalWrites(t *testing.T) {
	fake, cfg := newFakeS3(t)
	if err := TestConnection(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	if len(fake.objects) != 0 {
		t.Fatal("connection probe not removed")
	}
	fake.ignoreConditions = true
	fake.mu.Unlock()
	if err := TestConnection(context.Background(), cfg); err == nil {
		t.Fatal("unsafe provider accepted")
	}
	// A user may save configuration without clicking Test connection. Sync
	// itself must still reject an incompatible provider before publishing head.
	store := openStore(t)
	savePreset(t, store, "", "Local")
	if _, err := Sync(context.Background(), store, cfg, nil); err == nil {
		t.Fatal("sync bypassed provider capability verification")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.headWrites != 0 {
		t.Fatal("unsafe provider received a sync commit")
	}
}

func TestExistingLargeObjectHEADUsesMetadataSize(t *testing.T) {
	_, cfg := newFakeS3(t)
	a := openStore(t)
	dir := t.TempDir()
	data := make([]byte, 16<<10)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: large\ndescription: test\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.bin"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Install([]skills.Candidate{{Skill: skills.Skill{Name: "large", Description: "test"}, Path: dir}}); err != nil {
		t.Fatal(err)
	}
	syncOK(t, a, cfg)
	savePreset(t, a, "", "Force another commit")
	result := syncOK(t, a, cfg)
	if result.Uploaded != 0 {
		t.Fatal("existing content was reuploaded")
	}
}

func TestLocalEditDuringCommitIsNeverOverwritten(t *testing.T) {
	fake, cfg := newFakeS3(t)
	a := openStore(t)
	savePreset(t, a, "", "Initial")
	syncOK(t, a, cfg)
	savePreset(t, a, "", "Before commit")
	fake.mu.Lock()
	fake.beforeHeadPut = func() { savePreset(t, a, "", "During network request") }
	fake.mu.Unlock()
	if _, err := Sync(context.Background(), a, cfg, nil); err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("concurrent edit was not reported: %v", err)
	}
	if !strings.Contains(presetNames(t, a), "During network request") {
		t.Fatal("concurrent local edit was overwritten")
	}
	syncOK(t, a, cfg)
	b := openStore(t)
	syncOK(t, b, cfg)
	if presetNames(t, a) != presetNames(t, b) {
		t.Fatal("retry did not reconcile concurrent edit")
	}
}

func TestContentConditionalFailuresCannotPublishMissingObjects(t *testing.T) {
	for _, status := range []int{409, 412} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fake, cfg := newFakeS3(t)
			a := openStore(t)
			syncOK(t, a, cfg)
			addSkill(t, a, "new-content")
			fake.mu.Lock()
			before := fake.headWrites
			fake.objectPutStatus = status
			fake.mu.Unlock()
			if _, err := Sync(context.Background(), a, cfg, nil); err == nil {
				t.Fatal("conditional failure with no stored content was accepted")
			}
			fake.mu.Lock()
			if fake.headWrites != before {
				t.Error("published a manifest referring to missing content")
			}
			fake.objectPutStatus = 0
			fake.mu.Unlock()
			syncOK(t, a, cfg)
		})
	}
}

func TestRestoreRepairsMissingRemoteObjectsFromVerifiedLocalCopy(t *testing.T) {
	fake, cfg := newFakeS3(t)
	a := openStore(t)
	skill := addSkill(t, a, "historical-content")
	first := syncOK(t, a, cfg)
	if err := a.RemoveSkill(skill.ID); err != nil {
		t.Fatal(err)
	}
	syncOK(t, a, cfg)
	fake.mu.Lock()
	delete(fake.objects, cfg.Prefix+"/v1/objects/"+skill.ContentHash+".enc")
	fake.mu.Unlock()
	restored, err := RestoreBackup(context.Background(), a, cfg, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Uploaded != 1 || restored.Downloaded != 0 {
		t.Fatalf("restore should repair from local copy: %#v", restored)
	}
	b := openStore(t)
	result := syncOK(t, b, cfg)
	if result.Downloaded != 1 {
		t.Fatal("restored revision did not provide content to new device")
	}
	state, err := b.State()
	if err != nil || len(state.Skills) != 1 || state.Skills[0].ContentHash != skill.ContentHash {
		t.Fatalf("restored state invalid: %#v %v", state.Skills, err)
	}
}

func TestSigV4OfficialS3Example(t *testing.T) {
	// AWS's published single-chunk signing test vector:
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html
	req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	signRequest(req, nil, Config{Region: "us-east-1", AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}, now)
	if !strings.HasSuffix(req.Header.Get("Authorization"), "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41") {
		t.Fatal(req.Header.Get("Authorization"))
	}
}

func TestConfigAndArchiveBounds(t *testing.T) {
	_, cfg := newFakeS3(t)
	for _, endpoint := range []string{"http://s3.example.com", "https://user:pass@example.com", "https://example.com/bucket", "https://example.com?secret=abc"} {
		bad := cfg
		bad.Endpoint = endpoint
		if err := ValidateConfig(bad); err == nil {
			t.Fatal(endpoint)
		}
	}
	for _, prefix := range []string{"a/../b", "a//b", "../b", "a\\b"} {
		bad := cfg
		bad.Prefix = prefix
		if err := ValidateConfig(bad); err == nil {
			t.Fatal(prefix)
		}
	}
	for _, name := range []string{"../x", "/x", "a/../../x", "a\\x", "C:/x", "a/./b", "a//b"} {
		if safeArchiveName(name) {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"SKILL.md", "scripts/run.sh", "empty/"} {
		if !safeArchiveName(name) {
			t.Fatal(name)
		}
	}
	if actual := canonicalQuery(map[string][]string{"prefix": {"a b/+"}, "list-type": {"2"}}); actual != "list-type=2&prefix=a%20b%2F%2B" {
		t.Fatal(actual)
	}
	for _, mode := range []os.FileMode{os.ModeSymlink | 0777, os.ModeNamedPipe | 0600} {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		header := zip.FileHeader{Name: "unsafe"}
		header.SetMode(mode)
		entry, _ := writer.CreateHeader(&header)
		fmt.Fprint(entry, "target")
		writer.Close()
		if err := extractObject(buffer.Bytes(), t.TempDir(), strings.Repeat("0", 64)); err == nil {
			t.Fatalf("accepted mode %v", mode)
		}
	}
}
