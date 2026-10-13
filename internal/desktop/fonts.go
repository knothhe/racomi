package desktop

type InterfaceFontStyle struct {
	Name           string `json:"name"`
	PostscriptName string `json:"postscriptName"`
}

type InterfaceFontFamily struct {
	Family string               `json:"family"`
	Styles []InterfaceFontStyle `json:"styles"`
}

func (s *Service) ListInterfaceFonts() ([]InterfaceFontFamily, error) {
	return listInterfaceFonts()
}
