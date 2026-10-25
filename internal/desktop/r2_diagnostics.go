package desktop

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"racomi/internal/cloudflare"
)

const r2DiagnosticMaxBytes = 512 << 10

// Only explicit stage names, booleans and the Cloudflare client's sanitized
// events cross this boundary. Never accept an error, URL, query or response body.
type r2DiagnosticEvent struct {
	Time              string                      `json:"time"`
	PID               int                         `json:"pid"`
	Stage             string                      `json:"stage"`
	Generation        uint64                      `json:"generation,omitempty"`
	Result            string                      `json:"result,omitempty"`
	ClientConfigured  *bool                       `json:"clientConfigured,omitempty"`
	MethodMatches     *bool                       `json:"methodMatches,omitempty"`
	HostMatches       *bool                       `json:"hostMatches,omitempty"`
	PathMatches       *bool                       `json:"pathMatches,omitempty"`
	StatePresent      *bool                       `json:"statePresent,omitempty"`
	StateMatches      *bool                       `json:"stateMatches,omitempty"`
	CodePresent       *bool                       `json:"codePresent,omitempty"`
	OAuthErrorPresent *bool                       `json:"oauthErrorPresent,omitempty"`
	OAuthError        string                      `json:"oauthError,omitempty"`
	API               *cloudflare.DiagnosticEvent `json:"api,omitempty"`
}

type r2DiagnosticLog struct {
	mu   sync.Mutex
	path string
	warn sync.Once
}

func newR2DiagnosticLog(root string, enabled bool) *r2DiagnosticLog {
	if !enabled {
		return nil
	}
	return &r2DiagnosticLog{path: filepath.Join(root, "logs", "cloudflare-oauth.jsonl")}
}

func diagnosticBool(value bool) *bool { return &value }

// r2OAuthErrorCode keeps only the standard OAuth error codes; any other
// callback value is recorded as "other" so arbitrary text never reaches the log.
func r2OAuthErrorCode(value string) string {
	switch value {
	case "":
		return ""
	case "invalid_request", "unauthorized_client", "access_denied", "unsupported_response_type", "invalid_scope", "server_error", "temporarily_unavailable", "invalid_state":
		return value
	}
	return "other"
}

func (d *r2DiagnosticLog) write(event r2DiagnosticEvent) {
	if d == nil {
		return
	}
	event.Time, event.PID = time.Now().UTC().Format(time.RFC3339Nano), os.Getpid()
	data, err := json.Marshal(event)
	if err == nil {
		d.mu.Lock()
		err = d.append(append(data, '\n'))
		d.mu.Unlock()
	}
	if err != nil {
		d.warn.Do(func() { log.Print("Cloudflare OAuth diagnostic log could not be written.") })
	}
}

func (d *r2DiagnosticLog) append(data []byte) error {
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid diagnostic directory")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return err
	}
	if info, err = os.Lstat(d.path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("invalid diagnostic file")
		}
		if err = os.Chmod(d.path, 0600); err != nil {
			return err
		}
		if info.Size()+int64(len(data)) > r2DiagnosticMaxBytes {
			if err = os.Rename(d.path, d.path+".1"); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(d.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	return err
}
