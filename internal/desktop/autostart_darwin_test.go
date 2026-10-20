package desktop

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAtLoginDisablesCurrentAndLegacyRecords(t *testing.T) {
	for _, labels := range [][]string{{loginLabel}, {legacyLoginLabel}, {loginLabel, legacyLoginLabel}} {
		t.Run(strings.Join(labels, "+"), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := filepath.Join(home, "Library", "LaunchAgents")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, label := range labels {
				if err := os.WriteFile(filepath.Join(dir, label+".plist"), loginPlist("/Applications/racomi.app/Contents/MacOS/racomi"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if !openAtLoginEnabled() {
				t.Fatal("existing login item should remain enabled after upgrading")
			}
			if err := setOpenAtLogin(false); err != nil {
				t.Fatal(err)
			}
			for _, label := range labels {
				if _, err := os.Stat(filepath.Join(dir, label+".plist")); !os.IsNotExist(err) {
					t.Fatalf("login item %s was not removed: %v", label, err)
				}
			}
			if openAtLoginEnabled() {
				t.Fatal("login item should be disabled")
			}
			if err := setOpenAtLogin(false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLoginPlistEscapesExecutableAndRunsOnlyAtLogin(t *testing.T) {
	exe := `/Applications/racomi & "tools".app/Contents/MacOS/racomi`
	b := loginPlist(exe)
	decoder := xml.NewDecoder(bytes.NewReader(b))
	var values []string
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "string" {
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
	}
	if len(values) < 2 || values[0] != loginLabel || values[1] != exe {
		t.Fatal(values)
	}
	if !strings.Contains(string(b), "<key>RunAtLoad</key><true/>") || strings.Contains(string(b), "KeepAlive") {
		t.Fatal("login item should run once without restarting a quit app")
	}
}
