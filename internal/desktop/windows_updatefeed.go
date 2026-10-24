package desktop

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The wire format and Ed25519 verification match the Linux feed, while archive
// names and extraction are explicitly pinned to Windows releases.
func windowsArchiveName(version, arch string) (string, error) {
	if arch != "amd64" {
		return "", fmt.Errorf("unsupported Windows architecture: %s", arch)
	}
	return "racomi-" + version + "-x86_64-windows.zip", nil
}

func parseWindowsUpdateFeed(data []byte, arch string) (linuxUpdateFeed, linuxUpdateAsset, error) {
	var feed linuxUpdateFeed
	if len(data) > maxUpdateFeedBytes {
		return feed, linuxUpdateAsset{}, errors.New("The update feed is too large.")
	}
	if json.Unmarshal(data, &feed) != nil || !semverPattern.MatchString(feed.Version) || !commitPattern.MatchString(feed.Commit) || strings.Contains(feed.Version, "-") {
		return feed, linuxUpdateAsset{}, errors.New("The update feed is invalid.")
	}
	asset, ok := feed.Assets[arch]
	if !ok {
		return feed, asset, errors.New("No update is published for this architecture.")
	}
	name, err := windowsArchiveName(feed.Version, arch)
	if err != nil {
		return feed, asset, err
	}
	if asset.URL != releaseDownloadBase+"/v"+feed.Version+"/"+name || asset.Length <= 0 || asset.Length > maxUpdateArchiveBytes || len(asset.SHA256) != 64 || asset.EdSignature == "" {
		return feed, asset, errors.New("The update feed is invalid.")
	}
	return feed, asset, nil
}

func extractWindowsUpdate(data []byte, dest io.Writer) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("The update archive is invalid.")
	}
	var executable *zip.File
	for _, file := range archive.File {
		if file.Name != "racomi.exe" {
			continue
		}
		if executable != nil || !file.Mode().IsRegular() || file.UncompressedSize64 == 0 || file.UncompressedSize64 > maxUpdateBinaryBytes {
			return errors.New("The update archive is invalid.")
		}
		executable = file
	}
	if executable == nil {
		return errors.New("The update archive has no racomi executable.")
	}
	reader, err := executable.Open()
	if err != nil {
		return errors.New("The update archive is invalid.")
	}
	defer reader.Close()
	written, err := io.Copy(dest, io.LimitReader(reader, maxUpdateBinaryBytes+1))
	if err != nil || written != int64(executable.UncompressedSize64) {
		return errors.New("The update archive is invalid.")
	}
	return nil
}
