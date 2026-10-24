package desktop

import "errors"

// SetLocale syncs native dialogs with the frontend's persisted language choice.
func (s *Service) SetLocale(locale string) error {
	if locale != "zh-CN" && locale != "zh-TW" && locale != "en" {
		return errors.New("locale must be zh-CN, zh-TW, or en")
	}
	s.themeMu.Lock()
	defer s.themeMu.Unlock()
	s.locale = locale
	go s.configureMenu()
	return nil
}

func (s *Service) nativeText(english, simplified, traditional string) string {
	s.themeMu.Lock()
	defer s.themeMu.Unlock()
	switch s.locale {
	case "en":
		return english
	case "zh-TW":
		return traditional
	default:
		return simplified
	}
}
