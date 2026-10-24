package desktop

import "testing"

func TestNativeUpdaterStatusUsesJSONBooleans(t *testing.T) {
	// With no controller, the native bridge must still decode successfully.
	// NSNumber boxes a C comparison as an integer unless explicitly made BOOL.
	state := nativeUpdaterState()
	if state.Reason != "" || state.Available || state.Automatic || state.CanCheck {
		t.Fatalf("native updater status did not decode: %+v", state)
	}
}
