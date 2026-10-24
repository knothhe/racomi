package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"racomi/internal/buildinfo"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const windowsRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartInstalled(exe string) bool {
	temp := strings.ToLower(filepath.Clean(os.TempDir())) + string(filepath.Separator)
	return filepath.IsAbs(exe) && !strings.HasPrefix(strings.ToLower(exe), temp) && !strings.Contains(strings.ToLower(exe), `\go-build`)
}

func loginCommand(exe string) string { return `"` + filepath.Clean(exe) + `"` }

func openAtLoginEnabled() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(buildinfo.Current().ID)
	return err == nil && strings.EqualFold(value, loginCommand(exe))
}

func setOpenAtLogin(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return setWindowsLogin(key, buildinfo.Current().ID, exe, on)
}

// The value name separates Dev and release. Its exact quoted path prevents a
// second installation from adopting or deleting the first installation's entry.
func setWindowsLogin(key registry.Key, name, exe string, on bool) error {
	value, _, err := key.GetStringValue(name)
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	if err == nil && !strings.EqualFold(value, loginCommand(exe)) {
		return errors.New("login registration belongs to another installation")
	}
	if !on {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return key.DeleteValue(name)
	}
	if !autostartInstalled(exe) {
		return errors.New("Install racomi before enabling open at login")
	}
	if strings.ContainsAny(exe, "\"\n\r") {
		return errors.New("Invalid login executable path")
	}
	return key.SetStringValue(name, loginCommand(exe))
}
