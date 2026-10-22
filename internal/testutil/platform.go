// Package testutil provides platform capabilities used by integration fixtures.
package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// Symlink skips only when Windows denies the privilege; all other errors remain
// test failures. Developer Mode/elevated Windows and Unix run the full tests.
func Symlink(t *testing.T, oldname, newname string) error {
	t.Helper()
	err := os.Symlink(oldname, newname)
	if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
		t.Skip("symbolic links require Windows Developer Mode or elevation")
	}
	return err
}

func RequireSymlink(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	root := t.TempDir()
	if err := Symlink(t, root, filepath.Join(root, "probe")); err != nil {
		t.Fatal(err)
	}
}
