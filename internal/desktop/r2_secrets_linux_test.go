//go:build linux

package desktop

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/godbus/dbus/v5"
)

// Classification only; TestR2LinuxSecretServiceRoundTrip is the opt-in check
// against the user's real keyring.
func TestR2LinuxSecretServiceErrorClassification(t *testing.T) {
	for _, name := range []string{"org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner", "org.freedesktop.Secret.Error.NoSuchObject"} {
		if err := r2SecretServiceError("read", dbus.Error{Name: name}); !errors.Is(err, errR2SecretServiceUnavailable) {
			t.Fatalf("%s was not reported as a missing keyring: %v", name, err)
		}
	}
	for _, err := range []error{dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"}, dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", nil)} {
		if !errors.Is(r2SecretServiceError("save", err), errR2SecretAccessDenied) {
			t.Fatalf("%v did not stop automatic credential access", err)
		}
	}
	for _, err := range []error{errR2SecretNotFound, errR2SecretAccessDenied, errR2SecretServiceUnavailable} {
		if got := r2SecretServiceError("delete", err); got != err {
			t.Fatalf("sentinel %v was rewritten as %v", err, got)
		}
	}
	other := r2SecretServiceError("read", dbus.Error{Name: "org.freedesktop.DBus.Error.Failed"})
	if errors.Is(other, errR2SecretAccessDenied) || errors.Is(other, errR2SecretServiceUnavailable) {
		t.Fatalf("an unexpected keyring failure was misclassified: %v", other)
	}
}

func TestR2LinuxSecretServiceRoundTrip(t *testing.T) {
	if os.Getenv("RACOMI_TEST_SECRET_SERVICE") != "1" {
		t.Skip("set RACOMI_TEST_SECRET_SERVICE=1 to write a temporary item to the session keyring")
	}
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	store := &r2SystemSecretStore{service: "com.knothhe.racomi.r2-oauth.test-" + hex.EncodeToString(random), backend: r2NativeSecretBackend{}}
	const key = "oauth-roundtrip"
	t.Cleanup(func() { _ = store.Delete(key) })
	if _, err := store.Load(key); !errors.Is(err, errR2SecretNotFound) {
		t.Fatalf("missing item: %v", err)
	}
	for _, value := range [][]byte{[]byte("first\x00secret"), []byte("second")} {
		if err := store.Save(key, value); err != nil {
			t.Fatal(err)
		}
		got, err := store.Load(key)
		if err != nil || !bytes.Equal(got, value) {
			t.Fatalf("load=%q err=%v, want %q", got, err, value)
		}
	}
	if err := store.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(key); !errors.Is(err, errR2SecretNotFound) {
		t.Fatalf("deleted item still loads: %v", err)
	}
	if err := store.Delete(key); err != nil {
		t.Fatalf("deleting a missing item: %v", err)
	}
}
