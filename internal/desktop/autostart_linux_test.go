package desktop

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"racomi/internal/buildinfo"
	"strings"
	"testing"
)

func TestLinuxAutostartEntryIsOwnedByExactExecutable(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	record := filepath.Join(config, "autostart", buildinfo.Current().ID+".desktop")
	// go test binaries live in a temporary build directory and must be refused.
	if err := setOpenAtLogin(true); err == nil || !strings.Contains(err.Error(), "Install racomi") {
		t.Fatalf("temporary executable registered: %v", err)
	}
	entry, err := autostartEntry(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(record, entry, 0o644); err != nil {
		t.Fatal(err)
	}
	if !openAtLoginEnabledFor(exe) || !openAtLoginEnabled() {
		t.Fatal("own registration not recognised")
	}
	if err = setOpenAtLogin(false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("own registration not removed")
	}

	other, _ := autostartEntry("/opt/other/racomi")
	if err = os.WriteFile(record, other, 0o644); err != nil {
		t.Fatal(err)
	}
	if openAtLoginEnabled() {
		t.Fatal("another installation's registration adopted")
	}
	if err = setOpenAtLogin(false); err == nil {
		t.Fatal("another installation's registration removed")
	}
	if err = os.WriteFile(record, []byte("[Desktop Entry]\nExec=\""+exe+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = setOpenAtLogin(false); err == nil {
		t.Fatal("unmarked desktop entry removed")
	}
	if _, err = os.Stat(record); err != nil {
		t.Fatal("foreign entry must be preserved")
	}
	for _, path := range []string{`/opt/ra"comi`, "/opt/$HOME/racomi", "/opt/100%/racomi", "/opt/a\\b"} {
		if _, err := autostartEntry(path); err == nil {
			t.Fatalf("unsafe Exec path accepted: %s", path)
		}
	}
	if autostartInstalled(filepath.Join(os.TempDir(), "go-build1", "exe", "racomi")) || !autostartInstalled("/home/user/.local/bin/racomi") {
		t.Fatal("installation location check")
	}
}

func TestTrayIconIsSmallAndOpaqueWhereSourceIs(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "build", "appicon.png"))
	if err != nil {
		t.Fatal(err)
	}
	icon := trayIcon(source)
	decoded, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != image.Rect(0, 0, trayIconSize, trayIconSize) || len(icon) > 64<<10 {
		t.Fatalf("tray icon %v, %d bytes", decoded.Bounds(), len(icon))
	}
	_, _, _, alpha := decoded.At(trayIconSize/2, trayIconSize/2).RGBA()
	if alpha == 0 {
		t.Fatal("icon centre lost its artwork")
	}
	if trayIcon(nil) != nil || !bytes.Equal(trayIcon([]byte("not png")), []byte("not png")) {
		t.Fatal("non-image icon handling")
	}
}
