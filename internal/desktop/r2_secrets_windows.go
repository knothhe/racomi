//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	r2WinGenericCredential = 1
	r2WinPersistLocal      = 2
	r2WinMaxSecretBytes    = 5 * 512
)

type r2NativeSecretBackend struct{}

// Layout follows CREDENTIALW from wincred.h. Secrets are passed to the native
// API in memory; they never enter a process command line or the clipboard.
type r2WinCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         unsafe.Pointer
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	r2WinCredLibrary = windows.NewLazySystemDLL("advapi32.dll")
	r2WinCredRead    = r2WinCredLibrary.NewProc("CredReadW")
	r2WinCredWrite   = r2WinCredLibrary.NewProc("CredWriteW")
	r2WinCredDelete  = r2WinCredLibrary.NewProc("CredDeleteW")
	r2WinCredFree    = r2WinCredLibrary.NewProc("CredFree")
)

func r2WinCredentialError(operation string, err error) error {
	if errors.Is(err, windows.ERROR_NOT_FOUND) {
		return errR2SecretNotFound
	}
	code, ok := err.(syscall.Errno)
	if !ok || code == 0 {
		code = windows.ERROR_INVALID_FUNCTION
	}
	return fmt.Errorf("Windows Credential Manager could not %s Cloudflare credentials (code %d)", operation, uint32(code))
}

func (r2NativeSecretBackend) load(service, key string) ([]byte, error) {
	target, err := windows.UTF16PtrFromString(service + "/" + key)
	if err != nil {
		return nil, errors.New("invalid Cloudflare credential identifier")
	}
	var credential *r2WinCredential
	result, _, callErr := r2WinCredRead.Call(uintptr(unsafe.Pointer(target)), r2WinGenericCredential, 0, uintptr(unsafe.Pointer(&credential)))
	runtime.KeepAlive(target)
	if result == 0 {
		return nil, r2WinCredentialError("read", callErr)
	}
	if credential == nil {
		return nil, errors.New("invalid saved Cloudflare credentials")
	}
	defer r2WinCredFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential.CredentialBlob == nil || credential.CredentialBlobSize == 0 || credential.CredentialBlobSize > r2WinMaxSecretBytes {
		return nil, errors.New("invalid saved Cloudflare credentials")
	}
	return append([]byte(nil), unsafe.Slice(credential.CredentialBlob, int(credential.CredentialBlobSize))...), nil
}

func (r2NativeSecretBackend) save(service, key string, value []byte) error {
	if len(value) > r2WinMaxSecretBytes {
		return errors.New("Cloudflare credentials exceed the Windows Credential Manager limit of 2560 bytes")
	}
	target, err := windows.UTF16PtrFromString(service + "/" + key)
	if err != nil {
		return errors.New("invalid Cloudflare credential identifier")
	}
	username, _ := windows.UTF16PtrFromString("racomi Cloudflare R2")
	credential := r2WinCredential{Type: r2WinGenericCredential, TargetName: target,
		CredentialBlobSize: uint32(len(value)), CredentialBlob: &value[0], Persist: r2WinPersistLocal, UserName: username}
	result, _, callErr := r2WinCredWrite.Call(uintptr(unsafe.Pointer(&credential)), 0)
	runtime.KeepAlive(value)
	runtime.KeepAlive(credential)
	if result == 0 {
		return r2WinCredentialError("save", callErr)
	}
	return nil
}

func (r2NativeSecretBackend) delete(service, key string) error {
	target, err := windows.UTF16PtrFromString(service + "/" + key)
	if err != nil {
		return errors.New("invalid Cloudflare credential identifier")
	}
	result, _, callErr := r2WinCredDelete.Call(uintptr(unsafe.Pointer(target)), r2WinGenericCredential, 0)
	runtime.KeepAlive(target)
	if result == 0 {
		return r2WinCredentialError("delete", callErr)
	}
	return nil
}
