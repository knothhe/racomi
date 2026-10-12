package cloudsync

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"racomi/internal/skills"
)

const maxArchiveBytes = 64 << 20
const maxExpandedBytes = 128 << 20
const maxArchiveEntries = 10000

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxArchiveBytes {
		return 0, errors.New("skill archive exceeds 64 MiB")
	}
	return b.Buffer.Write(data)
}

func archiveObject(directory, expected string) ([]byte, error) {
	actual, err := skills.HashDir(directory)
	if err != nil {
		return nil, err
	}
	if actual != expected {
		return nil, errors.New("local skill object failed content verification")
	}
	var buffer boundedBuffer
	writer := zip.NewWriter(&buffer)
	entries := 0
	var total int64
	err = filepath.WalkDir(directory, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if full == directory {
			return nil
		}
		entries++
		if entries > maxArchiveEntries {
			return errors.New("skill contains more than 10,000 entries")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("unsupported skill archive entry")
		}
		name, err := filepath.Rel(directory, full)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		header.Method = zip.Deflate
		if info.IsDir() {
			header.Name += "/"
			header.Method = zip.Store
		} else {
			total += info.Size()
			if total > maxExpandedBytes {
				return errors.New("skill exceeds 128 MiB expanded size")
			}
		}
		output, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		input, err := os.Open(full)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(output, io.LimitReader(input, info.Size()+1))
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != info.Size() {
			return errors.New("skill changed while archiving")
		}
		return nil
	})
	if err != nil {
		writer.Close()
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	actual, err = skills.HashDir(directory)
	if err != nil || actual != expected {
		return nil, errors.New("local skill changed while archiving")
	}
	return buffer.Bytes(), nil
}

func safeArchiveName(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\x00:") || strings.HasPrefix(name, "/") || len(name) > 4096 {
		return false
	}
	trimmed := strings.TrimSuffix(name, "/")
	return trimmed != "" && path.Clean(trimmed) == trimmed && trimmed != "." && trimmed != ".." && !strings.HasPrefix(trimmed, "../")
}
func extractObject(data []byte, directory, expected string) error {
	if len(data) > maxArchiveBytes {
		return errors.New("skill archive exceeds 64 MiB")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("invalid remote skill archive")
	}
	if len(reader.File) > maxArchiveEntries {
		return errors.New("skill archive contains too many entries")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := map[string]bool{}
	var total uint64
	for _, entry := range reader.File {
		if !safeArchiveName(entry.Name) {
			return errors.New("unsafe path in remote skill archive")
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if seen[name] {
			return errors.New("duplicate path in remote skill archive")
		}
		seen[name] = true
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsDir() && !mode.IsRegular()) {
			return errors.New("unsupported entry in remote skill archive")
		}
		if mode.IsDir() != strings.HasSuffix(entry.Name, "/") {
			return errors.New("invalid archive directory entry")
		}
		total += entry.UncompressedSize64
		if entry.UncompressedSize64 > maxExpandedBytes || total > maxExpandedBytes {
			return errors.New("remote skill archive exceeds 128 MiB expanded size")
		}
		if mode.IsDir() {
			if err = root.MkdirAll(name, 0755); err != nil {
				return err
			}
			continue
		}
		if err = root.MkdirAll(path.Dir(name), 0755); err != nil {
			return err
		}
		input, err := entry.Open()
		if err != nil {
			return errors.New("cannot read remote skill archive")
		}
		output, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return err
		}
		n, copyErr := io.Copy(output, io.LimitReader(input, maxExpandedBytes+1))
		input.Close()
		if copyErr == nil && uint64(n) != entry.UncompressedSize64 {
			copyErr = errors.New("remote skill archive length mismatch")
		}
		if copyErr == nil {
			copyErr = output.Chmod(0644 | mode.Perm()&0111)
		}
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	actual, err := skills.HashDir(directory)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("remote skill object failed content verification: %s", expected)
	}
	return nil
}
