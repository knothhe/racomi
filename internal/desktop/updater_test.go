package desktop

import (
	"racomi/internal/buildinfo"
	"testing"
)

func TestUpdaterRejectsDevLocalDirtyAndUnsupportedBuilds(t *testing.T) {
	originalFeed, originalKey := buildinfo.FeedURL, buildinfo.PublicKey
	defer func() { buildinfo.FeedURL, buildinfo.PublicKey = originalFeed, originalKey }()
	buildinfo.FeedURL = "https://example.test/appcast.xml"
	buildinfo.PublicKey = "test-key"
	valid := buildinfo.Info{Channel: "stable", Executor: "ci", OS: "darwin", Arch: "arm64"}
	if reason := updateEligibility(valid); reason != "" {
		t.Fatal(reason)
	}
	for _, change := range []func(*buildinfo.Info){func(i *buildinfo.Info) { i.Channel = "dev" }, func(i *buildinfo.Info) { i.Channel = "preview" }, func(i *buildinfo.Info) { i.Executor = "local" }, func(i *buildinfo.Info) { i.Dirty = true }, func(i *buildinfo.Info) { i.OS = "windows" }, func(i *buildinfo.Info) { i.Arch = "amd64" }} {
		info := valid
		change(&info)
		if updateEligibility(info) == "" {
			t.Fatalf("ineligible build accepted: %+v", info)
		}
	}
	buildinfo.PublicKey = ""
	if updateEligibility(valid) == "" {
		t.Fatal("missing trust root accepted")
	}
}
