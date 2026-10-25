package desktop

import "C"

//export racomiUpdateAborted
func racomiUpdateAborted() {
	if updaterService != nil {
		updaterService.lifecycle.mu.Lock()
		updaterService.lifecycle.running = false
		updaterService.lifecycle.mu.Unlock()
		updaterService.lifecycle.permit.Store(false)
		updaterService.lifecycle.stopping.Store(false)
		updaterService.app.Event.Emit("racomi:lifecycle-resumed")
	}
}
