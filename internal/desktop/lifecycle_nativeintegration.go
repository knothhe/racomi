//go:build nativeintegration

package desktop

// NativeFrontendReady observes the real frontend handshake in native harnesses.
// It does not acknowledge a lifecycle request or alter production behavior.
func NativeFrontendReady(s *Service) bool {
	s.lifecycle.mu.Lock()
	defer s.lifecycle.mu.Unlock()
	return s.lifecycle.ready && s.lifecycle.present
}
