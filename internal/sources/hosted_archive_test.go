package sources

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hostedArchiveTestEntry struct {
	name     string
	body     string
	mode     os.FileMode
	tarType  byte
	linkname string
}

func makeHostedTestArchive(t *testing.T, format string, entries []hostedArchiveTestEntry) []byte {
	t.Helper()
	var data bytes.Buffer
	if format == "zip" {
		writer := zip.NewWriter(&data)
		for _, entry := range entries {
			header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
			header.SetMode(entry.mode)
			file, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(file, entry.body); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	compressed := gzip.NewWriter(&data)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		kind := entry.tarType
		if kind == 0 {
			kind = tar.TypeReg
			if entry.mode.IsDir() {
				kind = tar.TypeDir
			}
		}
		header := &tar.Header{Name: entry.name, Mode: int64(entry.mode.Perm()), Size: int64(len(entry.body)), Typeflag: kind, Linkname: entry.linkname}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestExtractHostedArchiveLayoutsAndAssets(t *testing.T) {
	for _, format := range []string{"tar.gz", "zip"} {
		for _, wrapper := range []string{"", "./", "lark-doc/", "./lark-doc/"} {
			t.Run(format+"/"+wrapper, func(t *testing.T) {
				entries := []hostedArchiveTestEntry{
					{name: wrapper + "scripts/run.sh", body: "#!/bin/sh\nexit 0\n", mode: 0751},
					{name: wrapper + "SKILL.md", body: "---\nname: lark-doc\ndescription: Read documents\n---\n", mode: 0644},
					{name: wrapper + "assets/guide.txt", body: "supporting reference", mode: 0644},
					// Explicit directories may occur after their child entries.
					{name: wrapper + "assets/", mode: os.ModeDir | 0700},
					{name: wrapper + "empty/", mode: os.ModeDir | 0755},
				}
				if wrapper != "" {
					entries = append(entries, hostedArchiveTestEntry{name: wrapper, mode: os.ModeDir | 0755})
				}
				root := t.TempDir()
				if err := extractHostedArchive(context.Background(), makeHostedTestArchive(t, format, entries), root); err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries[:3] {
					name := strings.TrimPrefix(entry.name, wrapper)
					body, err := os.ReadFile(filepath.Join(root, name))
					if err != nil || string(body) != entry.body {
						t.Fatalf("%s: body %q, error %v", name, body, err)
					}
				}
				info, err := os.Stat(filepath.Join(root, "scripts", "run.sh"))
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatalf("executable permissions: %v, error %v", info, err)
				}
				info, err = os.Stat(filepath.Join(root, "empty"))
				if err != nil || !info.IsDir() {
					t.Fatalf("empty directory: %v, error %v", info, err)
				}
				if wrapper != "" {
					if _, err = os.Stat(filepath.Join(root, "lark-doc")); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("wrapper remained after extraction: %v", err)
					}
				}
			})
		}
	}
}

func TestExtractHostedArchiveRejectsUnsafePathsAndConflicts(t *testing.T) {
	cases := map[string][]hostedArchiveTestEntry{
		"parent traversal":     {{name: "../outside"}},
		"nested traversal":     {{name: "assets/../outside"}},
		"backslash":            {{name: "assets\\outside"}},
		"absolute":             {{name: "/outside"}},
		"windows volume":       {{name: "C:/outside"}},
		"git directory":        {{name: "nested/.GiT/config"}},
		"dot component":        {{name: "assets/./outside"}},
		"empty component":      {{name: "assets//outside"}},
		"oversized path":       {{name: strings.Repeat("a/", 2048) + "file"}},
		"duplicate file":       {{name: "assets/file"}, {name: "assets/file"}},
		"duplicate directory":  {{name: "assets/", mode: os.ModeDir}, {name: "assets/", mode: os.ModeDir}},
		"case variant":         {{name: "assets/File"}, {name: "assets/file"}},
		"unicode case variant": {{name: "assets/σ"}, {name: "assets/ς"}},
		"parent case variant":  {{name: "Assets/one"}, {name: "assets/two"}},
		"file used as parent":  {{name: "assets"}, {name: "assets/child"}},
		"parent used as file":  {{name: "assets/child"}, {name: "assets"}},
	}
	for _, format := range []string{"tar.gz", "zip"} {
		for name, entries := range cases {
			t.Run(format+"/"+name, func(t *testing.T) {
				// Even a valid preceding file must remain unwritten on failure.
				entries = append([]hostedArchiveTestEntry{{name: "SKILL.md", body: "valid marker", mode: 0644}}, entries...)
				root := t.TempDir()
				if err := extractHostedArchive(context.Background(), makeHostedTestArchive(t, format, entries), root); err == nil {
					t.Fatal("unsafe archive accepted")
				}
				contents, err := os.ReadDir(root)
				if err != nil || len(contents) != 0 {
					t.Fatalf("invalid archive was materialized: %v, %v", contents, err)
				}
			})
		}
	}
}

func TestExtractHostedArchiveRejectsUnsupportedTypes(t *testing.T) {
	for name, kind := range map[string]byte{"symlink": tar.TypeSymlink, "hard link": tar.TypeLink, "device": tar.TypeChar, "FIFO": tar.TypeFifo} {
		t.Run("tar/"+name, func(t *testing.T) {
			data := makeHostedTestArchive(t, "tar.gz", []hostedArchiveTestEntry{{name: "SKILL.md", body: "marker"}, {name: "link", tarType: kind, linkname: "SKILL.md"}})
			if err := extractHostedArchive(context.Background(), data, t.TempDir()); err == nil {
				t.Fatal("unsupported entry type accepted")
			}
		})
	}
	for name, mode := range map[string]os.FileMode{"symlink": os.ModeSymlink, "device": os.ModeDevice, "FIFO": os.ModeNamedPipe, "socket": os.ModeSocket} {
		t.Run("zip/"+name, func(t *testing.T) {
			data := makeHostedTestArchive(t, "zip", []hostedArchiveTestEntry{{name: "SKILL.md", body: "marker"}, {name: "link", mode: mode}})
			if err := extractHostedArchive(context.Background(), data, t.TempDir()); err == nil {
				t.Fatal("unsupported entry type accepted")
			}
		})
	}
}

func TestExtractHostedArchiveRejectsInvalidLayout(t *testing.T) {
	for _, format := range []string{"tar.gz", "zip"} {
		for _, entries := range [][]hostedArchiveTestEntry{
			{},
			{{name: "README.md"}},
			{{name: "one/SKILL.md"}, {name: "two/asset"}},
			{{name: "one/nested/SKILL.md"}},
			{{name: "SKILL.md/", mode: os.ModeDir}},
			{{name: "skill.md"}},
		} {
			if err := extractHostedArchive(context.Background(), makeHostedTestArchive(t, format, entries), t.TempDir()); err == nil {
				t.Fatalf("%s: invalid layout accepted: %+v", format, entries)
			}
		}
	}
}

func TestExtractHostedArchiveRejectsOversizedContent(t *testing.T) {
	var data bytes.Buffer
	compressed := gzip.NewWriter(&data)
	writer := tar.NewWriter(compressed)
	if err := writer.WriteHeader(&tar.Header{Name: "SKILL.md", Typeflag: tar.TypeReg, Size: maxRepositoryBytes + 1}); err != nil {
		t.Fatal(err)
	}
	// An oversized header is rejected before its missing body is read.
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	err := extractHostedArchive(context.Background(), data.Bytes(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "100 MiB") {
		t.Fatalf("oversized file: %v", err)
	}
	zipData := makeHostedTestArchive(t, "zip", []hostedArchiveTestEntry{{name: "SKILL.md"}})
	central := bytes.Index(zipData, []byte{'P', 'K', 1, 2})
	if central < 0 {
		t.Fatal("ZIP central directory missing")
	}
	binary.LittleEndian.PutUint32(zipData[central+24:], maxRepositoryBytes+1)
	err = extractHostedArchive(context.Background(), zipData, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "100 MiB") {
		t.Fatalf("oversized ZIP file: %v", err)
	}
}

func TestExtractHostedArchiveRejectsTooManyEntries(t *testing.T) {
	entries := []hostedArchiveTestEntry{{name: "SKILL.md"}}
	for i := 0; i < maxRepositoryEntries; i++ {
		entries = append(entries, hostedArchiveTestEntry{name: fmt.Sprintf("file-%05d", i)})
	}
	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			err := extractHostedArchive(context.Background(), makeHostedTestArchive(t, format, entries), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "20,000") {
				t.Fatalf("too many entries: %v", err)
			}
		})
	}
}

func TestExtractHostedArchiveChecksArchiveIntegrity(t *testing.T) {
	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			data := makeHostedTestArchive(t, format, []hostedArchiveTestEntry{{name: "SKILL.md", body: "marker", mode: 0644}})
			if format == "tar.gz" {
				data[len(data)-8] ^= 1 // Corrupt the gzip checksum after TAR EOF.
			} else {
				central := bytes.Index(data, []byte{'P', 'K', 1, 2})
				if central < 0 {
					t.Fatal("ZIP central directory missing")
				}
				data[central+16] ^= 1 // Corrupt the central directory's content CRC.
			}
			root := t.TempDir()
			if err := extractHostedArchive(context.Background(), data, root); err == nil {
				t.Fatal("archive with corrupt checksum accepted")
			}
			contents, err := os.ReadDir(root)
			if err != nil || len(contents) != 0 {
				t.Fatalf("corrupt archive was materialized: %v, %v", contents, err)
			}
		})
	}
}

func TestExtractHostedArchiveCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, format := range []string{"tar.gz", "zip"} {
		data := makeHostedTestArchive(t, format, []hostedArchiveTestEntry{{name: "SKILL.md", body: "marker"}})
		if err := extractHostedArchive(ctx, data, t.TempDir()); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: cancellation was not preserved: %v", format, err)
		}
	}
}
