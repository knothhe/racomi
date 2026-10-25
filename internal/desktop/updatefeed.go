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
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// update.json is a small manifest published beside each stable release for
// Linux and Windows. Its archives are signed with the same Ed25519 key as the
// macOS appcast, so a client trusts nothing the signature does not cover.
type updateFeed struct {
	Version    string                 `json:"version"`
	Commit     string                 `json:"commit"`
	ReleaseURL string                 `json:"releaseURL"`
	Assets     map[string]updateAsset `json:"assets"`
}

type updateAsset struct {
	URL         string `json:"url"`
	Length      int64  `json:"length"`
	SHA256      string `json:"sha256"`
	EdSignature string `json:"edSignature"`
}

const (
	maxUpdateFeedBytes    = 1 << 20
	maxUpdateArchiveBytes = 256 << 20
	maxUpdateBinaryBytes  = 512 << 20
)

// releaseDownloadBase is where release archives may come from. Like the feed
// URL it is fixed at build time; -X can point a verification build elsewhere.
var releaseDownloadBase = "https://github.com/knothhe/racomi/releases/download"

var (
	semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func linuxArchiveArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	}
	return "", fmt.Errorf("unsupported Linux architecture: %s", goarch)
}

func linuxArchiveName(version, goarch string) (string, error) {
	arch, err := linuxArchiveArch(goarch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("racomi-%s-%s-linux", version, arch), nil
}

// parseUpdateFeed pins every archive to its release download URL, so a
// tampered manifest can point only at an archive the release itself published.
// Assets are keyed by GOOS-GOARCH; the feed only ever carries stable versions.
func parseUpdateFeed(data []byte, goos, goarch string) (updateFeed, updateAsset, error) {
	var feed updateFeed
	if len(data) > maxUpdateFeedBytes {
		return feed, updateAsset{}, errors.New("The update feed is too large.")
	}
	if json.Unmarshal(data, &feed) != nil || !semverPattern.MatchString(feed.Version) || !commitPattern.MatchString(feed.Commit) || strings.Contains(feed.Version, "-") {
		return feed, updateAsset{}, errors.New("The update feed is invalid.")
	}
	asset, ok := feed.Assets[goos+"-"+goarch]
	if !ok {
		return feed, asset, errors.New("No update is published for this platform.")
	}
	var name string
	var err error
	switch goos {
	case "linux":
		if name, err = linuxArchiveName(feed.Version, goarch); err == nil {
			name += ".tar.gz"
		}
	case "windows":
		name, err = windowsArchiveName(feed.Version, goarch)
	default:
		err = fmt.Errorf("unsupported update platform: %s", goos)
	}
	if err != nil {
		return feed, asset, err
	}
	if asset.URL != releaseDownloadBase+"/v"+feed.Version+"/"+name || asset.Length <= 0 || asset.Length > maxUpdateArchiveBytes || len(asset.SHA256) != 64 || asset.EdSignature == "" {
		return feed, asset, errors.New("The update feed is invalid.")
	}
	return feed, asset, nil
}

func verifyUpdateArchive(data []byte, asset updateAsset, publicKey string) error {
	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("The update feed is not configured.")
	}
	signature, err := base64.StdEncoding.DecodeString(asset.EdSignature)
	if err != nil {
		return errors.New("The update signature is invalid.")
	}
	digest := sha256.Sum256(data)
	if int64(len(data)) != asset.Length || !strings.EqualFold(hex.EncodeToString(digest[:]), asset.SHA256) {
		return errors.New("The downloaded update does not match the update feed.")
	}
	if !ed25519.Verify(ed25519.PublicKey(key), data, signature) {
		return errors.New("The update signature is invalid.")
	}
	return nil
}

// extractLinuxUpdate copies only <name>/racomi, the executable package-linux.sh
// places in the archive; every other entry is ignored rather than written.
func extractLinuxUpdate(archive []byte, name string, dest io.Writer) error {
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return errors.New("The update archive is invalid.")
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("The update archive has no racomi executable.")
		}
		if err != nil {
			return errors.New("The update archive is invalid.")
		}
		if header.Name != name+"/racomi" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxUpdateBinaryBytes {
			return errors.New("The update archive is invalid.")
		}
		if _, err = io.CopyN(dest, reader, header.Size); err != nil {
			return errors.New("The update archive is invalid.")
		}
		return nil
	}
}

// compareVersions orders SemVer strings, including prerelease identifiers.
// Invalid versions sort below every valid one, so they are never offered.
func compareVersions(left, right string) int {
	l, r := semverPattern.FindStringSubmatch(left), semverPattern.FindStringSubmatch(right)
	switch {
	case l == nil && r == nil:
		return 0
	case l == nil:
		return -1
	case r == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		a, _ := strconv.ParseUint(l[i], 10, 64)
		b, _ := strconv.ParseUint(r[i], 10, 64)
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	if l[4] == r[4] {
		return 0
	}
	if l[4] == "" {
		return 1
	}
	if r[4] == "" {
		return -1
	}
	a, b := strings.Split(l[4], "."), strings.Split(r[4], ".")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		an, aErr := strconv.ParseUint(a[i], 10, 64)
		bn, bErr := strconv.ParseUint(b[i], 10, 64)
		switch {
		case aErr == nil && bErr == nil:
			if an < bn {
				return -1
			}
			return 1
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		case a[i] < b[i]:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}
