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

// ERROR_NO_SUCH_LOGON_SESSION: the account has no credential store, as for
// some service and remote sessions.
const r2WinNoLogonSession = windows.Errno(1312)

var errR2SecretStoreMissing = errors.New("Windows Credential Manager is not available for this account.")

type r2NativeSecretBackend struct{}

// r2WinCredItems stores single blobs; r2WinCredentials splits longer values.
type r2WinCredItems struct{}

var r2WinCredentials = chunkedSecretBackend{items: r2WinCredItems{}, limit: r2WinMaxSecretBytes}

func (r2NativeSecretBackend) load(service, key string) ([]byte, error) {
	return r2WinCredentials.load(service, key)
}
func (r2NativeSecretBackend) save(service, key string, value []byte) error {
	return r2WinCredentials.save(service, key, value)
}
func (r2NativeSecretBackend) delete(service, key string) error {
	return r2WinCredentials.delete(service, key)
}

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
	if errors.Is(err, r2WinNoLogonSession) {
		return errR2SecretStoreMissing
	}
	code, ok := err.(syscall.Errno)
	if !ok || code == 0 {
		code = windows.ERROR_INVALID_FUNCTION
	}
	return fmt.Errorf("Windows Credential Manager could not %s racomi credentials (code %d)", operation, uint32(code))
}

func (r2WinCredItems) load(name string) ([]byte, error) {
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, errors.New("invalid racomi credential identifier")
	}
	var credential *r2WinCredential
	result, _, callErr := r2WinCredRead.Call(uintptr(unsafe.Pointer(target)), r2WinGenericCredential, 0, uintptr(unsafe.Pointer(&credential)))
	runtime.KeepAlive(target)
	if result == 0 {
		return nil, r2WinCredentialError("read", callErr)
	}
	if credential == nil {
		return nil, errors.New("invalid saved racomi credentials")
	}
	defer r2WinCredFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential.CredentialBlob == nil || credential.CredentialBlobSize == 0 || credential.CredentialBlobSize > r2WinMaxSecretBytes {
		return nil, errors.New("invalid saved racomi credentials")
	}
	return append([]byte(nil), unsafe.Slice(credential.CredentialBlob, int(credential.CredentialBlobSize))...), nil
}

func (r2WinCredItems) save(name string, value []byte) error {
	if len(value) == 0 || len(value) > r2WinMaxSecretBytes {
		return errors.New("a racomi credential item must contain between 1 and 2560 bytes")
	}
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return errors.New("invalid racomi credential identifier")
	}
	username, _ := windows.UTF16PtrFromString("racomi")
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

func (r2WinCredItems) delete(name string) error {
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return errors.New("invalid racomi credential identifier")
	}
	result, _, callErr := r2WinCredDelete.Call(uintptr(unsafe.Pointer(target)), r2WinGenericCredential, 0)
	runtime.KeepAlive(target)
	if result == 0 {
		return r2WinCredentialError("delete", callErr)
	}
	return nil
}
