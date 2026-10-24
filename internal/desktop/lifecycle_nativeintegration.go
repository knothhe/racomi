//go:build nativeintegration

package desktop

import "fmt"

// NativeFrontendReady observes the real frontend handshake in native harnesses.
// It does not acknowledge a lifecycle request or alter production behavior.
func NativeFrontendReady(s *Service) bool {
	s.lifecycle.mu.Lock()
	defer s.lifecycle.mu.Unlock()
	return s.lifecycle.ready && s.lifecycle.present
}

// NativeLifecycleStatus supplies non-sensitive timeout diagnostics to harnesses.
func NativeLifecycleStatus(s *Service) string {
	s.lifecycle.mu.Lock()
	defer s.lifecycle.mu.Unlock()
	l := &s.lifecycle
	pending := "none"
	if l.pending != nil {
		pending = fmt.Sprintf("%s/%d", l.pending.action, l.pending.id)
	}
	return fmt.Sprintf("ready=%t present=%t running=%t page=%d pending=%s", l.ready, l.present, l.running, l.page, pending)
}
