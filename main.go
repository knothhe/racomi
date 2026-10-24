package main

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"racomi/internal/buildinfo"
	"racomi/internal/desktop"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

//go:embed build/dev-icons/app-icon-dev.png
var devIcon []byte

// Keep the bundle's asset catalog in control of macOS light and dark Dock icons.
// A runtime image would replace that catalog with a fixed appearance.
func appIconFor() []byte {
	if buildinfo.DataDirectoryName() == "racomi-dev" {
		return devIcon
	}
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
	if len(os.Args) == 2 && os.Args[1] == "--build-info" {
		_ = json.NewEncoder(os.Stdout).Encode(buildinfo.Current())
		return
	}
	if desktop.RunUpdateHelper() {
		return
	}
	// A process relaunched by the Linux updater waits for its predecessor to
	// release the single-instance name before application.New claims it.
	desktop.WaitForUpdatedInstance()
	info := buildinfo.Current()
	service := desktop.NewApplicationService(os.Getenv("RACOMI_DATA_DIR"))
	app := application.New(application.Options{
		Name:         info.Name,
		ErrorHandler: func(err error) { log.Print(err) },
		ShouldQuit:   service.ShouldQuit,
		Description:  "A lightweight local agent skill manager",
		Icon:         appIconFor(),
		Services:     []application.Service{application.NewService(service)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		// The id doubles as the Wayland app_id, matching the installed desktop entry.
		// Closing the window decides whether to quit, as on macOS: a released
		// lightweight window must leave the app running.
		Linux:          application.LinuxOptions{ApplicationID: info.ID, DisableQuitOnLastWindowClosed: true},
		Windows:        application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: singleInstanceID(info.ID, os.Getenv("RACOMI_DATA_DIR")), OnSecondInstanceLaunch: func(application.SecondInstanceData) { service.OpenWindow() }},
	})
	desktop.UseIcon(service, appIconFor())
	desktop.Attach(service, app)

	desktop.OpenMainWindow(service, app)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
	desktop.RelaunchAfterUpdate()
}

// Explicit data profiles need separate locks so an isolated verification app
// cannot route a launch into the user's everyday application.
func singleInstanceID(identity, root string) string {
	if root == "" {
		return identity
	}
	canonical := func(value string) string {
		if resolved, err := filepath.EvalSymlinks(value); err == nil {
			value = resolved
		}
		if absolute, err := filepath.Abs(value); err == nil {
			value = absolute
		}
		return filepath.Clean(value)
	}
	root = canonical(root)
	if base, err := os.UserConfigDir(); err == nil && root == canonical(filepath.Join(base, buildinfo.DataDirectoryName())) {
		return identity
	}
	digest := sha256.Sum256([]byte(root))
	// Linux claims this as a D-Bus name, whose elements may not start with a digit.
	return fmt.Sprintf("%s.profile.p%x", identity, digest[:12])
}
