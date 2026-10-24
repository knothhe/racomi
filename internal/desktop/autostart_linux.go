package desktop

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"racomi/internal/buildinfo"
	"strings"
)

// XDG autostart entries are read by the session at login. The entry carries the
// exact executable, so a second installation never adopts or removes it.
const autostartMarker = "X-Racomi-Autostart=true"

func autostartRecord() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "autostart", buildinfo.Current().ID+".desktop"), nil
}

func autostartExecutable(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return "", errors.New("invalid login registration")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	marked, exe := false, ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == autostartMarker {
			marked = true
		}
		if value, ok := strings.CutPrefix(line, "Exec="); ok && len(value) > 1 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			exe = value[1 : len(value)-1]
		}
	}
	if !marked {
		return "", errors.New("login registration belongs to another application")
	}
	if exe == "" {
		return "", errors.New("missing login executable")
	}
	return exe, nil
}

func sameExecutable(left, right string) bool {
	if path, err := filepath.EvalSymlinks(left); err == nil {
		left = path
	}
	if path, err := filepath.EvalSymlinks(right); err == nil {
		right = path
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func openAtLoginEnabled() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return openAtLoginEnabledFor(exe)
}

func openAtLoginEnabledFor(exe string) bool {
	path, err := autostartRecord()
	if err != nil {
		return false
	}
	actual, err := autostartExecutable(path)
	return err == nil && sameExecutable(actual, exe)
}

// Desktop entry Exec values reinterpret these characters; a path containing
// them could launch something else, so registration is refused instead.
func autostartEntry(exe string) ([]byte, error) {
	if strings.ContainsAny(exe, "\"`$\\%\n\r") {
		return nil, errors.New("The racomi executable path contains characters a login entry cannot represent")
	}
	return []byte(fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Exec="%s"
TryExec=%s
Icon=%s
Terminal=false
X-GNOME-Autostart-enabled=true
%s
`, buildinfo.Current().Name, exe, exe, buildinfo.Current().ID, autostartMarker)), nil
}

// A go run or test binary lives in a temporary build directory that is gone by
// the next login.
func autostartInstalled(exe string) bool {
	temp := filepath.Clean(os.TempDir()) + string(filepath.Separator)
	return filepath.IsAbs(exe) && !strings.HasPrefix(exe, temp) && !strings.Contains(exe, string(filepath.Separator)+"go-build")
}

func setOpenAtLogin(on bool) error {
	path, err := autostartRecord()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	actual, readErr := autostartExecutable(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if readErr == nil && !sameExecutable(actual, exe) {
		return errors.New("login registration belongs to another installation")
	}
	if !on {
		if os.IsNotExist(readErr) {
			return nil
		}
		return os.Remove(path)
	}
	if !autostartInstalled(exe) {
		return errors.New("Install racomi before enabling open at login")
	}
	entry, err := autostartEntry(exe)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(path), ".racomi-login-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	if _, err = stage.Write(entry); err != nil {
		stage.Close()
		return err
	}
	if err = stage.Chmod(0o644); err != nil {
		stage.Close()
		return err
	}
	if err = stage.Close(); err != nil {
		return err
	}
	return os.Rename(stage.Name(), path)
}
