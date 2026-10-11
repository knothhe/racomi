//go:build unix

package skills

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSpecialFilesAreRejectedWithoutReading(t *testing.T) {
	store, base := newStore(t)
	source := candidate(t, base, "alpha", "safe")
	if err := syscall.Mkfifo(filepath.Join(source.Path, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := HashDir(source.Path); err == nil {
		t.Fatal("hash accepted FIFO")
	}
	if _, err := store.Install([]Candidate{source}); err == nil {
		t.Fatal("install accepted FIFO")
	}
	dst := filepath.Join(base, "copy")
	if err := CopyDir(source.Path, dst); err == nil {
		t.Fatal("copy accepted FIFO")
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatal("failed copy left partial content", err)
	}
	if len(state(t, store).Skills) != 0 {
		t.Fatal("failed install changed state")
	}
}

func TestCopyPreservesExecutableBitsWithRestrictiveUmask(t *testing.T) {
	base := testDir(t)
	source := candidate(t, base, "alpha", "#!/bin/sh\necho hi\n")
	if err := os.Chmod(filepath.Join(source.Path, "SKILL.md"), 0755); err != nil {
		t.Fatal(err)
	}
	want, err := HashDir(source.Path)
	if err != nil {
		t.Fatal(err)
	}
	previous := syscall.Umask(0077)
	defer syscall.Umask(previous)
	dst := filepath.Join(base, "copy")
	if err := CopyDir(source.Path, dst); err != nil {
		t.Fatal(err)
	}
	got, err := HashDir(dst)
	if err != nil || got != want {
		t.Fatal("umask changed executable content identity", err)
	}
	info, err := os.Stat(filepath.Join(dst, "SKILL.md"))
	if err != nil || info.Mode().Perm()&0111 != 0111 {
		t.Fatal("copy lost executable bits", err)
	}
}
