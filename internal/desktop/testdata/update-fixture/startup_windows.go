package main

import (
	"encoding/json"
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	marker := os.Getenv("RACOMI_TEST_UPDATE_MARKER")
	if marker == "" || len(os.Args) != 1 {
		return
	}
	var startup windows.StartupInfo
	if windows.GetStartupInfo(&startup) != nil {
		return
	}
	data, _ := json.Marshal(struct {
		Hidden bool `json:"hidden"`
	}{startup.Flags&windows.STARTF_USESHOWWINDOW != 0 && startup.ShowWindow == windows.SW_HIDE})
	_ = os.WriteFile(marker+".startup.json", data, 0o600)
}
