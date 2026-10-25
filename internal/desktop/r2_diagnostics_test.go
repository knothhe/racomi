package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"racomi/internal/cloudflare"
)

func waitR2DiagnosticClose(t *testing.T, d *r2DiagnosticLog) []r2DiagnosticEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		d.mu.Lock()
		data, err := os.ReadFile(d.path)
		d.mu.Unlock()
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		var events []r2DiagnosticEvent
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var event r2DiagnosticEvent
			if err := json.Unmarshal(line, &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
			if event.Stage == "listener_closed" {
				return events
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("callback listener did not close")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type r2DiagnosticCleanupFailureStore struct{ r2SecretStore }

func (r2DiagnosticCleanupFailureStore) Delete(string) error {
	return errors.New("synthetic private cleanup failure")
}

func TestR2DiagnosticsTerminalResults(t *testing.T) {
	for _, test := range []struct {
		name, result string
		connected    bool
	}{
		{name: "completed", result: "completed", connected: true},
		{name: "cleanup warning", result: "completed_with_cleanup_warning", connected: true},
		{name: "denied", result: "failed"},
		{name: "canceled", result: "canceled"},
		{name: "unexpected listener close", result: "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _, _, urls := r2TestService(t)
			d := newR2DiagnosticLog(t.TempDir(), true)
			s.r2.diagnostics = d
			if test.name == "cleanup warning" {
				s.r2.secrets = r2DiagnosticCleanupFailureStore{s.r2.secrets}
			}
			callback, auth := beginR2Authorization(t, s, urls)
			switch test.name {
			case "canceled":
				s.CancelR2OAuth()
			case "unexpected listener close":
				s.r2.mu.Lock()
				listener := s.r2.listener
				s.r2.mu.Unlock()
				_ = listener.Close()
			default:
				query := url.Values{"state": {auth.Get("state")}, "code": {"synthetic-code"}}
				if test.name == "denied" {
					query = url.Values{"state": {auth.Get("state")}, "error": {"access_denied"}}
				}
				if status, err := r2Callback(t, callback, query); err != nil || status != 200 {
					t.Fatalf("callback: status=%d err=%v", status, err)
				}
			}
			var finished, listenerErrors int
			for _, event := range waitR2DiagnosticClose(t, d) {
				if event.Stage == "finished" {
					finished++
					if event.Result != test.result {
						t.Fatalf("terminal result=%q, want %q", event.Result, test.result)
					}
				}
				if event.Stage == "listener_error" {
					listenerErrors++
				}
			}
			if finished != 1 || (listenerErrors > 0) != (test.name == "unexpected listener close") {
				t.Fatalf("unexpected terminal events: finished=%d listenerErrors=%d", finished, listenerErrors)
			}
			status := s.GetR2OAuthStatus()
			if status.Authorizing || status.Connected != test.connected || (test.name == "canceled" && status.LastError != "") {
				t.Fatalf("unexpected authorization status: %+v", status)
			}
		})
	}
}

func TestR2DiagnosticsTimeoutIsDistinctFromCancellation(t *testing.T) {
	s, _, _, _ := r2TestService(t)
	d := newR2DiagnosticLog(t.TempDir(), true)
	s.r2.diagnostics = d
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	s.r2.mu.Lock()
	s.r2.generation++
	generation := s.r2.generation
	s.r2.authorizing = true
	s.r2.mu.Unlock()
	go s.serveR2OAuth(ctx, listener, "localhost:1", "/callback", "synthetic-state", "synthetic-verifier", s.r2.redirectURI, generation)
	var finished int
	for _, event := range waitR2DiagnosticClose(t, d) {
		if event.Stage == "finished" {
			finished++
			if event.Result != "timeout" {
				t.Fatalf("timeout recorded as %q", event.Result)
			}
		}
		if event.Stage == "listener_error" {
			t.Fatal("timeout recorded an unexpected listener failure")
		}
	}
	if finished != 1 || s.GetR2OAuthStatus().LastError == "" {
		t.Fatal("timeout did not retain one distinct diagnostic result and its status error")
	}
}

func TestR2DiagnosticsTraceCallbackWithoutSecrets(t *testing.T) {
	s, _, _, urls := r2TestService(t)
	d := newR2DiagnosticLog(t.TempDir(), true)
	s.r2.diagnostics = d
	s.r2.client.Diagnostic = func(event cloudflare.DiagnosticEvent) {
		d.write(r2DiagnosticEvent{Stage: "cloudflare_api", API: &event})
	}
	callback, auth := beginR2Authorization(t, s, urls)
	const rejectedState = "private-invalid-state-must-not-appear"
	const privateCode = "private-code-must-not-appear"
	status, err := r2Callback(t, callback, url.Values{"state": {rejectedState}, "code": {privateCode}})
	if err != nil || status != 400 {
		t.Fatalf("invalid callback: status=%d err=%v", status, err)
	}
	status, err = r2Callback(t, callback, url.Values{"state": {auth.Get("state")}, "code": {privateCode}})
	if err != nil || status != 200 {
		t.Fatalf("valid callback: status=%d err=%v", status, err)
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{rejectedState, privateCode, auth.Get("state"), auth.Get("code_challenge"), "synthetic-oauth-access", "synthetic-oauth-refresh", "synthetic-racomi-client", callback.String(), "code_verifier"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("diagnostics included private authorization material")
		}
	}
	stages := map[string]bool{}
	var invalid, valid bool
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var event r2DiagnosticEvent
		if err = json.Unmarshal(line, &event); err != nil {
			t.Fatal("invalid diagnostic JSON:", err)
		}
		if event.Time == "" || event.PID == 0 {
			t.Fatal("diagnostics are missing correlation metadata")
		}
		stages[event.Stage] = true
		if event.Stage == "callback_received" && event.StateMatches != nil {
			invalid = invalid || !*event.StateMatches
			valid = valid || *event.StateMatches
		}
	}
	for _, stage := range []string{"listen_started", "browser_opened", "callback_received", "callback_rejected", "code_exchange_started", "code_exchange_finished", "cloudflare_api", "authorization_saved"} {
		if !stages[stage] {
			t.Fatalf("missing diagnostic stage %s", stage)
		}
	}
	if !invalid || !valid {
		t.Fatal("logs do not distinguish invalid and valid callback state")
	}
	info, err := os.Stat(d.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("diagnostic file permissions: %v", err)
	}
}

func TestR2DiagnosticsAreDevOnlyBoundedAndRejectSymlinks(t *testing.T) {
	root := t.TempDir()
	disabled := newR2DiagnosticLog(root, false)
	disabled.write(r2DiagnosticEvent{Stage: "initialized"})
	if _, err := os.Stat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatal("disabled diagnostic logger wrote a file")
	}
	d := newR2DiagnosticLog(root, true)
	d.write(r2DiagnosticEvent{Stage: "initialized"})
	if err := os.WriteFile(d.path, []byte(strings.Repeat("x", r2DiagnosticMaxBytes)), 0600); err != nil {
		t.Fatal(err)
	}
	d.write(r2DiagnosticEvent{Stage: "start_requested"})
	if info, err := os.Stat(d.path + ".1"); err != nil || info.Size() != r2DiagnosticMaxBytes || info.Mode().Perm() != 0600 {
		t.Fatalf("diagnostic rotation failed: %v", err)
	}
	if info, err := os.Stat(d.path); err != nil || info.Size() >= r2DiagnosticMaxBytes {
		t.Fatalf("diagnostic file remains unbounded: %v", err)
	}
	if err := os.Remove(d.path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "do-not-change")
	if err := os.WriteFile(target, []byte("preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, d.path); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	d.write(r2DiagnosticEvent{Stage: "start_requested"})
	if data, err := os.ReadFile(target); err != nil || string(data) != "preserved" {
		t.Fatal("diagnostic logger followed a symlink")
	}
}
