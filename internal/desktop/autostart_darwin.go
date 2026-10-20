package desktop

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	loginLabel       = "com.knothhe.racomi"
	legacyLoginLabel = "local.racomi.desktop"
)

func loginRecord() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", loginLabel+".plist"), nil
}
func openAtLoginEnabled() bool {
	path, err := loginRecord()
	if err != nil {
		return false
	}
	for _, record := range []string{path, filepath.Join(filepath.Dir(path), legacyLoginLabel+".plist")} {
		info, err := os.Stat(record)
		if err == nil && info.Mode().IsRegular() {
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
`, loginLabel, escaped.String()))
}
func setOpenAtLogin(on bool) error {
	path, err := loginRecord()
	if err != nil {
		return err
	}
	legacyPath := filepath.Join(filepath.Dir(path), legacyLoginLabel+".plist")
	if !on {
		return errors.Join(removeLoginRecord(path), removeLoginRecord(legacyPath))
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(exe, "/AppTranslocation/") || !strings.Contains(exe, ".app/Contents/MacOS/") {
		return errors.New("Install racomi in Applications before enabling open at login")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err = os.WriteFile(path+".tmp", loginPlist(exe), 0o600); err != nil {
		return err
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	return removeLoginRecord(legacyPath)
}

func removeLoginRecord(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
