package desktop

import (
	"strings"
	"testing"
)

func TestNativeLocaleAndSettingsPersistIndependently(t *testing.T) {
	service, _, _ := applicationService(t)
	for _, locale := range []struct{ code, word string }{{"en", "desktop"}, {"zh-CN", "桌面应用"}, {"zh-TW", "桌面應用程式"}} {
		if err := service.SetLocale(locale.code); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ChooseDirectory(); err == nil || !strings.Contains(err.Error(), locale.word) {
			t.Fatalf("native dialog error did not use %s: %v", locale.code, err)
		}
	}
	if err := service.SetLocale("fr"); err == nil {
		t.Fatal("unsupported locale was accepted")
	}
	app, err := service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	settings := app.State.Settings
	settings.DeploymentMode = "symlink"
	settings.DefaultProjectDirectories = []string{"claude", "codex"}
	if _, err = service.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(service.store.Root())
	if err != nil {
		t.Fatal(err)
	}
	app, err = reopened.GetState()
	if err != nil || app.State.Settings.DeploymentMode != "symlink" || len(app.State.Settings.DefaultProjectDirectories) != 2 {
		t.Fatalf("service settings were not persisted: %+v %v", app, err)
	}
}
