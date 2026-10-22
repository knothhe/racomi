//go:build !windows

package backup

import (
	"syscall"
	"testing"
)

func restrictUmask(t *testing.T) {
	t.Helper()
	previous := syscall.Umask(0077)
	t.Cleanup(func() { syscall.Umask(previous) })
}
