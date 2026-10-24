package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"racomi/internal/buildinfo"
	"runtime"
)

func (s *Service) configureMenu() {
	if s.app == nil || !s.lifecycle.started.Load() || runtime.GOOS != "darwin" {
		return
	}
	aboutLabel := s.nativeText("About racomi", "关于 racomi", "關於 racomi")
	updateLabel := s.nativeText("Check for Updates…", "检查应用更新…", "檢查應用程式更新…")
	editLabel := s.nativeText("Edit", "编辑", "編輯")
	application.InvokeAsync(func() {
		menu := application.NewMenu()
		appMenu := menu.AddSubmenu(buildinfo.Current().Name)
		appMenu.Add(aboutLabel).OnClick(func(*application.Context) { s.showAbout() })
		appMenu.Add(updateLabel).OnClick(func(*application.Context) {
			if err := s.CheckApplicationUpdates(); err != nil {
				s.showAbout()
			}
		})
		appMenu.AddSeparator()
		appMenu.AddRole(application.ServicesMenu)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Hide)
		appMenu.AddRole(application.HideOthers)
		appMenu.AddRole(application.UnHide)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Quit)
		edit := menu.AddSubmenu(editLabel)
		for _, role := range []application.Role{application.Undo, application.Redo, application.Cut, application.Copy, application.Paste, application.SelectAll} {
			edit.AddRole(role)
		}
		s.app.Menu.Set(menu)
	})
}

func (s *Service) showAbout() {
	s.lifecycle.mu.Lock()
	ready := s.lifecycle.ready
	s.lifecycle.about = !ready
	s.lifecycle.mu.Unlock()
	s.OpenWindow()
	if ready {
		s.app.Event.Emit("racomi:show-about")
	}
}
