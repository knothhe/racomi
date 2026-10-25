package desktop

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// Archive names and extraction are pinned to Windows releases; the feed and
// signature checks are shared with Linux in updatefeed.go.
func windowsArchiveName(version, arch string) (string, error) {
	if arch != "amd64" {
		return "", fmt.Errorf("unsupported Windows architecture: %s", arch)
	}
	return "racomi-" + version + "-x86_64-windows.zip", nil
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
