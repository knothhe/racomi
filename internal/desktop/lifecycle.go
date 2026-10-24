package desktop

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type lifecycleState struct {
	running  bool
	started  atomic.Bool
	about    bool
	mu       sync.Mutex
	page     uint64
	sequence uint64
	ready    bool
	present  bool
	pending  *lifecycleRequest
	permit   atomic.Bool
	stopping atomic.Bool
}

type lifecycleRequest struct {
	id           uint64
	page         uint64
	action       string
	continuation func()
	timer        *time.Timer
}

type LifecycleEvent struct {
	ID     uint64 `json:"id"`
	Page   uint64 `json:"page"`
	Action string `json:"action"`
}

func (s *Service) pageOpened() {
	l := &s.lifecycle
	l.mu.Lock()
	defer l.mu.Unlock()
	l.page++
	l.present = true
	l.ready = false
}
func (s *Service) pageReleased() {
	l := &s.lifecycle
	l.mu.Lock()
	defer l.mu.Unlock()
	l.present = false
	l.ready = false
}

// FrontendReady is called only after listeners have been attached, including on
// an initialization error. No window-dependent action runs before this handshake.
func (s *Service) FrontendReady() uint64 {
	l := &s.lifecycle
	l.mu.Lock()
	l.ready = true
	page, pending, about := l.page, l.pending, l.about
	l.about = false
	l.mu.Unlock()
	if pending != nil {
		s.emitLifecycle(pending)
	}
	if about && s.app != nil {
		s.app.Event.Emit("racomi:show-about")
	}
	return page
}

func (s *Service) emitLifecycle(r *lifecycleRequest) {
	if s.app != nil {
		s.app.Event.Emit("racomi:lifecycle", LifecycleEvent{r.id, r.page, r.action})
	}
}

func (s *Service) requestLifecycle(action string, continuation func()) {
	l := &s.lifecycle
	l.mu.Lock()
	if l.pending != nil || l.running {
		l.mu.Unlock()
		return
	}
	if !l.present {
		l.running = true
		l.mu.Unlock()
		s.finishLifecycle(action, continuation)
		return
	}
	l.sequence++
	r := &lifecycleRequest{id: l.sequence, page: l.page, action: action, continuation: continuation}
	l.pending = r
	r.timer = time.AfterFunc(45*time.Second, func() {
		l.mu.Lock()
		if l.pending != r {
			l.mu.Unlock()
			return
		}
		l.pending = nil
		l.mu.Unlock()
		if s.app != nil {
			s.app.Event.Emit("racomi:lifecycle-cancelled", r.id)
		}
	})
	ready := l.ready
	l.mu.Unlock()
	if ready {
		s.emitLifecycle(r)
	}
}

// CompleteLifecycle rejects stale pages/responses. A failed save never authorizes
// destruction of a page, even if another close request arrives while it is saving.
func (s *Service) CompleteLifecycle(id, page uint64, allow bool) (bool, error) {
	l := &s.lifecycle
	l.mu.Lock()
	r := l.pending
	if r == nil || r.id != id || r.page != page || l.page != page {
		l.mu.Unlock()
		return false, nil
	}
	l.pending = nil
	l.running = allow
	r.timer.Stop()
	l.mu.Unlock()
	if allow {
		go s.finishLifecycle(r.action, r.continuation)
	}
	return allow, nil
}

func (s *Service) finishLifecycle(action string, continuation func()) {
	if action == "quit" || action == "update" {
		s.lifecycle.stopping.Store(true)
		// Auto sync observes stopping before starting work. Existing mutations retain
		// their normal transaction/cancellation semantics and must finish first.
		done := make(chan struct{})
		go func() {
			if s.cloud != nil {
				s.cloud.runMu.Lock()
				s.cloud.runMu.Unlock()
			}
			s.mu.Lock()
			s.mu.Unlock()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			s.lifecycle.stopping.Store(false)
			s.lifecycle.mu.Lock()
			s.lifecycle.running = false
			s.lifecycle.mu.Unlock()
			if s.app != nil {
				s.app.Event.Emit("racomi:lifecycle-error", "A background operation is still running. Try again when it finishes.")
			}
			return
		}
		s.lifecycle.permit.Store(true)
	}
	application.InvokeAsync(func() {
		continuation()
		if action == "hide" && s.app != nil {
			s.lifecycle.mu.Lock()
			s.lifecycle.running = false
			s.lifecycle.mu.Unlock()
			s.app.Event.Emit("racomi:lifecycle-resumed")
		}
	})
}

// ShouldQuit gates Cmd+Q, Dock quit, system termination, application.Quit, and
// Sparkle's termination. It must return immediately on the native UI thread.
func (s *Service) ShouldQuit() bool {
	if s.lifecycle.permit.Load() {
		return true
	}
	if s.app == nil {
		return false
	}
	go s.requestLifecycle("quit", s.app.Quit)
	return false
}

func (s *Service) RequestQuit() error {
	if s.app == nil {
		return errors.New("desktop application is unavailable")
	}
	s.app.Quit()
	return nil
}

// Reopening cancels an unacknowledged hide; an already approved hide also checks
// its window generation on the UI thread before it may act.
func (s *Service) cancelLifecycle(action string) {
	l := &s.lifecycle
	l.mu.Lock()
	r := l.pending
	if r == nil || r.action != action {
		l.mu.Unlock()
		return
	}
	l.pending = nil
	r.timer.Stop()
	l.mu.Unlock()
	if s.app != nil {
		s.app.Event.Emit("racomi:lifecycle-cancelled", r.id)
	}
}
