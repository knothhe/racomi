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
	r := s.r2
	r.mu.Lock()
	defer r.mu.Unlock()
	index := -1
	for i, connection := range r.state.Connections {
		if connection.ID == connectionID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tokens, err := r.tokensLocked(ctx)
	if err != nil {
		return err
	}
	connection := r.state.Connections[index]
	if err = r.client.DeleteToken(ctx, tokens.AccessToken, connection.AccountID, connection.TokenID); err != nil {
		return err
	}
	key := connection.CredentialKey
	if key == "" {
		key = "connection-" + connectionID
	}
	if err = r.secrets.Delete(key); err != nil {
		return err
	}
	next := r.state
	next.Connections = append(append([]r2Connection{}, next.Connections[:index]...), next.Connections[index+1:]...)
	return r.saveLocked(next)
}
