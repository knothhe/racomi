package desktop

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// OmarchyTheme contains only palette data. Never execute theme configuration or
// install hooks into the user's desktop. The frontend derives its semantic roles.
type OmarchyTheme struct {
	Colors map[string]string `json:"colors"`
	Dark   bool              `json:"dark"`
}

var omarchyColorLine = regexp.MustCompile(`^([a-z_]+)\s*=\s*(?:"(#[0-9a-fA-F]{6})"|'(#[0-9a-fA-F]{6})')\s*(?:#.*)?$`)
var omarchyModeLine = regexp.MustCompile(`^(mode|theme_type)\s*=\s*(?:"(light|dark)"|'(light|dark)')\s*(?:#.*)?$`)

func parseOmarchyTheme(raw string) *OmarchyTheme {
	colors := make(map[string]string)
	mode, legacy := "", ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		// Only top-level palette keys belong to Omarchy's colors.toml format.
		if strings.HasPrefix(line, "[") {
			break
		}
		if match := omarchyColorLine.FindStringSubmatch(line); match != nil {
			colors[match[1]] = strings.ToLower(match[2] + match[3])
		}
		if match := omarchyModeLine.FindStringSubmatch(line); match != nil {
			if match[1] == "mode" {
				mode = match[2] + match[3]
			} else {
				legacy = match[2] + match[3]
			}
		}
	}
	for _, key := range []string{"background", "foreground", "accent"} {
		if colors[key] == "" {
			return nil
		}
	}
	if mode == "" {
		mode = legacy
	}
	r, g, b := hexRGB(colors["background"])
	dark := int(r)*299+int(g)*587+int(b)*114 < 128000
	if mode != "" {
		dark = mode == "dark"
	}
	return &OmarchyTheme{Colors: colors, Dark: dark}
}

func hexRGB(color string) (uint8, uint8, uint8) {
	n, _ := strconv.ParseUint(strings.TrimPrefix(color, "#"), 16, 24)
	return uint8(n >> 16), uint8(n >> 8), uint8(n)
}

func readOmarchyTheme() *OmarchyTheme {
	if runtime.GOOS != "linux" {
		return nil
	}
	state := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		state = filepath.Join(home, ".local", "state")
	}
	config, _ := os.UserConfigDir()
	for _, dir := range []string{state, config} {
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, "omarchy", "current", "theme", "colors.toml")
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		const maxPaletteSize = 64 << 10
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxPaletteSize {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(file, maxPaletteSize+1))
		file.Close()
		if err != nil || len(data) > maxPaletteSize {
			return nil
		}
		return parseOmarchyTheme(string(data))
	}
	return nil
}

// GetOmarchyTheme reopens the active path so both edits and symlink switches are
// detected. Missing or invalid palettes restore Racomi's normal appearance.
func (s *Service) GetOmarchyTheme() *OmarchyTheme { return readOmarchyTheme() }
