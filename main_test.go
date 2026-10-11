package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAppIconForExecutable(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		path       string
		catalog    string
		wantNative bool
	}{
		{"native catalog", "darwin", "racomi.app/Contents/MacOS/racomi", "file", true},
		{"missing catalog", "darwin", "racomi.app/Contents/MacOS/racomi", "", false},
		{"empty catalog", "darwin", "racomi.app/Contents/MacOS/racomi", "empty", false},
		{"directory is not catalog", "darwin", "racomi.app/Contents/MacOS/racomi", "directory", false},
		{"bare executable", "darwin", "bin/racomi", "file", false},
		{"nonbundle directory", "darwin", "racomi/Contents/MacOS/racomi", "file", false},
		{"other platform", "linux", "racomi.app/Contents/MacOS/racomi", "file", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), tt.path)
			catalog := filepath.Join(filepath.Dir(executable), "..", "Resources", "Assets.car")
			if err := os.MkdirAll(filepath.Dir(catalog), 0o755); err != nil {
				t.Fatal(err)
			}
			switch tt.catalog {
			case "file", "empty":
				var contents []byte
				if tt.catalog == "file" {
					contents = []byte("catalog fixture")
				}
				if err := os.WriteFile(catalog, contents, 0o644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(catalog, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := appIconForExecutable(tt.platform, executable)
			if tt.wantNative {
				if got != nil {
					t.Fatal("bundle catalog must retain native light/dark icon rendering")
				}
			} else if len(got) == 0 || !bytes.Equal(got, appIcon) {
				t.Fatal("expected the embedded fallback icon")
			}
		})
	}
}
