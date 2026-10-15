package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// Resolve the owner for every invocation: lightweight mode can destroy and
// recreate the main window. A native sheet then stays with the visible window
// instead of opening as an independent panel behind another application.
func (s *Service) dialogWindow() application.Window {
	return application.InvokeSyncWithResult(func() application.Window {
		var window application.Window
		for _, candidate := range s.app.Window.GetAll() {
			// Close finishes asynchronously in Wails, so the retiring and
			// recreated windows can briefly share the same name.
			if candidate.Name() == "main" && candidate.NativeWindow() != nil && (window == nil || candidate.ID() > window.ID()) {
				window = candidate
			}
		}
		if window == nil {
			return nil
		}
		window.Show()
		window.Focus()
		return window
	})
}

func (s *Service) openFileDialog() *application.OpenFileDialogStruct {
	return s.app.Dialog.OpenFile().AttachToWindow(s.dialogWindow())
}

func (s *Service) saveFileDialog() *application.SaveFileDialogStruct {
	return s.app.Dialog.SaveFile().AttachToWindow(s.dialogWindow())
}
