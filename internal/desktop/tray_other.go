//go:build !linux && !windows

package desktop

// macOS reopens a lightweight window from its Dock icon.
func (s *Service) syncTray() {}
