//go:build linux

package desktop

import (
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// Linux uses the freedesktop Secret Service (GNOME Keyring, KWallet, KeePassXC)
// on the session bus. The "plain" session transfers secrets only over the
// user's own session bus; any process able to read it can already query the
// unlocked keyring.
const (
	r2SecretServiceBus        = "org.freedesktop.secrets"
	r2SecretServicePath       = dbus.ObjectPath("/org/freedesktop/secrets")
	r2SecretServiceIface      = "org.freedesktop.Secret.Service"
	r2SecretPromptTimeout     = 2 * time.Minute
	r2SecretNoPrompt          = dbus.ObjectPath("/")
	r2SecretItemLabelProperty = "org.freedesktop.Secret.Item.Label"
	r2SecretItemAttrsProperty = "org.freedesktop.Secret.Item.Attributes"
)

var errR2SecretStoreMissing = errR2SecretServiceUnavailable

var errR2SecretServiceUnavailable = errors.New("No system keyring is available. Install and unlock a Secret Service keyring such as GNOME Keyring or KWallet, then retry.")

type r2SecretServiceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type r2NativeSecretBackend struct{}

type r2SecretServiceSession struct {
	conn    *dbus.Conn
	service dbus.BusObject
	path    dbus.ObjectPath
}

func r2SecretServiceError(operation string, err error) error {
	if err == nil || errors.Is(err, errR2SecretNotFound) || errors.Is(err, errR2SecretAccessDenied) || errors.Is(err, errR2SecretServiceUnavailable) {
		return err
	}
	name := ""
	var value dbus.Error
	var pointer *dbus.Error
	if errors.As(err, &value) {
		name = value.Name
	} else if errors.As(err, &pointer) {
		name = pointer.Name
	}
	switch name {
	case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner", "org.freedesktop.DBus.Error.UnknownObject",
		"org.freedesktop.Secret.Error.NoSuchObject":
		return errR2SecretServiceUnavailable
	case "org.freedesktop.Secret.Error.IsLocked", "org.freedesktop.DBus.Error.AccessDenied":
		return errR2SecretAccessDenied
	case "":
		return fmt.Errorf("the system keyring could not %s Cloudflare credentials: %v", operation, err)
	}
	return fmt.Errorf("the system keyring could not %s Cloudflare credentials (%s)", operation, name)
}

func openR2SecretService() (*r2SecretServiceSession, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, errR2SecretServiceUnavailable
	}
	service := conn.Object(r2SecretServiceBus, r2SecretServicePath)
	var output dbus.Variant
	var path dbus.ObjectPath
	if err := service.Call(r2SecretServiceIface+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &path); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &r2SecretServiceSession{conn: conn, service: service, path: path}, nil
}

func (s *r2SecretServiceSession) close() {
	_ = s.conn.Object(r2SecretServiceBus, s.path).Call("org.freedesktop.Secret.Session.Close", 0).Err
	_ = s.conn.Close()
}

// prompt runs a keyring dialog. Dismissing it, or leaving it unanswered, counts
// as refusal so background work does not keep reopening the dialog.
func (s *r2SecretServiceSession) prompt(path dbus.ObjectPath) error {
	if path == "" || path == r2SecretNoPrompt {
		return nil
	}
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(path), dbus.WithMatchInterface("org.freedesktop.Secret.Prompt"), dbus.WithMatchMember("Completed")}
	if err := s.conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer s.conn.RemoveMatchSignal(match...)
	signals := make(chan *dbus.Signal, 4)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)
	prompt := s.conn.Object(r2SecretServiceBus, path)
	if err := prompt.Call("org.freedesktop.Secret.Prompt.Prompt", 0, "").Err; err != nil {
		return err
	}
	timer := time.NewTimer(r2SecretPromptTimeout)
	defer timer.Stop()
	for {
		select {
		case signal, ok := <-signals:
			if !ok {
				return errR2SecretServiceUnavailable
			}
			if signal.Path != path || signal.Name != "org.freedesktop.Secret.Prompt.Completed" || len(signal.Body) == 0 {
				continue
			}
			if dismissed, _ := signal.Body[0].(bool); dismissed {
				return errR2SecretAccessDenied
			}
			return nil
		case <-timer.C:
			_ = prompt.Call("org.freedesktop.Secret.Prompt.Dismiss", 0).Err
			return errR2SecretAccessDenied
		}
	}
}

func (s *r2SecretServiceSession) unlock(paths []dbus.ObjectPath) error {
	if len(paths) == 0 {
		return nil
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := s.service.Call(r2SecretServiceIface+".Unlock", 0, paths).Store(&unlocked, &prompt); err != nil {
		return err
	}
	return s.prompt(prompt)
}

func r2SecretServiceAttributes(service, key string) map[string]string {
	return map[string]string{"service": service, "account": key}
}

func (s *r2SecretServiceSession) items(service, key string) ([]dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := s.service.Call(r2SecretServiceIface+".SearchItems", 0, r2SecretServiceAttributes(service, key)).Store(&unlocked, &locked); err != nil {
		return nil, err
	}
	if err := s.unlock(locked); err != nil {
		return nil, err
	}
	return append(unlocked, locked...), nil
}

func withR2SecretService(operation string, run func(*r2SecretServiceSession) error) error {
	session, err := openR2SecretService()
	if err != nil {
		return r2SecretServiceError(operation, err)
	}
	defer session.close()
	return r2SecretServiceError(operation, run(session))
}

func (r2NativeSecretBackend) load(service, key string) ([]byte, error) {
	var value []byte
	err := withR2SecretService("read", func(s *r2SecretServiceSession) error {
		items, err := s.items(service, key)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return errR2SecretNotFound
		}
		var secret r2SecretServiceSecret
		if err := s.conn.Object(r2SecretServiceBus, items[0]).Call("org.freedesktop.Secret.Item.GetSecret", 0, s.path).Store(&secret); err != nil {
			return err
		}
		value = secret.Value
		return nil
	})
	if err != nil {
		clear(value)
		return nil, err
	}
	return value, nil
}

func (r2NativeSecretBackend) save(service, key string, value []byte) error {
	return withR2SecretService("save", func(s *r2SecretServiceSession) error {
		var collection dbus.ObjectPath
		if err := s.service.Call(r2SecretServiceIface+".ReadAlias", 0, "default").Store(&collection); err != nil {
			return err
		}
		if collection == "" || collection == r2SecretNoPrompt {
			return errR2SecretServiceUnavailable
		}
		if err := s.unlock([]dbus.ObjectPath{collection}); err != nil {
			return err
		}
		properties := map[string]dbus.Variant{
			r2SecretItemLabelProperty: dbus.MakeVariant("racomi Cloudflare R2 credentials"),
			r2SecretItemAttrsProperty: dbus.MakeVariant(r2SecretServiceAttributes(service, key)),
		}
		secret := r2SecretServiceSecret{Session: s.path, Parameters: []byte{}, Value: value, ContentType: "application/octet-stream"}
		var item, prompt dbus.ObjectPath
		if err := s.conn.Object(r2SecretServiceBus, collection).Call("org.freedesktop.Secret.Collection.CreateItem", 0, properties, secret, true).Store(&item, &prompt); err != nil {
			return err
		}
		return s.prompt(prompt)
	})
}

func (r2NativeSecretBackend) delete(service, key string) error {
	return withR2SecretService("delete", func(s *r2SecretServiceSession) error {
		items, err := s.items(service, key)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return errR2SecretNotFound
		}
		for _, item := range items {
			var prompt dbus.ObjectPath
			if err := s.conn.Object(r2SecretServiceBus, item).Call("org.freedesktop.Secret.Item.Delete", 0).Store(&prompt); err != nil {
				return err
			}
			if err := s.prompt(prompt); err != nil {
				return err
			}
		}
		return nil
	})
}
