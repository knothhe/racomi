package desktop

import (
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	skillstore "racomi/internal/skills"
)

// GlobalApplication is a known user skill directory, including shared paths
// supported by multiple agents. An empty slot has no persisted Target until the
// user saves a selection.
type GlobalApplication struct {
	Key         string             `json:"key"`
	AgentID     string             `json:"agentId"`
	AgentName   string             `json:"agentName"`
	Name        string             `json:"name"`
	Path        string             `json:"path"`
	Description string             `json:"description"`
	Target      *skillstore.Target `json:"target"`
}

func globalApplications(state skillstore.State) ([]GlobalApplication, error) {
	directories, err := codexDirectoryOptions()
	if err != nil {
		return nil, err
	}
	home := filepath.Dir(filepath.Dir(directories[0].Path))
	applications := []GlobalApplication{
		// Retain the historical key so saved defaults and deployment ownership
		// survive the correction from a Codex-only label to a shared directory.
		{Key: "codex-user", AgentID: "agents", AgentName: "Agent Skills", Name: "共享技能目录", Path: directories[0].Path, Description: directories[0].Description},
		{Key: "codex-legacy", AgentID: "codex", AgentName: "Codex", Name: "兼容技能目录", Path: directories[1].Path, Description: directories[1].Description},
		{Key: "claude-user", AgentID: "claude", AgentName: "Claude Code", Name: "用户技能目录", Path: filepath.Join(home, ".claude", "skills"), Description: "Claude Code 的本机用户技能目录，Cursor 也可兼容读取。"},
		{Key: "cursor-user", AgentID: "cursor", AgentName: "Cursor", Name: "用户技能目录", Path: filepath.Join(home, ".cursor", "skills"), Description: "Cursor 的本机用户技能目录，适用于所有项目。"},
	}
	matched := map[string]bool{}
	for i := range applications {
		applications[i].Path = applicationPath(applications[i].Path)
		for j := range state.Targets {
			target := &state.Targets[j]
			if target.Kind == "global" && sameApplicationPath(target.Path, applications[i].Path) {
				applications[i].Target = target
				matched[target.ID] = true
				break
			}
		}
	}
	// Keep historical custom directories addressable with their original IDs and
	// deployment ownership; fixed Agent slots never silently take them over.
	for i := range state.Targets {
		target := &state.Targets[i]
		if target.Kind == "global" && !matched[target.ID] {
			applications = append(applications, GlobalApplication{Key: target.ID, AgentID: "custom", AgentName: "自定义", Name: target.Name, Path: target.Path, Description: "已保存的自定义全局技能目录。", Target: target})
		}
	}
	return applications, nil
}

// Match the store's handling of macOS's system directory aliases, without
// resolving arbitrary user symlinks that the deployment checks must reject.
func applicationPath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "darwin" {
		for _, alias := range []string{"/tmp", "/var", "/etc"} {
			if path == alias || strings.HasPrefix(path, alias+"/") {
				resolved, err := filepath.EvalSymlinks(alias)
				if err == nil && resolved == "/private"+alias {
					return resolved + strings.TrimPrefix(path, alias)
				}
			}
		}
	}
	return path
}

func sameApplicationPath(a, b string) bool {
	a, b = applicationPath(a), applicationPath(b)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (s *Service) globalApplication(key string) (GlobalApplication, error) {
	state, err := s.store.State()
	if err != nil {
		return GlobalApplication{}, err
	}
	applications, err := globalApplications(state)
	if err != nil {
		return GlobalApplication{}, err
	}
	var found *GlobalApplication
	for i := range applications {
		if applications[i].Key == key {
			if found != nil {
				return GlobalApplication{}, errors.New("ambiguous global application key")
			}
			found = &applications[i]
		}
	}
	if found == nil {
		return GlobalApplication{}, errors.New("global application not found")
	}
	return *found, nil
}

func emptyApplicationPlan(key string) skillstore.ApplyPlan {
	return skillstore.ApplyPlan{TargetID: key, Add: []string{}, Update: []string{}, Remove: []string{}, Conflicts: []string{}, BlockingConflicts: []string{}, Overwrite: []string{}, Directories: []skillstore.DirectoryPlan{}}
}

func (s *Service) SetGlobalSelection(key string, skillIDs, presetIDs []string) (AppState, error) {
	return s.mutate(func() error {
		application, err := s.globalApplication(key)
		if err != nil {
			return err
		}
		if application.Target != nil {
			return s.store.SetTargetSelection(application.Target.ID, skillIDs, presetIDs)
		}
		_, err = s.store.SaveTarget(skillstore.Target{Name: application.AgentName + " · " + application.Name, Kind: "global", Path: application.Path, EnabledSkillIDs: skillIDs, EnabledPresetIDs: presetIDs})
		return err
	})
}

func (s *Service) PlanGlobal(key string) (skillstore.ApplyPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	application, err := s.globalApplication(key)
	if err != nil {
		return skillstore.ApplyPlan{}, err
	}
	if application.Target == nil {
		return emptyApplicationPlan(key), nil
	}
	return s.store.Plan(application.Target.ID)
}

func (s *Service) ApplyGlobal(key string) (AppState, error) {
	return s.mutate(func() error {
		application, err := s.globalApplication(key)
		if err != nil || application.Target == nil {
			return err
		}
		return s.store.Apply(application.Target.ID)
	})
}

func (s *Service) ForceApplyGlobal(key string, reviewed skillstore.ApplyPlan) (AppState, error) {
	return s.mutate(func() error {
		application, err := s.globalApplication(key)
		if err != nil || application.Target == nil {
			return err
		}
		return s.store.ForceApplyReviewed(application.Target.ID, reviewed)
	})
}

func (s *Service) project(id string) (skillstore.Target, error) {
	state, err := s.store.State()
	if err != nil {
		return skillstore.Target{}, err
	}
	for _, target := range state.Targets {
		if target.ID == id {
			if target.Kind != "project" {
				return skillstore.Target{}, errors.New("application is not a project")
			}
			return target, nil
		}
	}
	return skillstore.Target{}, errors.New("project not found")
}

func (s *Service) SaveProject(target skillstore.Target) (AppState, error) {
	return s.mutate(func() error {
		if strings.TrimSpace(target.Path) == "" {
			return errors.New("select a project root directory")
		}
		if target.ID != "" {
			if _, err := s.project(target.ID); err != nil {
				return err
			}
		}
		root := target.ProjectRoot
		if root == "" {
			root = target.Path
		}
		root, err := expandPath(root)
		if err != nil {
			return err
		}
		if err := skillstore.ValidateProjectRoot(root); err != nil {
			return err
		}
		target.Kind = "project"
		_, err = s.store.SaveTarget(target)
		return err
	})
}

func (s *Service) DeleteProject(id string) (AppState, error) {
	return s.mutate(func() error {
		if _, err := s.project(id); err != nil {
			return err
		}
		return s.store.DeleteTarget(id)
	})
}

func (s *Service) SetProjectSelection(id string, skillIDs, presetIDs []string) (AppState, error) {
	return s.mutate(func() error {
		if _, err := s.project(id); err != nil {
			return err
		}
		return s.store.SetTargetSelection(id, skillIDs, presetIDs)
	})
}

func (s *Service) PlanProject(id string) (skillstore.ApplyPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.project(id); err != nil {
		return skillstore.ApplyPlan{}, err
	}
	return s.store.Plan(id)
}

func (s *Service) ApplyProject(id string) (AppState, error) {
	return s.mutate(func() error {
		if _, err := s.project(id); err != nil {
			return err
		}
		return s.store.Apply(id)
	})
}

func (s *Service) ForceApplyProject(id string, reviewed skillstore.ApplyPlan) (AppState, error) {
	return s.mutate(func() error {
		if _, err := s.project(id); err != nil {
			return err
		}
		return s.store.ForceApplyReviewed(id, reviewed)
	})
}

// globalBatch validates every requested key before allowing any mutation.
func (s *Service) globalBatch(keys []string) ([]GlobalApplication, error) {
	state, err := s.store.State()
	if err != nil {
		return nil, err
	}
	applications, err := globalApplications(state)
	if err != nil {
		return nil, err
	}
	selected := []GlobalApplication{}
	for _, key := range keys {
		if slices.ContainsFunc(selected, func(application GlobalApplication) bool { return application.Key == key }) {
			continue
		}
		matches := []GlobalApplication{}
		for _, application := range applications {
			if application.Key == key {
				matches = append(matches, application)
			}
		}
		if len(matches) != 1 {
			return nil, errors.New("unknown or ambiguous global application key")
		}
		// CODEX_HOME may make two shortcuts point at the same user directory.
		if slices.ContainsFunc(selected, func(application GlobalApplication) bool {
			return sameApplicationPath(application.Path, matches[0].Path)
		}) {
			continue
		}
		selected = append(selected, matches[0])
	}
	return selected, nil
}

func (s *Service) SetGlobalSelections(keys, skillIDs, presetIDs []string) (AppState, error) {
	return s.mutate(func() error {
		applications, err := s.globalBatch(keys)
		if err != nil {
			return err
		}
		targets := []skillstore.Target{}
		for _, application := range applications {
			target := skillstore.Target{Name: application.AgentName + " · " + application.Name, Kind: "global", Path: application.Path}
			if application.Target != nil {
				target = *application.Target
			}
			target.EnabledSkillIDs, target.EnabledPresetIDs = append([]string{}, skillIDs...), append([]string{}, presetIDs...)
			targets = append(targets, target)
		}
		return s.store.SaveTargets(targets)
	})
}

func (s *Service) PlanGlobals(keys []string) (skillstore.ApplyPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	applications, err := s.globalBatch(keys)
	if err != nil {
		return skillstore.ApplyPlan{}, err
	}
	targetIDs := []string{}
	for _, application := range applications {
		if application.Target != nil {
			targetIDs = append(targetIDs, application.Target.ID)
		}
	}
	return s.store.PlanTargets(targetIDs)
}

func (s *Service) ApplyGlobals(keys []string) (AppState, error) {
	return s.mutate(func() error {
		applications, err := s.globalBatch(keys)
		if err != nil {
			return err
		}
		targetIDs := []string{}
		for _, application := range applications {
			if application.Target != nil {
				targetIDs = append(targetIDs, application.Target.ID)
			}
		}
		return s.store.ApplyTargets(targetIDs)
	})
}

func (s *Service) ForceApplyGlobals(keys []string, reviewed skillstore.ApplyPlan) (AppState, error) {
	return s.mutate(func() error {
		applications, err := s.globalBatch(keys)
		if err != nil {
			return err
		}
		targetIDs := []string{}
		for _, application := range applications {
			if application.Target != nil {
				targetIDs = append(targetIDs, application.Target.ID)
			}
		}
		return s.store.ForceApplyTargetsReviewed(targetIDs, reviewed)
	})
}
