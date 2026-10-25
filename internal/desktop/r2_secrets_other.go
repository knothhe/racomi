//go:build (!darwin && !windows && !linux) || (darwin && !cgo)

package desktop

var errR2SecretStoreMissing = errR2SecretUnavailable

type r2NativeSecretBackend struct{}

func (r2NativeSecretBackend) load(string, string) ([]byte, error) {
	return nil, errR2SecretUnavailable
}

func (r2NativeSecretBackend) save(string, string, []byte) error { return errR2SecretUnavailable }
func (r2NativeSecretBackend) delete(string, string) error       { return errR2SecretUnavailable }
