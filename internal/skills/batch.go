package skills

import (
	"errors"
	"fmt"
	"slices"
)

// RemoveSkills preflights the whole selection before one index commit and
// recovery snapshot. Source registrations and immutable objects are retained.
func (l *Store) RemoveSkills(skillIDs []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, skillID := range skillIDs {
		if !slices.ContainsFunc(s.Skills, func(skill Skill) bool { return skill.ID == skillID }) {
			return errors.New("skill not found")
		}
		wanted[skillID] = true
	}
	if len(wanted) == 0 {
		return nil
	}
	for _, preset := range s.Presets {
		for _, skillID := range preset.SkillIDs {
			if wanted[skillID] {
				return fmt.Errorf("skill is still used by preset %q", preset.Name)
			}
		}
	}
	for _, target := range s.Targets {
		for _, skill := range desiredSkills(s, target) {
			if wanted[skill.ID] {
				return fmt.Errorf("skill is still selected for %q", target.Name)
			}
		}
	}
	if _, err = l.snapshot(s, "before deleting skill"); err != nil {
		return err
	}
	s.Skills = slices.DeleteFunc(s.Skills, func(skill Skill) bool { return wanted[skill.ID] })
	return l.writeState(s)
}

// UpdateSkills commits updates across sources together. Candidates may only
// replace the same installed source paths at their current reviewed revisions.
func (l *Store) UpdateSkills(candidates []Candidate) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	prepared := make([]Candidate, len(candidates))
	seen := map[string]bool{}
	paths := map[string]map[string]bool{}
	for i, candidate := range candidates {
		index := slices.IndexFunc(s.Skills, func(skill Skill) bool { return skill.ID == candidate.Skill.ID })
		if index < 0 {
			return errors.New("skill not found")
		}
		previous := s.Skills[index]
		if seen[previous.ID] {
			return errors.New("duplicate skill in update selection")
		}
		seen[previous.ID] = true
		if previous.SourceID == "" || candidate.Skill.SourceID != previous.SourceID || candidate.Skill.Subpath != previous.Subpath {
			return errors.New("skill source association changed; review the updates again")
		}
		sourceIndex := slices.IndexFunc(s.Sources, func(source Source) bool { return source.ID == previous.SourceID })
		if sourceIndex < 0 {
			return errors.New("source not found")
		}
		source := s.Sources[sourceIndex]
		if source.LastError != "" || source.Revision == "" {
			return errors.New("source must be refreshed successfully before updating")
		}
		prepared[i], err = sourceCandidate(source, candidate)
		if err != nil {
			return err
		}
		if paths[source.ID] == nil {
			paths[source.ID] = map[string]bool{}
		}
		paths[source.ID][previous.Subpath] = true
	}
	before := cloneState(s)
	if _, err = l.installCandidates(&s, prepared); err != nil {
		return err
	}
	for i := range s.Sources {
		for j := range s.Sources[i].Catalog {
			if paths[s.Sources[i].ID][s.Sources[i].Catalog[j].Subpath] {
				s.Sources[i].Catalog[j].New = false
			}
		}
	}
	if err = l.validate(s); err != nil {
		return err
	}
	reason := "before installing or updating skills"
	if len(prepared) == 1 {
		reason = "before synchronizing source"
	}
	if _, err = l.snapshot(before, reason); err != nil {
		return err
	}
	return l.writeState(s)
}
