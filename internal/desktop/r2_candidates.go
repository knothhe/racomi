package desktop

import (
	"context"
	"time"
)

// DiscardR2Connection releases an abandoned setup, never a saved profile. Saves
// share runMu, so closing an old editor cannot revoke a just-committed bucket.
func (s *Service) DiscardR2Connection(ctx context.Context, connectionID string) error {
	if connectionID == "" || s.cloud == nil || s.r2 == nil {
		return nil
	}
	c := s.cloud
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.mu.Lock()
	retained := c.r2Config.R2ConnectionID == connectionID || c.config.R2ConnectionID == connectionID
	c.mu.Unlock()
	if retained {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.r2.manager.Discard(ctx, connectionID)
}
