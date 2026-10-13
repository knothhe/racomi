package sources

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

type hostedArchiveEntry struct {
	name string
	size int64
	mode os.FileMode
	dir  bool
}

type hostedArchivePath struct {
	name     string
	dir      bool
	explicit bool
}

// extractHostedArchive accepts a single skill at the archive root, optionally
// wrapped in one directory. The destination is an owned, empty preview directory.
// Validate paths, file types, checksums and budgets before materializing anything.
func extractHostedArchive(ctx context.Context, data []byte, root string) error {
	paths := make(map[string]hostedArchivePath)
	rootSeen := false
	err := walkHostedArchive(ctx, data, func(entry hostedArchiveEntry, _ io.Reader) error {
		name, err := cleanHostedArchivePath(entry.name, entry.dir)
		if err != nil {
			return err
		}
		if name == "." {
			if rootSeen {
				return errors.New("hosted skill archive contains duplicate root entries")
			}
			rootSeen = true
			return nil
		}
		return registerHostedPath(paths, name, entry.dir)
	})
	if err != nil {
		return err
	}
	prefix, err := hostedArchivePrefix(paths)
	if err != nil {
		return err
	}
	destination, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer destination.Close()
	return walkHostedArchive(ctx, data, func(entry hostedArchiveEntry, reader io.Reader) error {
		name, err := cleanHostedArchivePath(entry.name, entry.dir)
		if err != nil {
			return err
		}
		if name == "." || name == prefix {
			return nil
		}
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix+"/")
		}
		if entry.dir {
			return destination.MkdirAll(name, 0755)
		}
		if err := destination.MkdirAll(path.Dir(name), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644) | entry.mode.Perm()&0111
		file, err := destination.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, reader)
		if copyErr == nil {
			// Restore executable permissions even under a restrictive umask.
			copyErr = file.Chmod(mode)
		}
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

// Count implicit parent directories too, and reject file/directory collisions
// before either an archive or a file manifest can touch the filesystem.
func registerHostedPath(paths map[string]hostedArchivePath, name string, directory bool) error {
	parts := strings.Split(name, "/")
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		key := foldHostedArchivePath(current)
		leaf := i == len(parts)-1
		dir := !leaf || directory
		previous, exists := paths[key]
		if exists {
			if previous.name != current || previous.dir != dir || leaf && previous.explicit {
				return fmt.Errorf("hosted skill contains conflicting path %q", name)
			}
			if leaf {
				previous.explicit = true
				paths[key] = previous
			}
			continue
		}
		paths[key] = hostedArchivePath{name: current, dir: dir, explicit: leaf}
		if len(paths) > maxRepositoryEntries {
			return errors.New("hosted skill has too many entries (maximum 20,000)")
		}
	}
	return nil
}

func cleanHostedArchivePath(name string, dir bool) (string, error) {
	unsafe := func() (string, error) {
		return "", fmt.Errorf("unsafe hosted skill archive path %q", name)
	}
	if len(name) > 4096 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00\r\n:") || strings.HasPrefix(name, "/") {
		return unsafe()
	}
	cleaned := strings.TrimPrefix(name, "./")
	if dir {
		cleaned = strings.TrimSuffix(cleaned, "/")
	}
	if dir && (cleaned == "" && name == "./" || cleaned == ".") {
		return ".", nil
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return unsafe()
		}
	}
	return cleaned, nil
}

// SimpleFold catches Unicode case variants as well as ordinary ASCII names.
// Conflict checks are portable even on a case-sensitive preview filesystem.
func foldHostedArchivePath(name string) string {
	return strings.Map(func(r rune) rune {
		folded := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < folded {
				folded = next
			}
		}
		return folded
	}, name)
}

func hostedArchivePrefix(paths map[string]hostedArchivePath) (string, error) {
	if marker, ok := paths[foldHostedArchivePath("SKILL.md")]; ok && marker.name == "SKILL.md" && !marker.dir {
		return "", nil
	}
	wrapper := ""
	for _, entry := range paths {
		top, _, _ := strings.Cut(entry.name, "/")
		if wrapper == "" {
			wrapper = top
		} else if wrapper != top {
			return "", errors.New("hosted skill archive must contain SKILL.md at its root or in one top-level skill directory")
		}
	}
	marker, ok := paths[foldHostedArchivePath(wrapper+"/SKILL.md")]
	if wrapper == "" || !ok || marker.name != wrapper+"/SKILL.md" || marker.dir {
		return "", errors.New("hosted skill archive must contain SKILL.md at its root or in one top-level skill directory")
	}
	return wrapper, nil
}

func walkHostedArchive(ctx context.Context, data []byte, visit func(hostedArchiveEntry, io.Reader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	count := 0
	var total int64
	readEntry := func(entry hostedArchiveEntry, reader io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > maxRepositoryEntries {
			return errors.New("hosted skill archive has too many entries (maximum 20,000)")
		}
		if entry.size < 0 || entry.size > maxRepositoryBytes-total {
			return errors.New("hosted skill content exceeds the 100 MiB preview limit")
		}
		if entry.dir && entry.size != 0 {
			return errors.New("hosted skill archive directory contains file data")
		}
		total += entry.size
		counter := &hostedCountingReader{reader: hostedContextReader{ctx: ctx, reader: reader}}
		bounded := io.LimitReader(counter, entry.size+1)
		if err := visit(entry, bounded); err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, bounded); err != nil {
			return fmt.Errorf("invalid hosted skill archive content: %w", err)
		}
		if counter.bytes != entry.size {
			return errors.New("hosted skill archive file size does not match its header")
		}
		return ctx.Err()
	}
	if bytes.HasPrefix(data, []byte{'P', 'K'}) {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("invalid hosted skill ZIP archive: %w", err)
		}
		if len(archive.File) > maxRepositoryEntries {
			return errors.New("hosted skill archive has too many entries (maximum 20,000)")
		}
		for _, file := range archive.File {
			mode := file.Mode()
			if mode.Type() != 0 && mode.Type() != os.ModeDir {
				return fmt.Errorf("hosted skill archive contains an unsupported file type at %q", file.Name)
			}
			if file.UncompressedSize64 > maxRepositoryBytes {
				return errors.New("hosted skill content exceeds the 100 MiB preview limit")
			}
			reader, err := file.Open()
			if err != nil {
				return fmt.Errorf("invalid hosted skill ZIP entry: %w", err)
			}
			err = readEntry(hostedArchiveEntry{name: file.Name, size: int64(file.UncompressedSize64), mode: mode, dir: mode.IsDir()}, reader)
			closeErr := reader.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return ctx.Err()
	}
	if !bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		return errors.New("hosted skill archive must be a gzip-compressed TAR or ZIP file")
	}
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid hosted skill gzip archive: %w", err)
	}
	defer compressed.Close()
	// Bound TAR padding and extended metadata independently of file contents.
	expanded := &io.LimitedReader{R: hostedContextReader{ctx: ctx, reader: compressed}, N: maxRepositoryBytes + maxTreeBytes + 1}
	archive := tar.NewReader(expanded)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			// Reading to gzip EOF also verifies its checksum and trailer.
			if _, err = io.Copy(io.Discard, expanded); err != nil {
				return fmt.Errorf("invalid hosted skill gzip archive: %w", err)
			}
			if expanded.N == 0 {
				return errors.New("hosted skill archive metadata exceeds the preview limit")
			}
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("invalid hosted skill TAR archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return fmt.Errorf("hosted skill archive contains an unsupported file type at %q", header.Name)
		}
		if err := readEntry(hostedArchiveEntry{name: header.Name, size: header.Size, mode: header.FileInfo().Mode(), dir: header.Typeflag == tar.TypeDir}, archive); err != nil {
			return err
		}
	}
}

type hostedContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader hostedContextReader) Read(p []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(p)
}

type hostedCountingReader struct {
	reader io.Reader
	bytes  int64
}

func (reader *hostedCountingReader) Read(p []byte) (int, error) {
	n, err := reader.reader.Read(p)
	reader.bytes += int64(n)
	return n, err
}
