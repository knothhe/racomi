package desktop

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	skillstore "racomi/internal/skills"
)

func applicationPathsOverlap(a, b string) bool {
	a, b = applicationPath(a), applicationPath(b)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	contains := func(parent, path string) bool {
		relative, err := filepath.Rel(parent, path)
		return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
	}
	return contains(a, b) || contains(b, a)
}

func builtInGlobalPaths() ([]string, error) {
	applications, err := globalApplications(skillstore.State{})
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, application := range applications {
		paths = append(paths, application.Path)
	}
	return paths, nil
}

func (s *Service) customGlobalDirectory(id string) (skillstore.Target, error) {
	state, err := s.store.State()
	if err != nil {
		return skillstore.Target{}, err
	}
	for _, target := range state.Targets {
		if target.ID != id {
			continue
		}
		if target.Kind != "global" {
			return skillstore.Target{}, errors.New("application is not a custom global directory")
		}
		builtIns, err := builtInGlobalPaths()
		if err != nil {
			return skillstore.Target{}, err
		}
		paths := target.Paths
		if len(paths) == 0 {
			paths = []string{target.Path}
		}
		for _, path := range paths {
			for _, builtIn := range builtIns {
				if applicationPathsOverlap(path, builtIn) {
					return skillstore.Target{}, errors.New("built-in global directories cannot be edited or deleted")
				}
			}
		}
		return target, nil
	}
	return skillstore.Target{}, errors.New("custom global directory not found")
}

// SaveGlobalDirectory registers one named custom skill directory. Selections
// and deployment ownership survive metadata edits, and saving never deploys.
func (s *Service) SaveGlobalDirectory(target skillstore.Target) (AppState, error) {
	return s.mutate(func() error {
		if target.Kind != "" && target.Kind != "global" {
			return errors.New("application is not a custom global directory")
		}
		if strings.TrimSpace(target.Path) == "" {
			return errors.New("select a custom global directory")
		}
		if target.ID != "" {
			previous, err := s.customGlobalDirectory(target.ID)
			if err != nil {
				return err
			}
			if target.EnabledSkillIDs == nil {
				target.EnabledSkillIDs = previous.EnabledSkillIDs
			}
			if target.EnabledPresetIDs == nil {
				target.EnabledPresetIDs = previous.EnabledPresetIDs
			}
		}
		path, err := expandPath(target.Path)
		if err != nil {
			return err
		}
		path = applicationPath(path)
		builtIns, err := builtInGlobalPaths()
		if err != nil {
			return err
		}
		for _, builtIn := range builtIns {
			if applicationPathsOverlap(path, builtIn) {
				return errors.New("custom global directory cannot overlap a built-in directory")
			}
		}
		target.Kind, target.Path, target.Paths = "global", path, []string{path}
		_, err = s.store.SaveTarget(target)
		return err
	})
}

func (s *Service) DeleteGlobalDirectory(id string) (AppState, error) {
	return s.mutate(func() error {
		if _, err := s.customGlobalDirectory(id); err != nil {
			return err
		}
		return s.store.DeleteTarget(id)
	})
}
