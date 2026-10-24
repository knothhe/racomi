//go:build !darwin && !linux

package desktop

func nativeStartUpdater(*Service) {}
func nativeUpdaterState() UpdaterState {
	return UpdaterState{Reason: "The desktop updater is unavailable."}
}
func nativeCheckUpdates()            {}
func nativeSetAutomaticUpdates(bool) {}

func RelaunchAfterUpdate()    {}
func WaitForUpdatedInstance() {}
