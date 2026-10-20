package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"racomi/internal/desktop"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

// Keep the bundle's asset catalog in control of macOS light and dark Dock icons.
// A runtime image would replace that catalog with a fixed appearance.
func appIconFor() []byte {
	executable, err := os.Executable()
	if err != nil {
		return appIcon
	}
	return appIconForExecutable(runtime.GOOS, executable)
}

func appIconForExecutable(platform, executable string) []byte {
	if platform == "darwin" {
		macOSDir := filepath.Dir(executable)
		contentsDir := filepath.Dir(macOSDir)
		bundleDir := filepath.Dir(contentsDir)
		if filepath.Base(macOSDir) == "MacOS" && filepath.Base(contentsDir) == "Contents" && filepath.Ext(bundleDir) == ".app" {
			catalog := filepath.Join(contentsDir, "Resources", "Assets.car")
			if info, err := os.Stat(catalog); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				return nil
			}
		}
	}
	return appIcon
}

func main() {
	service, err := desktop.New(os.Getenv("RACOMI_DATA_DIR"))
	if err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name:        "racomi",
		Description: "A lightweight local agent skill manager",
		Icon:        appIconFor(),
		Services:    []application.Service{application.NewService(service)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "com.knothhe.racomi", OnSecondInstanceLaunch: func(application.SecondInstanceData) { service.OpenWindow() }},
	})
	desktop.Attach(service, app)

	desktop.OpenMainWindow(service, app)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
