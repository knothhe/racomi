package desktop

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GDI full face names are valid CSS local() sources for the font/style picker.
type windowsLogFont struct {
	Height, Width, Escapement, Orientation, Weight       int32
	Italic, Underline, StrikeOut, CharSet                byte
	OutPrecision, ClipPrecision, Quality, PitchAndFamily byte
	FaceName                                             [32]uint16
}
type windowsEnumFont struct {
	LogFont  windowsLogFont
	FullName [64]uint16
	Style    [32]uint16
	Script   [32]uint16
}

var (
	fontEnumMu   sync.Mutex
	fontFamilies map[string]map[string]string
	fontCallback = windows.NewCallback(func(face *windowsEnumFont, _, _, _ uintptr) uintptr {
		family := windows.UTF16ToString(face.LogFont.FaceName[:])
		full := windows.UTF16ToString(face.FullName[:])
		style := windows.UTF16ToString(face.Style[:])
		if family == "" || full == "" || strings.HasPrefix(family, "@") {
			return 1
		}
		if style == "" {
			style = "Regular"
		}
		if fontFamilies[family] == nil {
			fontFamilies[family] = map[string]string{}
		}
		fontFamilies[family][full] = style
		return 1
	})
)

func listInterfaceFonts() ([]InterfaceFontFamily, error) {
	fontEnumMu.Lock()
	defer fontEnumMu.Unlock()
	fontFamilies = map[string]map[string]string{}
	user := windows.NewLazySystemDLL("user32.dll")
	dc, _, _ := user.NewProc("GetDC").Call(0)
	if dc == 0 {
		return nil, errors.New("could not read installed fonts")
	}
	defer user.NewProc("ReleaseDC").Call(0, dc)
	font := windowsLogFont{CharSet: 1}
	windows.NewLazySystemDLL("gdi32.dll").NewProc("EnumFontFamiliesExW").Call(dc, uintptr(unsafe.Pointer(&font)), fontCallback, 0, 0)
	families := make([]string, 0, len(fontFamilies))
	for family := range fontFamilies {
		families = append(families, family)
	}
	for _, family := range families {
		font = windowsLogFont{CharSet: 1}
		copy(font.FaceName[:], windows.StringToUTF16(family))
		windows.NewLazySystemDLL("gdi32.dll").NewProc("EnumFontFamiliesExW").Call(dc, uintptr(unsafe.Pointer(&font)), fontCallback, 0, 0)
	}
	result := []InterfaceFontFamily{}
	for family, faces := range fontFamilies {
		styles := []InterfaceFontStyle{}
		for full, style := range faces {
			styles = append(styles, InterfaceFontStyle{Name: style, PostscriptName: full})
		}
		sort.Slice(styles, func(i, j int) bool {
			return styles[i].Name+styles[i].PostscriptName < styles[j].Name+styles[j].PostscriptName
		})
		result = append(result, InterfaceFontFamily{Family: family, Styles: styles})
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Family) < strings.ToLower(result[j].Family) })
	return result, nil
}
