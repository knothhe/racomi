package desktop

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"racomi/internal/buildinfo"
	"strings"
)

const (
	stableLoginLabel = "com.knothhe.racomi"
	legacyLoginLabel = "local.racomi.desktop"
)

func loginLabel() string { return buildinfo.Current().ID }

func loginRecord() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", loginLabel()+".plist"), nil
}
func loginExecutable(path string) (string, error) {
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
	decoder := xml.NewDecoder(bytes.NewReader(data))
	found := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if element.Name.Local == "key" {
			var key string
			if err = decoder.DecodeElement(&key, &element); err != nil {
				return "", err
			}
			found = key == "ProgramArguments"
		}
		if found && element.Name.Local == "string" {
			var exe string
			err = decoder.DecodeElement(&exe, &element)
			return exe, err
		}
	}
	return "", errors.New("missing login executable")
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
func removeOwnedLoginRecord(path, exe string) error {
	if path == "" {
		return nil
	}
	actual, err := loginExecutable(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !sameExecutable(actual, exe) {
		return errors.New("login registration belongs to another installation")
	}
	return removeLoginRecord(path)
}
func openAtLoginEnabled() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return openAtLoginEnabledFor(exe)
}
func openAtLoginEnabledFor(exe string) bool {
	path, err := loginRecord()
	if err != nil {
		return false
	}
	records := []string{path}
	if buildinfo.DataDirectoryName() != "racomi-dev" {
		records = append(records, filepath.Join(filepath.Dir(path), legacyLoginLabel+".plist"))
	}
	for _, record := range records {
		actual, err := loginExecutable(record)
		if err == nil && sameExecutable(actual, exe) {
			return true
		}
	}
	return false
}
func loginPlist(exe string) []byte {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(exe))
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string></array>
<key>RunAtLoad</key><true/>
<key>LimitLoadToSessionType</key><string>Aqua</string>
<key>ProcessType</key><string>Interactive</string>
</dict></plist>
`, loginLabel(), escaped.String()))
}
func setOpenAtLogin(on bool) error {
	path, err := loginRecord()
	if err != nil {
		return err
	}
	legacyPath := ""
	if buildinfo.DataDirectoryName() != "racomi-dev" {
		legacyPath = filepath.Join(filepath.Dir(path), legacyLoginLabel+".plist")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !on {
		return errors.Join(removeOwnedLoginRecord(path, exe), removeOwnedLoginRecord(legacyPath, exe))
	}
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return homeErr
	}
	installed := strings.HasPrefix(exe, "/Applications/") || strings.HasPrefix(exe, filepath.Join(home, "Applications")+string(filepath.Separator))
	if !installed || strings.Contains(exe, "/AppTranslocation/") || !strings.HasSuffix(exe, ".app/Contents/MacOS/racomi") {
		return errors.New("Install racomi in Applications before enabling open at login")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Check both registrations before changing either installation's record.
	for _, record := range []string{path, legacyPath} {
		if record == "" {
			continue
		}
		actual, readErr := loginExecutable(record)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if readErr == nil && !sameExecutable(actual, exe) {
			return errors.New("login registration belongs to another installation")
		}
	}
	previous, previousErr := os.ReadFile(path)
	writeRecord := func(data []byte) error {
		stage, err := os.CreateTemp(filepath.Dir(path), ".racomi-login-*")
		if err != nil {
			return err
		}
		defer os.Remove(stage.Name())
		if _, err = stage.Write(data); err != nil {
			stage.Close()
			return err
		}
		if err = stage.Close(); err != nil {
			return err
		}
		return os.Rename(stage.Name(), path)
	}
	if err = writeRecord(loginPlist(exe)); err != nil {
		return err
	}
	if err = removeOwnedLoginRecord(legacyPath, exe); err != nil {
		if previousErr == nil {
			return errors.Join(err, writeRecord(previous))
		}
		return errors.Join(err, removeLoginRecord(path))
	}
	return nil
}

func removeLoginRecord(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
