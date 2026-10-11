package skills

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func expandedTargetPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("target directory cannot be empty")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	path, err := filepath.Abs(path)
	return canonicalSystemPath(path), err
}

// Relative custom paths follow the project root. Absolute and home-relative
// inputs are resolved once when saved, just like other machine-local paths.
func projectCustomPath(root, input string) (stored, resolved string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errors.New("custom project directory cannot be empty")
	}
	if filepath.IsAbs(input) || input == "~" || strings.HasPrefix(input, "~/") {
		resolved, err = expandedTargetPath(input)
		return resolved, resolved, err
	}
	stored = filepath.Clean(input)
	return stored, canonicalSystemPath(filepath.Join(root, stored)), nil
}

func (l *Store) prepareTarget(state State, target Target) (Target, error) {
	target.Name = strings.TrimSpace(target.Name)
	if target.Kind == "" {
		target.Kind = "global"
	}
	index := slices.IndexFunc(state.Targets, func(t Target) bool { return t.ID == target.ID })
	var previous *Target
	if index >= 0 {
		previous = &state.Targets[index]
	}
	if target.ID == "" {
		target.ID = id("target")
	}
	target.Path = strings.TrimSpace(target.Path)
	if target.Kind == "project" {
		root := target.ProjectRoot
		if root == "" {
			root = target.Path
			if previous != nil && previous.Kind == "project" && target.Path == previous.Path {
				root = previous.ProjectRoot
			}
		}
		if root == "" {
			return Target{}, errors.New("select a repository directory for project targets")
		}
		var err error
		root, err = expandedTargetPath(root)
		if err != nil {
			return Target{}, err
		}
		legacyRoot, legacyID, legacy := projectDirectory(root)
		if legacy {
			root = legacyRoot
		}
		if previous != nil && previous.Kind == "project" && len(previous.Deployments) > 0 && root != previous.ProjectRoot {
			return Target{}, errors.New("clear and apply the target before changing its directory")
		}
		if err = noSymlinks(root); err != nil {
			return Target{}, err
		}
		// Existing legacy skills directories could be saved before the repository
		// exists. New root selection still requires a real repository directory.
		if !legacy || previous != nil {
			info, statErr := os.Stat(root)
			if statErr != nil {
				return Target{}, statErr
			}
			if !info.IsDir() {
				return Target{}, errors.New("repository must be a directory")
			}
		}
		customPaths := target.CustomPaths
		if customPaths == nil && previous != nil {
			customPaths = previous.CustomPaths
		}
		directories := target.DirectoryIDs
		if directories == nil {
			if previous != nil {
				directories = previous.DirectoryIDs
			} else if legacy {
				directories = []string{legacyID}
			} else {
				directories = []string{}
				customPaths = append([]string{}, customPaths...)
				for _, directoryID := range state.Settings.DefaultProjectDirectories {
					if _, builtin := projectDirectoryNames[directoryID]; builtin {
						directories = append(directories, directoryID)
						continue
					}
					for _, directory := range state.Settings.CustomProjectDirectories {
						if directory.ID == directoryID {
							customPaths = append(customPaths, directory.Path)
							break
						}
					}
				}
			}
		}
		directories = unique(directories)
		target.Paths = []string{}
		for _, directoryID := range directories {
			directory, ok := projectDirectoryNames[directoryID]
			if !ok {
				return Target{}, errors.New("unknown project directory")
			}
			target.Paths = append(target.Paths, filepath.Join(root, directory, "skills"))
		}
		target.CustomPaths = []string{}
		for _, input := range customPaths {
			stored, resolved, pathErr := projectCustomPath(root, input)
			if pathErr != nil {
				return Target{}, pathErr
			}
			target.CustomPaths = append(target.CustomPaths, stored)
			target.Paths = append(target.Paths, resolved)
		}
		if len(target.Paths) == 0 {
			return Target{}, errors.New("select at least one project directory")
		}
		target.ProjectRoot, target.DirectoryIDs = root, append([]string{}, directories...)
		target.Path = target.Paths[0]
	} else {
		if len(target.Paths) == 0 {
			if target.Path == "" {
				target.Path = "~/.agents/skills"
			}
			target.Paths = []string{target.Path}
		}
		for i := range target.Paths {
			path, err := expandedTargetPath(target.Paths[i])
			if err != nil {
				return Target{}, err
			}
			target.Paths[i] = path
		}
		target.Paths = unique(target.Paths)
		target.Path = target.Paths[0]
		if previous != nil && len(previous.Deployments) > 0 && !slices.Equal(previous.Paths, target.Paths) {
			return Target{}, errors.New("clear and apply the target before changing its directory")
		}
		target.ProjectRoot, target.DirectoryIDs, target.CustomPaths = "", []string{}, []string{}
	}
	for _, path := range target.Paths {
		if err := l.checkTargetPath(path, true); err != nil {
			return Target{}, err
		}
	}
	if previous != nil {
		target.Deployments = previous.Deployments
		if target.EnabledSkillIDs == nil {
			target.EnabledSkillIDs = previous.EnabledSkillIDs
		}
	} else {
		target.Deployments = []Deployment{}
	}
	target.EnabledSkillIDs, target.EnabledPresetIDs = unique(target.EnabledSkillIDs), unique(target.EnabledPresetIDs)
	return normalizeTarget(target), nil
}

// SaveTargets commits a batch of selections together, without deploying files.
func (l *Store) SaveTargets(targets []Target) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, err := l.readState()
	if err != nil {
		return err
	}
	before := cloneState(state)
	seen := map[string]bool{}
	for _, target := range targets {
		target, err = l.prepareTarget(state, target)
		if err != nil {
			return err
		}
		if seen[target.ID] {
			return errors.New("duplicate target")
		}
		seen[target.ID] = true
		index := slices.IndexFunc(state.Targets, func(t Target) bool { return t.ID == target.ID })
		if index >= 0 {
			state.Targets[index] = target
		} else {
			state.Targets = append(state.Targets, target)
		}
	}
	if err = l.validate(state); err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	if _, err = l.snapshot(before, "before changing target selections"); err != nil {
		return err
	}
	return l.writeState(state)
}
