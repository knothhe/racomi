package desktop

import (
	"os"
	"testing"

	"racomi/internal/desktop/internal/updaterharness"
)

func TestNativeUpdaterStatusUsesJSONBooleans(t *testing.T) {
	// With no controller, the native bridge must still decode successfully.
	// NSNumber boxes a C comparison as an integer unless explicitly made BOOL.
	state := nativeUpdaterState()
	if state.Reason != "" || state.Available || state.Automatic || state.CanCheck {
		t.Fatalf("native updater status did not decode: %+v", state)
	}
}

func TestSparkleUsesRetryableTerminationInsteadOfPostponingInstallation(t *testing.T) {
	// Sparkle consumes its initial Install and Relaunch reply once. Holding that
	// reply across our save handshake strands the button when saving is cancelled.
	// Let Sparkle request normal termination: ShouldQuit protects unsaved edits,
	// and Sparkle retains a separate, repeatable termination callback.
	exists, postpones, _ := updaterharness.DelegateContract()
	if !exists {
		t.Fatal("production Sparkle delegate was not linked")
	}
	if postpones {
		t.Fatal("postponed installation can consume the install button without allowing a retry")
	}
}

func TestSparkleKeepsExplicitProfileRelaunchProtection(t *testing.T) {
	for _, key := range []string{"RACOMI_DATA_DIR", "CFFIXED_USER_HOME"} {
		// Register restoration even if the original environment has no value.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, relaunches := updaterharness.DelegateContract(); !relaunches {
		t.Fatal("ordinary installations must allow Sparkle to relaunch")
	}
	for _, key := range []string{"RACOMI_DATA_DIR", "CFFIXED_USER_HOME"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, t.TempDir())
			if _, _, relaunches := updaterharness.DelegateContract(); relaunches {
				t.Fatal("Launch Services would lose the explicit profile on relaunch")
			}
		})
	}
}
