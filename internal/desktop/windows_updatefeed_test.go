package desktop

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func windowsArchiveFixture(t *testing.T, names, bodies []string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for i, name := range names {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if len(name) > 0 && name[len(name)-1] == '/' {
			continue
		}
		if _, err = file.Write([]byte(bodies[i])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func signedWindowsFeed(t *testing.T, data []byte, key ed25519.PrivateKey) []byte {
	t.Helper()
	digest := sha256.Sum256(data)
	feed := linuxUpdateFeed{Version: "9.0.0", Commit: testCommit, Assets: map[string]linuxUpdateAsset{"amd64": {
		URL: releaseDownloadBase + "/v9.0.0/racomi-9.0.0-x86_64-windows.zip", Length: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), EdSignature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)),
	}}}
	result, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestWindowsUpdateTrustAndZIPExtraction(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(nil)
	key := base64.StdEncoding.EncodeToString(public)
	archive := windowsArchiveFixture(t, []string{"../escape", "INSTALL.txt", "racomi.exe"}, []string{"ignore", "notes", "executable"})
	data := signedWindowsFeed(t, archive, private)
	_, asset, err := parseWindowsUpdateFeed(data, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyLinuxUpdateArchive(archive, asset, key); err != nil {
		t.Fatal(err)
	}
	var binary bytes.Buffer
	if err = extractWindowsUpdate(archive, &binary); err != nil || binary.String() != "executable" {
		t.Fatal(binary.String(), err)
	}
	archive[0] ^= 1
	if verifyLinuxUpdateArchive(archive, asset, key) == nil {
		t.Fatal("tampered archive accepted")
	}
	if _, _, err = parseWindowsUpdateFeed(data, "arm64"); err == nil {
		t.Fatal("unsupported architecture accepted")
	}
	for _, mutation := range []func(*linuxUpdateFeed){
		func(f *linuxUpdateFeed) { f.Version = "9.0.0-rc.1" },
		func(f *linuxUpdateFeed) { f.Commit = "main" },
		func(f *linuxUpdateFeed) {
			a := f.Assets["amd64"]
			a.URL = "https://example.com/app.zip"
			f.Assets["amd64"] = a
		},
		func(f *linuxUpdateFeed) {
			a := f.Assets["amd64"]
			a.URL = releaseDownloadBase + "/v9.0.0/racomi-9.0.0-x86_64-linux.tar.gz"
			f.Assets["amd64"] = a
		},
	} {
		var feed linuxUpdateFeed
		json.Unmarshal(data, &feed)
		mutation(&feed)
		changed, _ := json.Marshal(feed)
		if _, _, err = parseWindowsUpdateFeed(changed, "amd64"); err == nil {
			t.Fatal("invalid feed accepted")
		}
	}
	for _, names := range [][]string{{"racomi.exe", "racomi.exe"}, {"folder/racomi.exe"}, {"racomi.exe/"}} {
		bodies := make([]string, len(names))
		for i := range bodies {
			bodies[i] = "binary"
		}
		if extractWindowsUpdate(windowsArchiveFixture(t, names, bodies), &bytes.Buffer{}) == nil {
			t.Fatal("ambiguous/invalid executable accepted", names)
		}
	}
}
