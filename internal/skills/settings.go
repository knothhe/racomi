package skills

import (
	"errors"
	"fmt"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const DeploymentCopy = "copy"
const DeploymentSymlink = "symlink"

var projectDirectoryNames = map[string]string{"agents": ".agents", "claude": ".claude", "codex": ".codex", "cursor": ".cursor"}

func defaultSettings() Settings {
	return Settings{DeploymentMode: DeploymentCopy, DefaultProjectDirectories: []string{"agents"}, DefaultGlobalDirectories: []string{"codex-user"}, CustomProjectDirectories: []ProjectDirectory{}, SnapshotRetentionDays: 7, SnapshotMaxCount: 100}
}

func normalizeSettings(settings Settings) Settings {
	defaults := defaultSettings()
	if settings.SnapshotRetentionDays == 0 {
		settings.SnapshotRetentionDays = defaults.SnapshotRetentionDays
	}
	if settings.SnapshotMaxCount == 0 {
		settings.SnapshotMaxCount = defaults.SnapshotMaxCount
	}
	if settings.DeploymentMode == "" {
		settings.DeploymentMode = defaults.DeploymentMode
	}
	if settings.DefaultProjectDirectories == nil {
		settings.DefaultProjectDirectories = defaults.DefaultProjectDirectories
	}
	if settings.DefaultGlobalDirectories == nil {
		settings.DefaultGlobalDirectories = defaults.DefaultGlobalDirectories
	}
	settings.DefaultProjectDirectories = append([]string{}, settings.DefaultProjectDirectories...)
	settings.DefaultGlobalDirectories = append([]string{}, settings.DefaultGlobalDirectories...)
	settings.CustomProjectDirectories = append([]ProjectDirectory{}, settings.CustomProjectDirectories...)
	return settings
}

func normalizeProjectDirectories(directories []ProjectDirectory) ([]ProjectDirectory, error) {
	result := make([]ProjectDirectory, 0, len(directories))
	seen := map[string]bool{}
	for _, directory := range directories {
		_, builtin := projectDirectoryNames[directory.ID]
		if !safeName.MatchString(directory.ID) || builtin || seen[directory.ID] {
			return nil, fmt.Errorf("invalid or duplicate project directory template ID %q", directory.ID)
		}
		seen[directory.ID] = true
		directory.Name = strings.TrimSpace(directory.Name)
		if length := utf8.RuneCountInString(directory.Name); length == 0 || length > 128 {
			return nil, errors.New("project directory template name must contain 1 to 128 characters")
		}
		directory.Path = strings.TrimSpace(directory.Path)
		if len(directory.Path) == 0 || len(directory.Path) > 4096 {
			return nil, errors.New("project directory template path must contain 1 to 4096 bytes")
		}
		if strings.ContainsRune(directory.Path, '\x00') {
			return nil, errors.New("project directory template path cannot contain a null byte")
		}
		var err error
		directory.Path, _, err = projectCustomPath("", directory.Path)
		if err != nil {
			return nil, err
		}
		for _, existing := range result {
			if within(existing.Path, directory.Path) && within(directory.Path, existing.Path) {
				return nil, fmt.Errorf("duplicate project directory template path %q", directory.Path)
			}
		}
		result = append(result, directory)
	}
	return result, nil
}

func relativeProjectTemplatePath(path string) bool {
	// Use slash semantics even on Windows to expose disguised drive prefixes.
	path = pathpkg.Clean(strings.ReplaceAll(strings.TrimSpace(path), `\`, "/"))
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return false
	}
	// Recognize Windows drive paths on every host, including drive-relative C:foo.
	return !(len(path) >= 2 && path[1] == ':' && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')))
}

func validateSettings(settings Settings, targets []Target) error {
	if settings.TextSize != 0 && !slices.Contains([]int{100, 110, 125, 150}, settings.TextSize) {
		return errors.New("text size must be 100, 110, 125, or 150")
	}
	if !validFontPreference(settings.InterfaceFont) || !validFontPreference(settings.InterfaceFontStyle) {
		return errors.New("invalid interface font")
	}
	if settings.SnapshotRetentionDays < 1 || settings.SnapshotRetentionDays > 3650 || settings.SnapshotMaxCount < 1 || settings.SnapshotMaxCount > 10000 {
		return errors.New("snapshot retention must be 1 to 3650 days and 1 to 10000 snapshots")
	}
	if settings.Theme != "" && !slices.Contains([]string{"system", "light", "dark"}, settings.Theme) {
		return errors.New("theme must be system, light, or dark")
	}
	if settings.Locale != "" && !slices.Contains([]string{"system", "zh-CN", "zh-TW", "en"}, settings.Locale) {
		return errors.New("locale must be system, zh-CN, zh-TW, or en")
	}
	if settings.NavLayout != "" && !slices.Contains([]string{"auto", "top", "side"}, settings.NavLayout) {
		return errors.New("navigation layout must be auto, top, or side")
	}
	if settings.DeploymentMode != DeploymentCopy && settings.DeploymentMode != DeploymentSymlink {
		return errors.New("deployment mode must be copy or symlink")
	}
	if len(settings.DefaultProjectDirectories) == 0 || len(settings.DefaultGlobalDirectories) == 0 {
		return errors.New("select at least one default project and global directory")
	}
	if _, err := normalizeProjectDirectories(settings.CustomProjectDirectories); err != nil {
		return err
	}
	for _, value := range settings.DefaultProjectDirectories {
		if _, ok := projectDirectoryNames[value]; !ok && !slices.ContainsFunc(settings.CustomProjectDirectories, func(directory ProjectDirectory) bool { return directory.ID == value }) {
			return fmt.Errorf("unknown project directory %q", value)
		}
	}
	for _, value := range settings.DefaultGlobalDirectories {
		if !slices.Contains([]string{"codex-user", "codex-legacy", "claude-user", "cursor-user"}, value) && !slices.ContainsFunc(targets, func(target Target) bool { return target.Kind == "global" && target.ID == value }) {
			return fmt.Errorf("unknown global directory %q", value)
		}
	}
	if len(unique(settings.DefaultProjectDirectories)) != len(settings.DefaultProjectDirectories) || len(unique(settings.DefaultGlobalDirectories)) != len(settings.DefaultGlobalDirectories) {
		return errors.New("duplicate default directories")
	}
	return nil
}

// SaveSettings changes future plans and defaults, and never writes deployments.
func (l *Store) SaveSettings(settings Settings) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, err := l.readState()
	if err != nil {
		return err
	}
	// Omitted templates preserve preferences for clients predating this field.
	// An explicit empty array removes them without changing any project paths.
	if settings.CustomProjectDirectories == nil {
		settings.CustomProjectDirectories = state.Settings.CustomProjectDirectories
	}
	// Older clients omit these WebView preferences. Preserve them until a
	// client explicitly chooses a supported value, including system/auto.
	if settings.TextSize == 0 {
		settings.TextSize = state.Settings.TextSize
	}
	if settings.InterfaceFont == "" {
		settings.InterfaceFont = state.Settings.InterfaceFont
	}
	if settings.InterfaceFontStyle == "" {
		if settings.InterfaceFont != state.Settings.InterfaceFont {
			settings.InterfaceFontStyle = "system"
		} else {
			settings.InterfaceFontStyle = state.Settings.InterfaceFontStyle
		}
	}
	if settings.SnapshotRetentionDays == 0 {
		settings.SnapshotRetentionDays = state.Settings.SnapshotRetentionDays
	}
	if settings.SnapshotMaxCount == 0 {
		settings.SnapshotMaxCount = state.Settings.SnapshotMaxCount
	}
	if settings.Theme == "" {
		settings.Theme = state.Settings.Theme
	}
	if settings.Locale == "" {
		settings.Locale = state.Settings.Locale
	}
	if settings.NavLayout == "" {
		settings.NavLayout = state.Settings.NavLayout
	}
	directories, err := normalizeProjectDirectories(settings.CustomProjectDirectories)
	if err != nil {
		return err
	}
	for i, directory := range settings.CustomProjectDirectories {
		// Legacy absolute templates remain editable as long as their saved path
		// is unchanged. Keep this write constraint out of state validation so
		// reading older settings and project path snapshots stays compatible.
		unchanged := slices.ContainsFunc(state.Settings.CustomProjectDirectories, func(previous ProjectDirectory) bool {
			return previous.ID == directory.ID && strings.TrimSpace(previous.Path) == strings.TrimSpace(directory.Path)
		})
		if !unchanged && (!relativeProjectTemplatePath(directory.Path) || !relativeProjectTemplatePath(directories[i].Path)) {
			return errors.New("项目目录模板必须使用相对项目根目录的路径")
		}
	}
	settings.CustomProjectDirectories = directories
	deleted := map[string]bool{}
	for _, old := range state.Settings.CustomProjectDirectories {
		if !slices.ContainsFunc(settings.CustomProjectDirectories, func(directory ProjectDirectory) bool { return directory.ID == old.ID }) {
			deleted[old.ID] = true
		}
	}
	settings.DefaultProjectDirectories = slices.DeleteFunc(append([]string{}, settings.DefaultProjectDirectories...), func(value string) bool { return deleted[value] })
	if len(settings.DefaultProjectDirectories) == 0 && len(deleted) > 0 {
		settings.DefaultProjectDirectories = []string{"agents"}
	}
	if err := validateSettings(settings, state.Targets); err != nil {
		return err
	}
	state.Settings = normalizeSettings(settings)
	if err = l.writeState(state); err != nil {
		return err
	}
	_, err = l.pruneSnapshots(state, time.Now(), true)
	return err
}

func validFontPreference(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 128 && !strings.ContainsAny(value, "\x00\r\n")
}

func deploymentMode(deployment Deployment) string {
	if deployment.Mode == "" {
		return DeploymentCopy
	}
	return deployment.Mode
}

func targetPaths(target Target) []string {
	if len(target.Paths) == 0 {
		return []string{target.Path}
	}
	return target.Paths
}

func deploymentPath(target Target, deployment Deployment) string {
	if deployment.Path == "" {
		return target.Path
	}
	return deployment.Path
}

func allTargetPaths(target Target) []string {
	paths := append([]string{}, targetPaths(target)...)
	for _, deployment := range target.Deployments {
		paths = append(paths, deploymentPath(target, deployment))
	}
	return unique(paths)
}

func projectDirectory(path string) (string, string, bool) {
	if filepath.Base(path) != "skills" {
		return "", "", false
	}
	for id, directory := range projectDirectoryNames {
		if filepath.Base(filepath.Dir(path)) == directory {
			return filepath.Dir(filepath.Dir(path)), id, true
		}
	}
	return "", "", false
}

func normalizeTarget(target Target) Target {
	target.Paths = append([]string{}, targetPaths(target)...)
	legacyDirectories := target.DirectoryIDs == nil && len(target.CustomPaths) == 0
	target.DirectoryIDs = append([]string{}, target.DirectoryIDs...)
	target.CustomPaths = append([]string{}, target.CustomPaths...)
	if target.Kind == "project" && target.ProjectRoot == "" {
		if root, _, ok := projectDirectory(target.Path); ok {
			target.ProjectRoot = root
		}
	}
	if target.Kind == "project" && legacyDirectories {
		for _, path := range target.Paths {
			if root, id, ok := projectDirectory(path); ok && root == target.ProjectRoot {
				target.DirectoryIDs = append(target.DirectoryIDs, id)
			}
		}
	}
	for i := range target.Deployments {
		target.Deployments[i].Path = deploymentPath(target, target.Deployments[i])
		target.Deployments[i].Mode = deploymentMode(target.Deployments[i])
	}
	return target
}
