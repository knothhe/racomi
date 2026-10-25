package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestWindowsLoginRegistrationOwnership(t *testing.T) {
	// A private key exercises the actual registry without changing login startup.
	path := `Software\RacomiTests\` + filepath.Base(t.TempDir())
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { key.Close(); registry.DeleteKey(registry.CURRENT_USER, path) }()
	exe := `C:\Users\User Name\应用\racomi.exe`
	name := "com.knothhe.racomi"
	if err = setWindowsLogin(key, name, exe, true); err != nil {
		t.Fatal(err)
	}
	value, _, err := key.GetStringValue(name)
	if err != nil || value != loginCommand(exe) {
		t.Fatal(value, err)
	}
	if err = setWindowsLogin(key, name, `C:\Other\racomi.exe`, false); err == nil {
		t.Fatal("foreign registration removed")
	}
	if err = setWindowsLogin(key, name, `C:\Other\racomi.exe`, true); err == nil {
		t.Fatal("foreign registration replaced")
	}
	if err = setWindowsLogin(key, name, exe, false); err != nil {
		t.Fatal(err)
	}
	if err = setWindowsLogin(key, name, exe, false); err != nil {
		t.Fatal(err)
	}
	if err = setWindowsLogin(key, name, filepath.Join(os.TempDir(), "go-build", "racomi.exe"), true); err == nil {
		t.Fatal("temporary build registered")
	}
}

func TestWindowsFontsIncludeUsableStyles(t *testing.T) {
	fonts, err := listInterfaceFonts()
	if err != nil || len(fonts) == 0 {
		t.Fatal("font catalog", err)
	}
	for _, font := range fonts {
		if font.Family == "" || len(font.Styles) == 0 {
			t.Fatal(font)
		}
		for _, style := range font.Styles {
			if style.Name == "" || style.PostscriptName == "" {
				t.Fatal(style)
			}
		}
	}
}
