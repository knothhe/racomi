package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleRejectsStalePagesAndCancelledSaves(t *testing.T) {
	s := &Service{}
	s.pageOpened()
	first := s.lifecycle.page
	s.pageReleased()
	s.pageOpened()
	r := &lifecycleRequest{id: 5, page: s.lifecycle.page, action: "quit", timer: time.NewTimer(time.Hour)}
	defer r.timer.Stop()
	s.lifecycle.pending = r
	if allowed, err := s.CompleteLifecycle(5, first, true); err != nil || allowed || s.lifecycle.pending != r {
		t.Fatal("old page authorized current request")
	}
	if allowed, _ := s.CompleteLifecycle(4, s.lifecycle.page, true); allowed || s.lifecycle.pending != r {
		t.Fatal("old acknowledgement authorized current request")
	}
	if allowed, _ := s.CompleteLifecycle(5, s.lifecycle.page, false); allowed || s.lifecycle.pending != nil || s.lifecycle.permit.Load() {
		t.Fatal("failed/cancelled save must retain application")
	}
	if allowed, _ := s.CompleteLifecycle(5, s.lifecycle.page, true); allowed {
		t.Fatal("cancelled acknowledgement was replayed")
	}
}

func TestAboutRemainsAvailableWhenLibraryCannotOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("invalid-json"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewApplicationService(root)
	if _, err := s.GetState(); err == nil {
		t.Fatal("corrupt library was treated as initialized")
	}
	if s.GetBuildInfo().Name == "" {
		t.Fatal("build info depends on business initialization")
	}
	after, _ := os.ReadFile(filepath.Join(root, "state.json"))
	if string(after) != "invalid-json" {
		t.Fatal("failed initialization overwrote original data")
	}
}

func TestReopenCancelsPendingHideWithoutCancellingQuit(t *testing.T) {
	s := &Service{}
	s.pageOpened()
	hide := &lifecycleRequest{id: 1, page: s.lifecycle.page, action: "hide", timer: time.NewTimer(time.Hour)}
	defer hide.timer.Stop()
	s.lifecycle.pending = hide
	s.cancelLifecycle("hide")
	if allowed, _ := s.CompleteLifecycle(1, s.lifecycle.page, true); allowed || s.lifecycle.running {
		t.Fatal("reopened page accepted the old hide acknowledgement")
	}
	quit := &lifecycleRequest{id: 2, page: s.lifecycle.page, action: "quit", timer: time.NewTimer(time.Hour)}
	defer quit.timer.Stop()
	s.lifecycle.pending = quit
	s.cancelLifecycle("hide")
	if s.lifecycle.pending != quit {
		t.Fatal("Dock reopen discarded an explicit quit request")
	}
}
