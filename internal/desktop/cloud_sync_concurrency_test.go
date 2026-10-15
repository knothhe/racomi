package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	skillstore "racomi/internal/skills"
)

func stalledCloudService(t *testing.T) (*Service, <-chan struct{}, func()) {
	t.Helper()
	root := t.TempDir()
	requested := make(chan struct{}, 1)
	released := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requested <- struct{}{}:
		default:
		}
		select {
		case <-released:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(func() { release(); server.Close() })
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	config := cloudFixtureConfig()
	config.Endpoint, config.AutoSync = server.URL, true
	if _, err = s.SaveCloudSyncConfig(config); err != nil {
		t.Fatal(err)
	}
	return s, requested, release
}

func saveProjectWhileCloudStalled(t *testing.T, s *Service, release func()) {
	t.Helper()
	root := t.TempDir()
	type saved struct {
		state AppState
		err   error
	}
	done := make(chan saved, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		state, err := s.SaveProject(skillstore.Target{Name: "Local project", Path: root})
		done <- saved{state, err}
	}()
	t.Cleanup(func() {
		release()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("project save did not finish after cloud request was released")
		}
	})
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if len(result.state.Projects) != 1 || result.state.Projects[0].ProjectRoot != applicationPath(root) {
			t.Fatal("project was not persisted while cloud I/O was pending")
		}
		if !result.state.CloudSync.Syncing {
			t.Fatal("project save did not finish until the cloud request ended")
		}
	case <-time.After(time.Second):
		t.Fatal("project save blocked on cloud network I/O")
	}
}

func TestStartupCloudSyncDoesNotBlockProjectSave(t *testing.T) {
	s, requested, release := stalledCloudService(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.startCloudSync(ctx)
	select {
	case <-requested:
	case <-time.After(5 * time.Second):
		t.Fatal("startup synchronization did not reach the test server")
	}
	saveProjectWhileCloudStalled(t, s, release)
	release()
}

func TestCloudSyncAndConfigurationWaitDoNotBlockProjectSave(t *testing.T) {
	for _, restore := range []bool{false, true} {
		name := "sync"
		if restore {
			name = "restore"
		}
		t.Run(name, func(t *testing.T) {
			s, requested, release := stalledCloudService(t)
			done := make(chan error, 1)
			go func() {
				var err error
				if restore {
					_, err = s.RestoreCloudBackup(t.Context(), strings.Repeat("a", 32))
				} else {
					_, err = s.SyncCloud(t.Context())
				}
				done <- err
			}()
			select {
			case <-requested:
			case <-time.After(5 * time.Second):
				t.Fatal("synchronization did not reach the test server")
			}
			config := s.GetCloudSyncStatus().Config
			config.Prefix = "replacement"
			configDone := make(chan error, 1)
			configFinished := make(chan struct{})
			go func() {
				defer close(configFinished)
				_, err := s.SaveCloudSyncConfig(config)
				configDone <- err
			}()
			t.Cleanup(func() {
				release()
				select {
				case <-configFinished:
				case <-time.After(5 * time.Second):
					t.Error("configuration save did not finish after cloud request was released")
				}
			})
			select {
			case <-configDone:
				t.Fatal("configuration changed while its cloud session was running")
			case <-time.After(50 * time.Millisecond):
			}
			saveProjectWhileCloudStalled(t, s, release)
			release()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("forbidden cloud response was accepted")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cloud request did not finish after release")
			}
			select {
			case err := <-configDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("configuration save did not finish after synchronization")
			}
			if s.GetCloudSyncStatus().Config.Prefix != config.Prefix {
				t.Fatal("configuration save was lost after synchronization")
			}
		})
	}
}
