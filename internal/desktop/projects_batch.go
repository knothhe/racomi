package desktop

import (
	"errors"
	"slices"

	skillstore "racomi/internal/skills"
)

// projectBatch checks the whole selection before previewing or deploying any
// destination, so a stale or global ID cannot partially apply a project batch.
func (s *Service) projectBatch(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, errors.New("select at least one project")
	}
	state, err := s.store.State()
	if err != nil {
		return nil, err
	}
	selected := []string{}
	for _, id := range ids {
		if !slices.ContainsFunc(state.Targets, func(target skillstore.Target) bool { return target.ID == id && target.Kind == "project" }) {
			return nil, errors.New("project not found")
		}
		if !slices.Contains(selected, id) {
			selected = append(selected, id)
		}
	}
	return selected, nil
}

func (s *Service) PlanProjects(ids []string) (skillstore.ApplyPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	selected, err := s.projectBatch(ids)
	if err != nil {
		return skillstore.ApplyPlan{}, err
	}
	return s.store.PlanTargets(selected)
}

func (s *Service) ApplyProjects(ids []string) (AppState, error) {
	return s.mutate(func() error {
		selected, err := s.projectBatch(ids)
		if err != nil {
			return err
		}
		return s.store.ApplyTargets(selected)
	})
}

func (s *Service) ForceApplyProjects(ids []string, reviewed skillstore.ApplyPlan) (AppState, error) {
	return s.mutate(func() error {
		selected, err := s.projectBatch(ids)
		if err != nil {
			return err
		}
		return s.store.ForceApplyTargetsReviewed(selected, reviewed)
	})
}

func (s *Service) ReorderProjects(ids []string) (AppState, error) {
	return s.mutate(func() error { return s.store.ReorderProjects(ids) })
}
