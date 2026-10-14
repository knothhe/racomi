package desktop

import (
	"context"
	"errors"
	"slices"

	skillstore "racomi/internal/skills"
	"racomi/internal/sources"
)

// UpdateSkills prepares every selected skill before committing any update. It
// uses the reviewed source catalogs, including for sources in all-skills mode.
func (s *Service) UpdateSkills(ctx context.Context, ids []string) (AppState, error) {
	return s.mutate(func() error {
		state, err := s.store.State()
		if err != nil {
			return err
		}
		selected := map[string]bool{}
		groups := map[string][]skillstore.Skill{}
		for _, id := range ids {
			if selected[id] {
				continue
			}
			selected[id] = true
			index := slices.IndexFunc(state.Skills, func(skill skillstore.Skill) bool { return skill.ID == id })
			if index < 0 {
				return errors.New("技能不存在")
			}
			skill := state.Skills[index]
			if skill.SourceID == "" {
				return errors.New("技能已解除来源关联，请添加来源后再更新")
			}
			if _, err = findSource(state, skill.SourceID); err != nil {
				return err
			}
			groups[skill.SourceID] = append(groups[skill.SourceID], skill)
		}
		type sourcePlan struct {
			source skillstore.Source
			paths  []string
			ids    map[string]string
		}
		plans := []sourcePlan{}
		for _, source := range state.Sources {
			skills := groups[source.ID]
			if len(skills) == 0 {
				continue
			}
			if source.LastError != "" {
				return errors.New("来源刷新失败，请成功刷新后再同步")
			}
			if source.Revision == "" {
				return errors.New("请先成功刷新来源，再同步技能")
			}
			paths := make([]string, 0, len(skills))
			ids := map[string]string{}
			for _, skill := range skills {
				if !slices.ContainsFunc(source.Catalog, func(item skillstore.SourceSkill) bool { return item.Subpath == skill.Subpath && !item.Removed }) {
					return errors.New("上游已移除此技能，已保留本地版本；请到来源查看或重新关联路径")
				}
				paths = append(paths, skill.Subpath)
				ids[skill.Subpath] = skill.ID
			}
			source.Mode = skillstore.SourceModeSelected
			paths, err = sourceSyncPaths(state, source, paths)
			if err != nil {
				return err
			}
			plans = append(plans, sourcePlan{source: source, paths: paths, ids: ids})
		}
		ctx, cancel := context.WithTimeout(ctx, sources.SourceOperationTimeout)
		defer cancel()
		prepared := []skillstore.Candidate{}
		for _, plan := range plans {
			candidates, previewID, err := s.sources.PrepareSourceSync(ctx, plan.source, plan.paths)
			if err != nil {
				return err
			}
			if previewID != "" {
				defer s.sources.Release(previewID)
			}
			for i := range candidates {
				candidates[i].Skill.ID = plan.ids[candidates[i].Skill.Subpath]
			}
			prepared = append(prepared, candidates...)
		}
		return s.store.UpdateSkills(prepared)
	})
}

func (s *Service) RemoveSkills(ids []string) (AppState, error) {
	return s.mutate(func() error { return s.store.RemoveSkills(ids) })
}
