package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	skillstore "racomi/internal/skills"
	"racomi/internal/sources"
)

type sourceRefreshError struct{ err error }

func (e *sourceRefreshError) Error() string { return e.err.Error() }
func (e *sourceRefreshError) Unwrap() error { return e.err }

func (s *Service) SaveSource(source skillstore.Source) (AppState, error) {
	return s.mutate(func() error {
		if source.ID == "" {
			var err error
			source, err = sources.NormalizeSource(source)
			if err != nil {
				return err
			}
			// Catalog and refresh status are owned by the service, never the UI.
			source.Catalog = []skillstore.SourceSkill{}
			source.Revision, source.RefreshedAt, source.LastError = "", "", ""
			source.Warnings = nil
		}
		_, err := s.store.SaveSource(source)
		return err
	})
}

func (s *Service) DeleteSource(id string) (AppState, error) {
	return s.mutate(func() error { return s.store.DeleteSource(id) })
}

func findSource(state skillstore.State, id string) (skillstore.Source, error) {
	for _, source := range state.Sources {
		if source.ID == id {
			return source, nil
		}
	}
	return skillstore.Source{}, errors.New("来源不存在")
}

func (s *Service) refreshSource(ctx context.Context, source skillstore.Source) (skillstore.Source, error) {
	return s.refreshSourceWithCache(ctx, source, true)
}

func (s *Service) refreshSourceWithCache(ctx context.Context, source skillstore.Source, fetch bool) (skillstore.Source, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	state, err := s.store.State()
	if err != nil {
		return source, err
	}
	source = sourceWithInstalledCatalog(source, state)
	var updated skillstore.Source
	var previewID string
	if fetch {
		updated, previewID, err = s.sources.RefreshSource(ctx, source)
	} else {
		updated, previewID, err = s.sources.RefreshCachedSource(ctx, source)
	}
	if err != nil {
		if recordErr := s.store.RecordSourceError(source.ID, err.Error()); recordErr != nil {
			return source, recordErr
		}
		return source, &sourceRefreshError{err}
	}
	defer s.sources.Release(previewID)
	if source.Ref == "" && updated.Ref != "" {
		// Resolve the default branch before considering detached local skills;
		// another branch's installed version must not seed this catalog.
		source.Ref = updated.Ref
		seeded := sourceWithInstalledCatalog(source, state)
		if len(seeded.Catalog) != len(source.Catalog) {
			updated, err = s.sources.CatalogForPreview(previewID, seeded)
			if err != nil {
				return source, err
			}
		}
	}
	if err = s.store.RecordSourceRefresh(updated); err != nil {
		return source, err
	}
	return updated, nil
}

// Imported registrations can have installed skills before their first catalog.
// Seed known paths so an unreadable directory cannot look like upstream removal.
func sourceWithInstalledCatalog(source skillstore.Source, state skillstore.State) skillstore.Source {
	source.Catalog = append([]skillstore.SourceSkill{}, source.Catalog...)
	known := make(map[string]bool, len(source.Catalog))
	for _, item := range source.Catalog {
		known[item.Subpath] = true
	}
	for _, skill := range state.Skills {
		associated := skill.SourceID == source.ID || (skill.SourceID == "" && skill.Source == source.Repository && skill.Ref == source.Ref)
		if !associated || !sourceContains(source.Subpath, skill.Subpath) || known[skill.Subpath] {
			continue
		}
		source.Catalog = append(source.Catalog, skillstore.SourceSkill{
			Name: skill.Name, Description: skill.Description, Subpath: skill.Subpath,
			Revision: skill.Revision, ContentHash: skill.ContentHash,
		})
		known[skill.Subpath] = true
	}
	return source
}

// A failed refresh is represented in persisted source status, so the UI can
// show the failure while retaining the last successful catalog and local copy.
func (s *Service) RefreshSource(ctx context.Context, id string) (AppState, error) {
	return s.mutate(func() error {
		state, err := s.store.State()
		if err != nil {
			return err
		}
		source, err := findSource(state, id)
		if err != nil {
			return err
		}
		_, refreshErr := s.refreshSource(ctx, source)
		var upstreamError *sourceRefreshError
		if refreshErr != nil && !errors.As(refreshErr, &upstreamError) {
			return refreshErr
		}
		return nil
	})
}

func sourceSyncPaths(state skillstore.State, source skillstore.Source, selected []string) ([]string, error) {
	paths := []string{}
	wanted := map[string]bool{}
	for _, path := range selected {
		wanted[path] = true
	}
	for _, item := range source.Catalog {
		if item.SkipReason != "" {
			if wanted[item.Subpath] {
				return nil, fmt.Errorf("skill %q was skipped during refresh: %s", item.Subpath, item.SkipReason)
			}
			continue
		}
		if item.Removed {
			continue
		}
		if source.Mode != "all" && !wanted[item.Subpath] {
			continue
		}
		delete(wanted, item.Subpath)
		unchanged := false
		for _, skill := range state.Skills {
			if skill.SourceID == source.ID && skill.Subpath == item.Subpath {
				unchanged = skill.ContentHash == item.ContentHash && skill.Name == item.Name
				break
			}
		}
		if !unchanged {
			paths = append(paths, item.Subpath)
		}
	}
	if source.Mode != "all" && len(wanted) != 0 {
		return nil, errors.New("所选技能已不在来源清单中，请重新查看同步计划")
	}
	return paths, nil
}

func (s *Service) syncSource(ctx context.Context, state skillstore.State, source skillstore.Source, revision string, selected []string) error {
	if source.LastError != "" {
		return errors.New("来源刷新失败，请成功刷新后再同步")
	}
	if source.Revision != revision || revision == "" {
		return errors.New("来源版本已变化，请重新查看同步计划")
	}
	paths, err := sourceSyncPaths(state, source, selected)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	candidates, previewID, err := s.sources.PrepareSourceSync(ctx, source, paths)
	if err != nil {
		return err
	}
	defer s.sources.Release(previewID)
	return s.store.SyncSource(source.ID, revision, candidates)
}

func (s *Service) SyncSource(ctx context.Context, id, revision string, paths []string) (AppState, error) {
	return s.mutate(func() error {
		state, err := s.store.State()
		if err != nil {
			return err
		}
		source, err := findSource(state, id)
		if err != nil {
			return err
		}
		return s.syncSource(ctx, state, source, revision, paths)
	})
}

func (s *Service) RelinkSourceSkill(ctx context.Context, skillID, sourceID, subpath string) (AppState, error) {
	return s.mutate(func() error {
		state, err := s.store.State()
		if err != nil {
			return err
		}
		source, err := findSource(state, sourceID)
		if err != nil {
			return err
		}
		if source.LastError != "" {
			return errors.New("来源刷新失败，请成功刷新后再重新关联")
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		candidates, previewID, err := s.sources.PrepareSourceSync(ctx, source, []string{subpath})
		if err != nil {
			return err
		}
		defer s.sources.Release(previewID)
		if len(candidates) != 1 {
			return errors.New("请选择一个可用的技能路径")
		}
		return s.store.RelinkSourceSkill(skillID, sourceID, candidates[0])
	})
}

// Existing discovery imports register a selected source and index its complete
// repository at exactly the reviewed commit. Selecting all is still selected
// mode; subscribing to future additions is always a separate choice.
func (s *Service) installFromPreview(previewID string, paths []string) error {
	preview, err := s.sources.DescribePreview(previewID)
	if err != nil {
		return err
	}
	candidates, err := s.sources.Candidates(previewID, paths)
	if err != nil || len(candidates) == 0 {
		return err
	}
	if preview.Local {
		if _, err = s.store.Install(candidates); err != nil {
			return err
		}
		return s.sources.Release(previewID)
	}
	state, err := s.store.State()
	if err != nil {
		return err
	}
	source := skillstore.Source{Repository: preview.Source, Ref: preview.Ref, Subpath: ".", Mode: "selected"}
	if preview.Hosted {
		source.Subpath = preview.Subpath
	}
	for _, existing := range state.Sources {
		normalized, normalizeErr := sources.NormalizeSource(existing)
		if normalizeErr != nil || normalized.Repository != preview.Source || (existing.Ref != preview.Ref && existing.Ref != "") {
			continue
		}
		containsAll := true
		for _, candidate := range candidates {
			containsAll = containsAll && sourceContains(existing.Subpath, candidate.Skill.Subpath)
		}
		if containsAll {
			if preview.Hosted && existing.Subpath != preview.Subpath {
				return errors.New("该托管技能已属于较大范围的来源，请刷新该来源并从来源同步")
			}
			source = existing
			break
		}
	}
	if source.ID == "" {
		source, err = sources.NormalizeSource(source)
		if err != nil {
			return err
		}
		source, err = s.store.SaveSource(source)
		if err != nil {
			return err
		}
	}
	// Do not roll a source catalog backwards when a stale discovery preview
	// would replace a more recently reviewed source. Request a fresh preview.
	if source.Revision != "" && source.Revision != preview.Revision {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if !s.sources.PreviewAdvances(ctx, previewID, source.Revision) {
			return errors.New("导入预览早于来源清单或来源历史已变化，请先刷新来源并从来源同步")
		}
	}
	source = sourceWithInstalledCatalog(source, state)
	source, err = s.sources.CatalogForPreview(previewID, source)
	if err != nil {
		return err
	}
	if err = s.store.RecordSourceRefresh(source); err != nil {
		return err
	}
	for i := range candidates {
		candidates[i].Skill.SourceID = source.ID
		candidates[i].Skill.Source = source.Repository
		candidates[i].Skill.Ref = source.Ref
	}
	if err = s.store.SyncSource(source.ID, source.Revision, candidates); err != nil {
		return err
	}
	return s.sources.Release(previewID)
}

func (s *Service) checkSourceUpdates(ctx context.Context) ([]sources.Update, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.store.State()
	if err != nil {
		return nil, err
	}
	refreshed := map[string]skillstore.Source{}
	failures := map[string]error{}
	fetched := map[string]bool{}
	for _, source := range state.Sources {
		key := source.Repository
		if normalized, normalizeErr := sources.NormalizeSource(source); normalizeErr == nil {
			key = normalized.Repository
		}
		updated, refreshErr := s.refreshSourceWithCache(ctx, source, !fetched[key])
		var upstreamError *sourceRefreshError
		if refreshErr != nil && !errors.As(refreshErr, &upstreamError) {
			return nil, refreshErr
		}
		refreshed[source.ID], failures[source.ID] = updated, refreshErr
		if refreshErr == nil {
			fetched[key] = true
		}
	}
	updates := make([]sources.Update, 0, len(state.Skills))
	for _, skill := range state.Skills {
		if skill.SourceID == "" {
			updates = append(updates, sources.Update{SkillID: skill.ID, Error: "未关联来源，本地版本已保留；添加来源后可恢复跟踪。"})
			continue
		}
		update := sources.Update{SkillID: skill.ID}
		if failed := failures[skill.SourceID]; failed != nil {
			update.Error = failed.Error()
		} else {
			source := refreshed[skill.SourceID]
			update.Revision, update.Removed = source.Revision, true
			for _, item := range source.Catalog {
				if item.Subpath == skill.Subpath && !item.Removed {
					update.Removed = false
					if item.SkipReason != "" {
						update.Error = item.SkipReason
						break
					}
					update.Available = item.ContentHash != skill.ContentHash || item.Name != skill.Name
					if update.Available {
						update.Summary = s.sources.UpdateSummary(ctx, source, skill)
						if update.Summary == "" {
							update.Summary = fmt.Sprintf("技能内容已变化，来源版本 %.12s。", source.Revision)
						}
					}
					break
				}
			}
			if update.Removed {
				update.Summary = "上游已移除，已保留本地版本与预设引用。"
			}
		}
		updates = append(updates, update)
	}
	return updates, nil
}

func sourceContains(scope, subpath string) bool {
	return scope == "." || scope == subpath || strings.HasPrefix(subpath, scope+"/")
}
