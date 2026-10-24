//go:build !darwin && !windows

package desktop

func listInterfaceFonts() ([]InterfaceFontFamily, error) { return []InterfaceFontFamily{}, nil }
