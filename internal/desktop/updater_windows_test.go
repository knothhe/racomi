package desktop

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"racomi/internal/buildinfo"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if RunUpdateHelper() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type windowsFixtureTransport map[string][]byte

func (f windowsFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	data, ok := f[request.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
}

func windowsReleaseFixture(t *testing.T, info buildinfo.Info) []byte {
	t.Helper()
	data, _ := json.Marshal(info)
	path := filepath.Join(t.TempDir(), "fixture.exe")
	command := exec.Command("go", "build", "-ldflags", "-s=false -w=false -X 'main.identity="+string(data)+"'", "-o", path, "./testdata/update-fixture")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return binary
}

func requireVisibleWindowsRestart(t *testing.T, marker string) {
	t.Helper()
	data, err := os.ReadFile(marker + ".startup.json")
	if err != nil {
		t.Fatal("restarted process did not report its startup window settings:", err)
	}
	var startup struct {
		Hidden bool `json:"hidden"`
	}
	if err = json.Unmarshal(data, &startup); err != nil {
		t.Fatal(err)
	}
	if startup.Hidden {
		t.Fatal("restart forces the application window hidden")
	}
}

func TestWindowsUpdaterStagesOnlyVerifiedIdentity(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(nil)
	oldKey, oldClient := buildinfo.PublicKey, updateHTTPClient
	defer func() { buildinfo.PublicKey, updateHTTPClient = oldKey, oldClient }()
	buildinfo.PublicKey = base64.StdEncoding.EncodeToString(public)
	release := buildinfo.Info{ID: buildinfo.Current().ID, Channel: "stable", Executor: "ci", OS: "windows", Arch: "amd64", Version: "9.0.0", Commit: testCommit}
	for name, info := range map[string]buildinfo.Info{
		"exact":         release,
		"wrong version": func() buildinfo.Info { i := release; i.Version = "8.0.0"; return i }(),
		"wrong commit":  func() buildinfo.Info { i := release; i.Commit = strings.Repeat("f", 40); return i }(),
		"local":         func() buildinfo.Info { i := release; i.Executor = "local"; return i }(),
	} {
		t.Run(name, func(t *testing.T) {
			binary := windowsReleaseFixture(t, info)
			archive := windowsArchiveFixture(t, []string{"racomi.exe"}, []string{string(binary)})
			data := signedWindowsFeed(t, archive, private)
			feed, asset, err := parseUpdateFeed(data, "windows", "amd64")
			if err != nil {
				t.Fatal(err)
			}
			updateHTTPClient = &http.Client{Transport: windowsFixtureTransport{asset.URL: archive}}
			dir := t.TempDir()
			u := windowsUpdater{executable: filepath.Join(dir, "racomi.exe")}
			staged, err := u.download(feed, asset)
			if name == "exact" {
				if err != nil {
					t.Fatal(err)
				}
				if filepath.Dir(staged) != dir {
					t.Fatal(staged)
				}
			} else {
				leftovers, _ := filepath.Glob(filepath.Join(dir, updatePrefix+"*"))
				if err == nil || len(leftovers) != 0 {
					t.Fatal("unverified identity staged", err, leftovers)
				}
			}
		})
	}
}

func TestWindowsUpdateHelperWaitsReplacesAndRestarts(t *testing.T) {
	dir := t.TempDir()
	info := buildinfo.Info{ID: buildinfo.Current().ID, Channel: "stable", Executor: "ci", OS: "windows", Arch: "amd64", Version: "9.0.0", Commit: testCommit}
	binary := windowsReleaseFixture(t, info)
	oldInfo := info
	oldInfo.Version = "8.0.0"
	oldBinary := windowsReleaseFixture(t, oldInfo)
	exe := filepath.Join(dir, "racomi.exe")
	staged := filepath.Join(dir, updatePrefix+"stage.exe")
	helper := filepath.Join(dir, updatePrefix+"helper-test.exe")
	for path, contents := range map[string][]byte{exe: oldBinary, staged: binary} {
		if err := os.WriteFile(path, contents, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := os.Executable()
	testBinary, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(helper, testBinary, 0o700); err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(exe, "--hold")
	if err = parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer parent.Process.Kill()
	digest, _ := executableDigest(staged)
	plan := windowsUpdatePlan{PID: parent.Process.Pid, Executable: exe, Staged: staged, SHA256: digest, Feed: updateFeed{Version: "9.0.0", Commit: testCommit}}
	planData, _ := json.Marshal(plan)
	if err = os.WriteFile(helper+".json", planData, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "restarted.json")
	command := exec.Command(helper, "--racomi-apply-update", helper+".json")
	command.Env = append(os.Environ(), "RACOMI_TEST_UPDATE_MARKER="+marker)
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		t.Fatal("helper did not wait for parent", err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err = os.Stat(staged); err != nil {
		t.Fatal("staged file changed before parent exit", err)
	}
	parent.Process.Kill()
	parent.Wait()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		command.Process.Kill()
		t.Fatal("helper timed out")
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if _, err = os.Stat(marker); err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err = os.Stat(marker); err != nil {
		logData, _ := os.ReadFile(helper + ".log")
		t.Fatalf("restart failed: %v %s", err, logData)
	}
	if _, err = os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("staged executable not consumed", err)
	}
	if digestAfter, _ := executableDigest(exe); digestAfter != digest {
		t.Fatal("wrong installed executable")
	}
	requireVisibleWindowsRestart(t, marker)
}

func TestWindowsReplacementFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "racomi.exe")
	stage := filepath.Join(dir, "new.exe")
	backup := filepath.Join(dir, "old.exe")
	os.WriteFile(exe, []byte("old"), 0o600)
	os.WriteFile(stage, []byte("new"), 0o600)
	locked, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err = replaceWindowsExecutable(exe, stage, backup); err == nil {
		locked.Close()
		t.Fatal("locked executable replaced")
	}
	locked.Close()
	data, _ := os.ReadFile(exe)
	if string(data) != "old" {
		t.Fatal("previous executable damaged")
	}
	if err = replaceWindowsExecutable(exe, stage, backup); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(backup)
	if string(data) != "old" {
		t.Fatal("backup missing")
	}
}

func TestWindowsUpdatePlanRejectsTamperingAndProcessTimeout(t *testing.T) {
	dir := t.TempDir()
	info := buildinfo.Info{ID: buildinfo.Current().ID, Channel: "stable", Executor: "ci", OS: "windows", Arch: "amd64", Version: "9.0.0", Commit: testCommit}
	binary := windowsReleaseFixture(t, info)
	exe := filepath.Join(dir, "racomi.exe")
	staged := filepath.Join(dir, updatePrefix+"stage.exe")
	helper := filepath.Join(dir, updatePrefix+"helper-test.exe")
	for _, path := range []string{exe, staged, helper} {
		if err := os.WriteFile(path, binary, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	digest, _ := executableDigest(staged)
	plan := windowsUpdatePlan{PID: os.Getpid(), Executable: exe, Staged: staged, SHA256: digest, Feed: updateFeed{Version: "9.0.0", Commit: testCommit}}
	if err := validateWindowsUpdatePlan(plan, helper, helper+".json"); err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.SHA256 = strings.Repeat("0", 64)
	if validateWindowsUpdatePlan(changed, helper, helper+".json") == nil {
		t.Fatal("changed staging accepted")
	}
	changed = plan
	changed.Executable = filepath.Join(t.TempDir(), "racomi.exe")
	if validateWindowsUpdatePlan(changed, helper, helper+".json") == nil {
		t.Fatal("foreign installation accepted")
	}
	if waitForWindowsProcess(os.Getpid(), 20*time.Millisecond) == nil {
		t.Fatal("running parent did not block replacement")
	}
}

func TestWindowsUpdateRollsBackWhenRestartCannotLaunch(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "racomi.exe")
	staged := filepath.Join(dir, updatePrefix+"stage.exe")
	info := buildinfo.Info{ID: buildinfo.Current().ID, Channel: "stable", Executor: "ci", OS: "windows", Arch: "amd64", Version: "8.0.0", Commit: testCommit}
	old := windowsReleaseFixture(t, info)
	os.WriteFile(exe, old, 0o700)
	os.WriteFile(staged, []byte("not a PE executable"), 0o700)
	parent := exec.Command(exe, "--build-info")
	if err := parent.Run(); err != nil {
		t.Fatal(err)
	}
	digest, _ := executableDigest(staged)
	marker := filepath.Join(dir, "restored.json")
	t.Setenv("RACOMI_TEST_UPDATE_MARKER", marker)
	if err := applyWindowsUpdate(windowsUpdatePlan{PID: parent.Process.Pid, Executable: exe, Staged: staged, SHA256: digest}); err == nil {
		t.Fatal("invalid replacement launched")
	}
	data, _ := os.ReadFile(exe)
	if !bytes.Equal(data, old) {
		t.Fatal("previous executable not restored")
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if _, err := os.Stat(marker); err == nil {
			requireVisibleWindowsRestart(t, marker)
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("previous executable was not restarted")
}

func TestWindowsAutomaticUpdatePreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater.json")
	if !loadAutomaticUpdates(path) || !loadAutomaticUpdates("") {
		t.Fatal("automatic checks must default on")
	}
	os.WriteFile(path, []byte(`{"automatic":false}`), 0o600)
	if loadAutomaticUpdates(path) {
		t.Fatal("saved preference ignored")
	}
}
