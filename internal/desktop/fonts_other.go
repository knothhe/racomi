//go:build !darwin

package desktop

func listInterfaceFonts() ([]InterfaceFontFamily, error) { return []InterfaceFontFamily{}, nil }
