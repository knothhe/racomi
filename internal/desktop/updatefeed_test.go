package desktop

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func linuxArchiveFixture(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	for name, body := range entries {
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			header = &tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := archive.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func signedFeed(t *testing.T, version string, archive []byte, private ed25519.PrivateKey) []byte {
	t.Helper()
	digest := sha256.Sum256(archive)
	feed := updateFeed{Version: version, Commit: testCommit, Assets: map[string]updateAsset{"linux-amd64": {
		URL:         "https://github.com/knothhe/racomi/releases/download/v" + version + "/racomi-" + version + "-x86_64-linux.tar.gz",
		Length:      int64(len(archive)),
		SHA256:      hex.EncodeToString(digest[:]),
		EdSignature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, archive)),
	}}}
	data, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLinuxUpdateFeedVerifiesSignedArchiveAndExtractsOnlyExecutable(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(public)
	name := "racomi-0.2.0-x86_64-linux"
	archive := linuxArchiveFixture(t, map[string]string{name + "/": "", name + "/INSTALL.txt": "notes", name + "/racomi": "new executable", "../escape": "never written"})
	feed, asset, err := parseUpdateFeed(signedFeed(t, "0.2.0", archive, private), "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyUpdateArchive(archive, asset, key); err != nil {
		t.Fatal(err)
	}
	var executable bytes.Buffer
	if err = extractLinuxUpdate(archive, name, &executable); err != nil || executable.String() != "new executable" {
		t.Fatalf("extracted %q, %v", executable.String(), err)
	}
	if feed.Commit != testCommit {
		t.Fatal(feed.Commit)
	}

	corrupt := bytes.Clone(archive)
	corrupt[len(corrupt)/2] ^= 1
	if verifyUpdateArchive(corrupt, asset, key) == nil {
		t.Fatal("tampered archive accepted")
	}
	otherPublic, _, _ := ed25519.GenerateKey(nil)
	if verifyUpdateArchive(archive, asset, base64.StdEncoding.EncodeToString(otherPublic)) == nil {
		t.Fatal("archive signed by another key accepted")
	}
	// A matching digest without the signature over these bytes is not trust.
	_, otherPrivate, _ := ed25519.GenerateKey(nil)
	forged := asset
	forged.EdSignature = base64.StdEncoding.EncodeToString(ed25519.Sign(otherPrivate, archive))
	if verifyUpdateArchive(archive, forged, key) == nil {
		t.Fatal("forged signature accepted")
	}
	if _, _, err = parseUpdateFeed(signedFeed(t, "0.2.0", archive, private), "linux", "arm64"); err == nil {
		t.Fatal("missing architecture accepted")
	}
	if extractLinuxUpdate(linuxArchiveFixture(t, map[string]string{name + "/README": "x"}), name, &bytes.Buffer{}) == nil {
		t.Fatal("archive without executable accepted")
	}
}

func TestLinuxUpdateFeedPinsReleaseDownloadURL(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(nil)
	archive := linuxArchiveFixture(t, map[string]string{"racomi-0.2.0-x86_64-linux/racomi": "x"})
	valid := signedFeed(t, "0.2.0", archive, private)
	for _, change := range []func(*updateFeed){
		func(f *updateFeed) {
			a := f.Assets["linux-amd64"]
			a.URL = "https://example.test/racomi.tar.gz"
			f.Assets["linux-amd64"] = a
		},
		func(f *updateFeed) { f.Version = "0.3.0" },
		func(f *updateFeed) { f.Version = "latest" },
		func(f *updateFeed) { f.Commit = "main" },
		func(f *updateFeed) {
			a := f.Assets["linux-amd64"]
			a.Length = maxUpdateArchiveBytes + 1
			f.Assets["linux-amd64"] = a
		},
		func(f *updateFeed) { a := f.Assets["linux-amd64"]; a.EdSignature = ""; f.Assets["linux-amd64"] = a },
	} {
		var feed updateFeed
		if err := json.Unmarshal(valid, &feed); err != nil {
			t.Fatal(err)
		}
		change(&feed)
		data, _ := json.Marshal(feed)
		if _, _, err := parseUpdateFeed(data, "linux", "amd64"); err == nil {
			t.Fatalf("invalid feed accepted: %s", data)
		}
	}
}

func TestCompareVersionsFollowsSemVer(t *testing.T) {
	ordered := []string{"0.1.2", "0.1.10", "0.2.0-alpha", "0.2.0-alpha.1", "0.2.0-alpha.beta", "0.2.0-beta.2", "0.2.0-beta.11", "0.2.0-rc.1", "0.2.0", "1.0.0"}
	for i := range ordered {
		for j := range ordered {
			got, want := compareVersions(ordered[i], ordered[j]), 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got != want {
				t.Fatalf("compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
	if compareVersions("development", "0.0.1") >= 0 || compareVersions("0.1.0+build.5", "0.1.0") != 0 {
		t.Fatal("invalid or build metadata ordering")
	}
}
