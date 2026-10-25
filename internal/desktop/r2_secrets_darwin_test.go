//go:build darwin && cgo

package desktop

import (
	"errors"
	"testing"
)

// These are Security.framework OSStatus values. No test calls the real Keychain.
func TestR2MacOSKeychainRefusalClassification(t *testing.T) {
	for _, status := range []int{-128, -25293, -25308} {
		var err error
		switch status {
		case -128:
			err = r2KeychainError("read", -128)
		case -25293:
			err = r2KeychainError("save", -25293)
		case -25308:
			err = r2KeychainError("delete", -25308)
		}
		if !errors.Is(err, errR2SecretAccessDenied) {
			t.Fatalf("macOS status %d did not stop automatic credential access", status)
		}
	}
	if !errors.Is(r2KeychainError("read", -25300), errR2SecretNotFound) {
		t.Fatal("a missing credential was classified as refusal")
	}
	if errors.Is(r2KeychainError("read", -25291), errR2SecretAccessDenied) {
		t.Fatal("an unavailable Keychain was classified as user refusal")
	}
}
