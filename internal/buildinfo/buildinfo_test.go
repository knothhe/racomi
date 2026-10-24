package buildinfo

import "testing"

func TestChannelsHaveIndependentRuntimeIdentity(t *testing.T) {
	previous := Channel
	defer func() { Channel = previous }()
	Channel = "dev"
	dev := Current()
	devData := DataDirectoryName()
	Channel = "stable"
	stable := Current()
	if dev.ID == stable.ID || dev.Name == stable.Name || devData == DataDirectoryName() {
		t.Fatal("Dev shares stable identity or data")
	}
	Channel = "invalid"
	if Current().ID != dev.ID || DataDirectoryName() != devData {
		t.Fatal("unknown channel must never fall back to stable")
	}
}
