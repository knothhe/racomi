// Package buildinfo is the single runtime identity for a built application.
package buildinfo

import "runtime"

// Values are injected by Make. An uninjected go run is always a Dev build.
var (
	Version  = "development"
	Number   = "0"
	Commit   = "unknown"
	Dirty    = "true"
	Channel  = "dev"
	Executor = "local"
)

const Repository = "https://github.com/knothhe/racomi"

type Info struct {
	Name       string `json:"name"`
	ID         string `json:"id"`
	Version    string `json:"version"`
	Number     string `json:"number"`
	Commit     string `json:"commit"`
	Dirty      bool   `json:"dirty"`
	Channel    string `json:"channel"`
	Executor   string `json:"executor"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Repository string `json:"repository"`
}

func Current() Info {
	name, id, channel := "racomi Dev", "com.knothhe.racomi.dev", "dev"
	if Channel == "stable" || Channel == "preview" {
		name, id, channel = "racomi", "com.knothhe.racomi", Channel
	}
	return Info{name, id, Version, Number, Commit, Dirty != "false", channel, Executor, runtime.GOOS, runtime.GOARCH, Repository}
}

func DataDirectoryName() string {
	if Channel == "stable" || Channel == "preview" {
		return "racomi"
	}
	return "racomi-dev"
}

// The stable release embeds these from build/updates.json; Dev has no feed.
var FeedURL, PublicKey string
