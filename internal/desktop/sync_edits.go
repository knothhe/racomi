package desktop

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	skillstore "racomi/internal/skills"
	"racomi/internal/sources"
)

const stalePresetEdit = "preset changed or was deleted while editing; reopen the editor and review the latest version"
const staleSourceEdit = "source changed or was deleted while editing; reopen the editor and review the latest version"

func samePresetEdit(a, b skillstore.Preset) bool {
	a.SkillIDs = append([]string{}, a.SkillIDs...)
	b.SkillIDs = append([]string{}, b.SkillIDs...)
	slices.Sort(a.SkillIDs)
	slices.Sort(b.SkillIDs)
	return reflect.DeepEqual(a, b)
}

func sameSourceEdit(a, b skillstore.Source) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Mode == b.Mode &&
		a.Repository == b.Repository && a.Ref == b.Ref && a.Subpath == b.Subpath
}

// SavePresetChecked keeps an editor opened before a background sync from
// silently overwriting the newly received record or resurrecting a deletion.
// Expected is the editor's detached snapshot, never its latest polled state.
func (s *Service) SavePresetChecked(preset skillstore.Preset, expected *skillstore.Preset) (AppState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.store.State()
	if err != nil {
		return AppState{}, err
	}
	if preset.ID == "" {
		if expected != nil {
			return AppState{}, errors.New(stalePresetEdit)
		}
	} else {
		index := slices.IndexFunc(state.Presets, func(current skillstore.Preset) bool { return current.ID == preset.ID })
		if expected == nil || expected.ID != preset.ID || index < 0 || !samePresetEdit(state.Presets[index], *expected) {
			return AppState{}, errors.New(stalePresetEdit)
		}
	}
	if _, err = s.store.SavePreset(preset); err != nil {
		return AppState{}, fmt.Errorf("cannot save preset; reopen the editor to review the latest library: %w", err)
	}
	return s.GetState()
}

// SaveSourceChecked compares the editable registration fields. Catalog refresh
// metadata may advance while an editor is open without invalidating its draft.
func (s *Service) SaveSourceChecked(source skillstore.Source, expected *skillstore.Source) (AppState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.store.State()
	if err != nil {
		return AppState{}, err
	}
	if source.ID == "" {
		if expected != nil {
			return AppState{}, errors.New(staleSourceEdit)
		}
		source, err = sources.NormalizeSource(source)
		if err != nil {
			return AppState{}, err
		}
		source.Catalog = []skillstore.SourceSkill{}
		source.Revision, source.RefreshedAt, source.LastError = "", "", ""
		source.Warnings = nil
	} else {
		index := slices.IndexFunc(state.Sources, func(current skillstore.Source) bool { return current.ID == source.ID })
		if expected == nil || expected.ID != source.ID || index < 0 || !sameSourceEdit(state.Sources[index], *expected) {
			return AppState{}, errors.New(staleSourceEdit)
		}
	}
	if _, err = s.store.SaveSource(source); err != nil {
		return AppState{}, fmt.Errorf("cannot save source; reopen the editor to review the latest library: %w", err)
	}
	return s.GetState()
}
