package backup

import (
	"path/filepath"
	"reflect"
	"testing"

	skillstore "racomi/internal/skills"
)

func TestPortablePresetIconsRoundTripAndLegacyDefault(t *testing.T) {
	for _, icon := range []string{"", "compass", "rocket", "git-branch", "file-text", "sparkles"} {
		t.Run("icon="+icon, func(t *testing.T) {
			store, err := skillstore.Open(filepath.Join(t.TempDir(), "source"))
			if err != nil {
				t.Fatal(err)
			}
			preset, err := store.SavePreset(skillstore.Preset{Name: "portable", Icon: icon})
			if err != nil {
				t.Fatal(err)
			}
			state, err := store.State()
			if err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "presets.zip")
			if err = Export(store.Root(), state, archive); err != nil {
				t.Fatal(err)
			}
			restored, err := skillstore.Open(filepath.Join(t.TempDir(), "restored"))
			if err != nil {
				t.Fatal(err)
			}
			if err = Import(restored, archive); err != nil {
				t.Fatal(err)
			}
			actual, err := restored.State()
			if err != nil || len(actual.Presets) != 1 || actual.Presets[0].Icon != preset.Icon {
				t.Fatalf("portable preset lost its custom/default icon: %+v %v", actual.Presets, err)
			}
		})
	}
}

func TestPortableImportRejectsInvalidPresetIconBeforeRestore(t *testing.T) {
	store, state := fixture(t)
	before, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	state.Presets[0].Icon = "<svg>"
	archive := filepath.Join(t.TempDir(), "invalid-icon.zip")
	if err = Export(store.Root(), state, archive); err != nil {
		t.Fatal(err)
	}
	if err = Import(store, archive); err == nil {
		t.Fatal("portable import accepted an invalid preset icon")
	}
	after, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	afterSnapshots, err := store.Snapshots()
	if err != nil || !reflect.DeepEqual(before, after) || len(afterSnapshots) != len(snapshots) {
		t.Fatalf("invalid preset icon changed local state or snapshots: %v", err)
	}
}
