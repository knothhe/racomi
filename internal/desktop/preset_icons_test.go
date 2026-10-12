package desktop

import (
	"testing"

	skillstore "racomi/internal/skills"
)

func TestPresetIconFlowsThroughDesktopState(t *testing.T) {
	service, _, _ := applicationService(t)
	app, err := service.SavePreset(skillstore.Preset{Name: "base", Icon: "database"})
	if err != nil || len(app.State.Presets) != 1 || app.State.Presets[0].Icon != "database" {
		t.Fatalf("preset icon did not reach the desktop state: %+v %v", app.State.Presets, err)
	}
	preset := app.State.Presets[0]
	preset.Icon = ""
	app, err = service.SavePreset(preset)
	if err != nil || app.State.Presets[0].Icon != "" {
		t.Fatalf("preset icon did not reset to the layers default: %+v %v", app.State.Presets, err)
	}
	preset.Icon = "<svg>"
	if _, err = service.SavePreset(preset); err == nil {
		t.Fatal("desktop save accepted an invalid preset icon")
	}
	app, err = service.GetState()
	if err != nil || app.State.Presets[0].Icon != "" {
		t.Fatalf("failed save changed preset metadata: %+v %v", app.State.Presets, err)
	}
}
