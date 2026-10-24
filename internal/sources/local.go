package sources

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PreviewLocal snapshots ordinary skill directories without invoking Git.
// Installation reads this owned cache, so later edits to the selected directory
// cannot silently change the content the user reviewed.
func (c *Client) PreviewLocal(ctx context.Context, path string) (Preview, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return Preview{}, errors.New("local skill directory must be an absolute path")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Preview{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Preview{}, err
	}
	if !info.IsDir() {
		return Preview{}, errors.New("local skill directory is not a directory")
	}
	// Bound discovery before metadata parsing or hashing large local trees.
	var size int64
	entries := 0
	if err = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > maxRepositoryEntries {
			return errors.New("local skill directory has too many entries (maximum 20,000)")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			size += info.Size()
			if size > maxRepositoryBytes {
				return errors.New("local skill content exceeds the 100 MiB preview limit")
			}
		}
		return nil
	}); err != nil {
		return Preview{}, err
	}
	items, skipped, warnings, err := scanSkills(path, ".", "", nil, true)
	if err != nil {
		return Preview{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		return Preview{}, err
	}
	id := hex.EncodeToString(idBytes)
	root := filepath.Join(c.cacheDir, "previews", id)
	if err = os.MkdirAll(root, 0700); err != nil {
		return Preview{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(root)
		}
	}()
	var copiedBytes int64
	copiedEntries := 0
	covered := []string{}
	for _, item := range items {
		alreadyCopied := false
		for _, parent := range covered {
			alreadyCopied = alreadyCopied || repositoryPathWithin(parent, item.Subpath)
		}
		if !alreadyCopied {
			if err = copyLocalSkill(ctx, filepath.Join(path, filepath.FromSlash(item.Subpath)), filepath.Join(root, filepath.FromSlash(item.Subpath)), &copiedBytes, &copiedEntries); err != nil {
				return Preview{}, err
			}
			covered = append(covered, item.Subpath)
		}
		cached, readErr := readSkill(root, item.Subpath, "")
		if readErr != nil {
			return Preview{}, readErr
		}
		if cached.ContentHash != item.ContentHash {
			return Preview{}, errors.New("local skill directory changed while preparing preview")
		}
	}
	result := Preview{ID: id, Local: true, Source: path, Subpath: ".", Candidates: items, Skipped: skipped, Warnings: warnings}
	c.previews[id] = previewRecord{preview: result, root: root}
	keep = true
	return result, nil
}

// copyLocalSkill bounds both the copied byte count and a file that grows while
// being read. Root handles keep replacements confined to the selected tree.
func copyLocalSkill(ctx context.Context, src, dst string, size *int64, entries *int) error {
	if repositoryPathWithin(filepath.ToSlash(src), filepath.ToSlash(dst)) || repositoryPathWithin(filepath.ToSlash(dst), filepath.ToSlash(src)) {
		return errors.New("copy source and destination overlap")
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("local skill directory is not a directory")
	}
	source, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer source.Close()
	if err = os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	destination, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	return fs.WalkDir(source.FS(), ".", func(name string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil || name == "." {
			return walkErr
		}
		*entries++
		if *entries > maxRepositoryEntries {
			return errors.New("local skill directory has too many entries (maximum 20,000)")
		}
		info, err := source.Lstat(name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return destination.Mkdir(name, 0755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file: %s", name)
		}
		if info.Size() > maxRepositoryBytes-*size {
			return errors.New("local skill content exceeds the 100 MiB preview limit")
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
			return errors.New("local skill directory changed while preparing preview")
		}
		output, err := destination.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644|info.Mode().Perm()&0111)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(output, io.LimitReader(input, maxRepositoryBytes-*size+1))
		if copyErr == nil {
			copyErr = output.Chmod(0644 | info.Mode().Perm()&0111)
		}
		closeErr := output.Close()
		*size += n
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if *size > maxRepositoryBytes {
			return errors.New("local skill content exceeds the 100 MiB preview limit")
		}
		if n != info.Size() {
			return errors.New("local skill directory changed while preparing preview")
		}
		return nil
	})
}
