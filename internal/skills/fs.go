package skills

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HashDir identifies a directory by its relative paths, file contents and
// executable permission bits. Symlinks and non-regular files are rejected.
func HashDir(path string) (string, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path = canonicalSystemPath(path)
	if err = noSymlinks(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("not a regular directory: %s", path)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	h := sha256.New()
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsupported file: %s", name)
		}
		binary.Write(h, binary.BigEndian, uint64(len(name)))
		io.WriteString(h, name)
		if info.IsDir() {
			h.Write([]byte{'d'})
			return nil
		}
		h.Write([]byte{'f'})
		binary.Write(h, binary.BigEndian, uint32(info.Mode().Perm()&0111))
		binary.Write(h, binary.BigEndian, uint64(info.Size()))
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		current, err := file.Stat()
		if err == nil && (!current.Mode().IsRegular() || !os.SameFile(info, current)) {
			err = fmt.Errorf("file changed while reading: %s", name)
		}
		if err == nil {
			var n int64
			n, err = io.Copy(h, file)
			if err == nil && n != info.Size() {
				err = fmt.Errorf("file changed while reading: %s", name)
			}
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CopyDir copies a tree to a new directory, preserving executable bits. It
// never merges into or replaces an existing destination.
func CopyDir(src, dst string) (err error) {
	src, err = filepath.Abs(src)
	if err != nil {
		return err
	}
	dst, err = filepath.Abs(dst)
	if err != nil {
		return err
	}
	src, dst = canonicalSystemPath(src), canonicalSystemPath(dst)
	if within(src, dst) || within(dst, src) {
		return errors.New("copy source and destination overlap")
	}
	if err = noSymlinks(src); err != nil {
		return err
	}
	if err = noSymlinks(filepath.Dir(dst)); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("copy source must be a directory")
	}
	source, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer source.Close()
	if err = os.Mkdir(dst, 0755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dst)
		}
	}()
	destination, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	return fs.WalkDir(source.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		info, err := source.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported: %s", name)
		}
		if info.IsDir() {
			return destination.Mkdir(name, 0755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file: %s", name)
		}
		input, err := source.Open(name)
		if err != nil {
			return err
		}
		defer input.Close()
		current, err := input.Stat()
		if err != nil {
			return err
		}
		if !current.Mode().IsRegular() || !os.SameFile(info, current) {
			return fmt.Errorf("file changed while copying: %s", name)
		}
		output, err := destination.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644|info.Mode().Perm()&0111)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(output, input)
		if copyErr == nil {
			copyErr = output.Chmod(0644 | info.Mode().Perm()&0111)
		}
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != info.Size() {
			return fmt.Errorf("file changed while copying: %s", name)
		}
		return nil
	})
}

func within(parent, path string) bool {
	// macOS and Windows commonly use case-insensitive volumes. Reject lexical
	// overlaps conservatively even on a case-sensitive volume on those hosts.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		parent, path = strings.ToLower(parent), strings.ToLower(path)
	}
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// macOS ships these directory aliases. Resolve only the known system mapping;
// arbitrary user-created symlinks must still be rejected for target paths.
func canonicalSystemPath(path string) string {
	if runtime.GOOS != "darwin" {
		return path
	}
	for _, alias := range []string{"/tmp", "/var", "/etc"} {
		if path != alias && !strings.HasPrefix(path, alias+"/") {
			continue
		}
		resolved, err := filepath.EvalSymlinks(alias)
		if err == nil && resolved == "/private"+alias {
			return resolved + strings.TrimPrefix(path, alias)
		}
	}
	return path
}

// Check every existing component, including ancestors of a not-yet-created
// target. An existing symlink is never an acceptable application destination.
func noSymlinks(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absolute = canonicalSystemPath(absolute)
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(absolute, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path contains a symlink: %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("path component is not a directory: %s", current)
		}
	}
	return nil
}

func atomicJSON(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular state file: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".racomi-json-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
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
	return os.Rename(name, path)
}
