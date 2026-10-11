package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ImportResult counts objects added to the store and objects reused without
// changing their existing metadata.
type ImportResult struct {
	AddedSkills    int `json:"addedSkills"`
	SkippedSkills  int `json:"skippedSkills"`
	AddedPresets   int `json:"addedPresets"`
	SkippedPresets int `json:"skippedPresets"`
	AddedSources   int `json:"addedSources"`
	SkippedSources int `json:"skippedSources"`
}

// MergeImport merges an external store in one transaction. All input IDs must
// be stable and valid. Existing objects, targets and target selections are never
// replaced; a conflicting identity cancels the entire import.
func (l *Store) MergeImport(candidates []Candidate, presets []Preset, sources []Source) (ImportResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return ImportResult{}, err
	}
	before := cloneState(s)
	result := ImportResult{}
	sourceIDs, err := mergeImportSources(&s, sources, &result)
	if err != nil {
		return ImportResult{}, err
	}

	// Keep a prospective index separate from the content installer. This lets
	// us reject missing references and collisions before staging any objects.
	prospective := cloneState(s)
	added := []Candidate{}
	skillIDs := map[string]string{}
	for _, candidate := range candidates {
		if _, exists := skillIDs[candidate.Skill.ID]; exists {
			return ImportResult{}, fmt.Errorf("duplicate imported skill ID %q", candidate.Skill.ID)
		}
		candidate, err = inspectImportCandidate(candidate)
		if err != nil {
			return ImportResult{}, err
		}
		incomingID := candidate.Skill.ID
		if mapped, exists := sourceIDs[candidate.Skill.SourceID]; exists {
			candidate.Skill.SourceID = mapped
		}
		if err = validateSources(State{Sources: prospective.Sources, Skills: []Skill{candidate.Skill}}); err != nil {
			return ImportResult{}, err
		}
		existing, found, matchErr := matchImportedSkill(prospective.Skills, candidate.Skill)
		if matchErr != nil {
			return ImportResult{}, matchErr
		}
		if found {
			// A skipped candidate still has its source tree inspected above and
			// its reused content object checked before reporting success.
			if slices.ContainsFunc(s.Skills, func(skill Skill) bool { return skill.ID == existing.ID }) {
				objectHash, hashErr := HashDir(filepath.Join(l.root, "objects", existing.ContentHash))
				if hashErr != nil {
					return ImportResult{}, fmt.Errorf("inspect reused skill %q: %w", existing.ID, hashErr)
				} else if objectHash != existing.ContentHash {
					return ImportResult{}, fmt.Errorf("existing content object for skill %q is damaged", existing.ID)
				}
			}
			skillIDs[incomingID] = existing.ID
			result.SkippedSkills++
			continue
		}
		skillIDs[incomingID] = incomingID
		prospective.Skills = append(prospective.Skills, candidate.Skill)
		added = append(added, candidate)
		result.AddedSkills++
	}
	if err = mergeImportPresets(&prospective, presets, skillIDs, &result); err != nil {
		return ImportResult{}, err
	}
	if err = l.validate(prospective); err != nil {
		return ImportResult{}, err
	}
	if result.AddedSkills+result.AddedPresets+result.AddedSources == 0 {
		return result, nil
	}
	s.Presets = prospective.Presets
	if _, err = l.installCandidates(&s, added); err != nil {
		return ImportResult{}, err
	}
	for _, candidate := range added {
		index := slices.IndexFunc(s.Skills, func(skill Skill) bool { return skill.ID == candidate.Skill.ID })
		if candidate.Skill.InstalledAt != "" {
			s.Skills[index].InstalledAt = candidate.Skill.InstalledAt
		}
		if candidate.Skill.UpdatedAt != "" {
			s.Skills[index].UpdatedAt = candidate.Skill.UpdatedAt
		}
	}
	if err = l.validate(s); err != nil {
		return ImportResult{}, err
	}
	// Check the final index size before creating a recovery snapshot.
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return ImportResult{}, err
	}
	if len(data)+1 > maxJSONBytes {
		return ImportResult{}, errors.New("技能与来源清单超过 32 MiB，请缩小来源扫描范围")
	}
	if _, err = l.snapshot(before, "before importing external skills"); err != nil {
		return ImportResult{}, err
	}
	if err = l.writeState(s); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func mergeImportSources(s *State, sources []Source, result *ImportResult) (map[string]string, error) {
	mapped := map[string]string{}
	for _, source := range sources {
		if _, exists := mapped[source.ID]; exists {
			return nil, fmt.Errorf("duplicate imported source ID %q", source.ID)
		}
		// Validate even sources that will be skipped, including their catalog.
		if err := validateSources(State{Sources: []Source{source}}); err != nil {
			return nil, err
		}
		if err := validateImportTime(source.RefreshedAt); err != nil {
			return nil, fmt.Errorf("invalid refresh time for imported source %q: %w", source.ID, err)
		}
		match := slices.IndexFunc(s.Sources, func(existing Source) bool { return existing.ID == source.ID })
		if match >= 0 && sourceIdentity(s.Sources[match]) != sourceIdentity(source) {
			return nil, fmt.Errorf("imported source ID %q conflicts with an existing source", source.ID)
		}
		if match < 0 {
			match = slices.IndexFunc(s.Sources, func(existing Source) bool {
				return existing.Repository == source.Repository && existing.Ref == source.Ref && sourceContains(existing.Subpath, source.Subpath)
			})
		}
		if match >= 0 {
			mapped[source.ID] = s.Sources[match].ID
			result.SkippedSources++
			continue
		}
		source.Catalog = append([]SourceSkill{}, source.Catalog...)
		source.Warnings = append([]string(nil), source.Warnings...)
		s.Sources = append(s.Sources, source)
		if err := validateSources(*s); err != nil {
			return nil, err
		}
		mapped[source.ID] = source.ID
		result.AddedSources++
	}
	return mapped, nil
}

func inspectImportCandidate(candidate Candidate) (Candidate, error) {
	skill := &candidate.Skill
	if !safeName.MatchString(skill.ID) || !validName(skill.Name) {
		return Candidate{}, fmt.Errorf("invalid imported skill ID or name %q", skill.ID)
	}
	if strings.ContainsAny(skill.Source+skill.Ref+skill.Revision, "\x00\r\n") || (skill.Source != "" && (strings.TrimSpace(skill.Source) == "" || !sourcePath(skill.Subpath))) {
		return Candidate{}, fmt.Errorf("invalid origin metadata for imported skill %q", skill.ID)
	}
	for _, value := range []string{skill.InstalledAt, skill.UpdatedAt} {
		if err := validateImportTime(value); err != nil {
			return Candidate{}, fmt.Errorf("invalid time for imported skill %q: %w", skill.ID, err)
		}
	}
	if err := noSymlinks(candidate.Path); err != nil {
		return Candidate{}, err
	}
	metadata, err := os.Lstat(filepath.Join(candidate.Path, "SKILL.md"))
	if err != nil {
		return Candidate{}, fmt.Errorf("imported skill %q must contain SKILL.md: %w", skill.ID, err)
	}
	if !metadata.Mode().IsRegular() {
		return Candidate{}, fmt.Errorf("imported skill %q SKILL.md must be a regular file", skill.ID)
	}
	actualHash, err := HashDir(candidate.Path)
	if err != nil {
		return Candidate{}, err
	}
	if skill.ContentHash != "" && skill.ContentHash != actualHash {
		return Candidate{}, fmt.Errorf("imported skill %q content differs from the inspected revision", skill.ID)
	}
	skill.ContentHash = actualHash
	return candidate, nil
}

func matchImportedSkill(skills []Skill, incoming Skill) (Skill, bool, error) {
	matches := []Skill{}
	for _, existing := range skills {
		if existing.ID == incoming.ID {
			matches = []Skill{existing}
			break
		}
		if incoming.Source != "" && existing.Source == incoming.Source && existing.Ref == incoming.Ref && existing.Subpath == incoming.Subpath {
			matches = append(matches, existing)
		} else if incoming.Source == "" && existing.Source == "" && existing.Name == incoming.Name && existing.ContentHash == incoming.ContentHash {
			matches = append(matches, existing)
		}
	}
	if len(matches) == 0 {
		return Skill{}, false, nil
	}
	if len(matches) != 1 {
		return Skill{}, false, fmt.Errorf("imported skill %q matches multiple existing skills", incoming.ID)
	}
	existing := matches[0]
	if existing.ContentHash != incoming.ContentHash {
		return Skill{}, false, fmt.Errorf("imported skill %q conflicts with existing skill %q: content differs", incoming.ID, existing.ID)
	}
	// Do not silently discard a rename, a changed description, or a different
	// origin. Revision and refresh bookkeeping may differ for identical content;
	// the existing revision, association and timestamps remain authoritative.
	if existing.Name != incoming.Name || existing.Description != incoming.Description || existing.Source != incoming.Source || existing.Ref != incoming.Ref || existing.Subpath != incoming.Subpath {
		return Skill{}, false, fmt.Errorf("imported skill %q conflicts with existing skill %q: metadata differs", incoming.ID, existing.ID)
	}
	return existing, true, nil
}

func mergeImportPresets(s *State, presets []Preset, skillIDs map[string]string, result *ImportResult) error {
	seen := map[string]bool{}
	for _, preset := range presets {
		preset.Name = strings.TrimSpace(preset.Name)
		if !safeName.MatchString(preset.ID) || preset.Name == "" || seen[preset.ID] {
			return fmt.Errorf("invalid or duplicate imported preset %q", preset.ID)
		}
		seen[preset.ID] = true
		if len(unique(preset.SkillIDs)) != len(preset.SkillIDs) {
			return fmt.Errorf("duplicate imported members in preset %q", preset.ID)
		}
		members := []string{}
		for _, member := range preset.SkillIDs {
			if mapped, exists := skillIDs[member]; exists {
				member = mapped
			}
			if !slices.ContainsFunc(s.Skills, func(skill Skill) bool { return skill.ID == member }) {
				return fmt.Errorf("imported preset %q refers to missing skill %q", preset.ID, member)
			}
			members = append(members, member)
		}
		preset.SkillIDs = unique(members)
		match := slices.IndexFunc(s.Presets, func(existing Preset) bool { return existing.ID == preset.ID })
		if match >= 0 && !sameImportMembers(s.Presets[match].SkillIDs, preset.SkillIDs) {
			return fmt.Errorf("imported preset ID %q conflicts with an existing preset", preset.ID)
		}
		if match < 0 {
			match = slices.IndexFunc(s.Presets, func(existing Preset) bool {
				return existing.Name == preset.Name && sameImportMembers(existing.SkillIDs, preset.SkillIDs)
			})
		}
		if match >= 0 {
			result.SkippedPresets++
		} else {
			s.Presets = append(s.Presets, preset)
			result.AddedPresets++
		}
	}
	return nil
}

func sameImportMembers(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for _, member := range first {
		if !slices.Contains(second, member) {
			return false
		}
	}
	return true
}

func validateImportTime(value string) error {
	if value == "" {
		return nil
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err
}
