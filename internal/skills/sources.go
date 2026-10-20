package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

const (
	SourceModeSelected = "selected"
	SourceModeAll      = "all"
)

func sourcePath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\\\x00\r\n") && !strings.HasPrefix(value, "/") && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../")
}

func sourceContains(scope, subpath string) bool {
	return scope == "." || scope == subpath || strings.HasPrefix(subpath, scope+"/")
}

func sourceIdentity(source Source) string {
	return source.Repository + "\x00" + source.Ref + "\x00" + source.Subpath
}

func migrateState(s State) State {
	if s.Schema != 1 {
		return s
	}
	s.Schema = schemaVersion
	s.Sources = []Source{}
	groups := map[string]int{}
	trackedPaths := map[string]bool{}
	for i := range s.Skills {
		skill := &s.Skills[i]
		skill.SourceID, skill.UpstreamRemoved = "", false
		subpath := skill.Subpath
		if subpath == "" {
			subpath = "."
		}
		// Schema 1 permitted arbitrary origin metadata and multiple explicit
		// IDs for a source path. Keep such local skills and their references,
		// but only associate origins that can be tracked unambiguously.
		if strings.TrimSpace(skill.Source) == "" || strings.ContainsAny(skill.Source+skill.Ref+skill.Revision, "\x00\r\n") || !sourcePath(subpath) {
			continue
		}
		key := skill.Source + "\x00" + skill.Ref
		originPath := key + "\x00" + subpath
		if trackedPaths[originPath] {
			continue
		}
		trackedPaths[originPath] = true
		index, exists := groups[key]
		if !exists {
			hash := sha256.Sum256([]byte(key))
			name := strings.TrimSuffix(path.Base(strings.TrimSuffix(skill.Source, "/")), ".git")
			if strings.TrimSpace(name) == "" || name == "." {
				name = skill.Source
			}
			index = len(s.Sources)
			groups[key] = index
			s.Sources = append(s.Sources, Source{ID: "source-" + hex.EncodeToString(hash[:12]), Name: name, Repository: skill.Source, Ref: skill.Ref, Subpath: ".", Mode: SourceModeSelected, Catalog: []SourceSkill{}})
		}
		source := &s.Sources[index]
		skill.SourceID, skill.UpstreamRemoved = source.ID, false
		// Early store callers could omit a root skill's subpath.
		skill.Subpath = subpath
		entry := SourceSkill{Name: skill.Name, Description: skill.Description, Subpath: skill.Subpath, Revision: skill.Revision, ContentHash: skill.ContentHash}
		if !slices.ContainsFunc(source.Catalog, func(item SourceSkill) bool { return item.Subpath == entry.Subpath }) {
			source.Catalog = append(source.Catalog, entry)
		}
	}
	return s
}

func validateSources(s State) error {
	sourceIDs := map[string]Source{}
	identities := map[string]bool{}
	for i, source := range s.Sources {
		identity := sourceIdentity(source)
		if !safeName.MatchString(source.ID) || strings.TrimSpace(source.Name) == "" || strings.TrimSpace(source.Repository) == "" || strings.ContainsAny(source.Repository+source.Ref+source.Revision, "\x00\r\n") || !sourcePath(source.Subpath) || (source.Mode != SourceModeSelected && source.Mode != SourceModeAll) || identities[identity] {
			return fmt.Errorf("invalid or duplicate source %q", source.ID)
		}
		if _, exists := sourceIDs[source.ID]; exists {
			return fmt.Errorf("duplicate source ID %q", source.ID)
		}
		for _, previous := range s.Sources[:i] {
			if previous.Repository == source.Repository && previous.Ref == source.Ref && (sourceContains(previous.Subpath, source.Subpath) || sourceContains(source.Subpath, previous.Subpath)) {
				return fmt.Errorf("source scope %q overlaps source %q; use disjoint scopes for the same repository and ref", source.Subpath, previous.Name)
			}
		}
		sourceIDs[source.ID], identities[identity] = source, true
		paths := map[string]bool{}
		for _, entry := range source.Catalog {
			// A newly discovered unreadable skill has no trusted content hash yet.
			// Its placeholder is informational and must never be synchronized.
			validHash := hashPattern.MatchString(entry.ContentHash) || (entry.SkipReason != "" && entry.ContentHash == "")
			if !validName(entry.Name) || !sourcePath(entry.Subpath) || !sourceContains(source.Subpath, entry.Subpath) || paths[entry.Subpath] || !validHash || strings.ContainsAny(entry.Revision, "\x00\r\n") || (entry.SkipReason != "" && entry.Removed) {
				return fmt.Errorf("invalid catalog entry in source %q", source.ID)
			}
			paths[entry.Subpath] = true
		}
	}
	linked := map[string]bool{}
	for _, skill := range s.Skills {
		if skill.SourceID == "" {
			if skill.UpstreamRemoved {
				return fmt.Errorf("unlinked skill %q cannot be marked removed upstream", skill.ID)
			}
			continue
		}
		source, exists := sourceIDs[skill.SourceID]
		if !exists || skill.Source != source.Repository || skill.Ref != source.Ref || !sourcePath(skill.Subpath) || !sourceContains(source.Subpath, skill.Subpath) {
			return fmt.Errorf("invalid source association for skill %q", skill.ID)
		}
		key := skill.SourceID + "\x00" + skill.Subpath
		if linked[key] {
			return fmt.Errorf("duplicate installed source path for skill %q", skill.ID)
		}
		linked[key] = true
	}
	return nil
}

// SaveSource creates a source or changes its name and subscription mode. A
// source's location is immutable so existing skills cannot silently change origin.
func (l *Store) SaveSource(source Source) (Source, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return Source{}, err
	}
	before := cloneState(s)
	source.Name = strings.TrimSpace(source.Name)
	source.Repository = strings.TrimSpace(source.Repository)
	if source.Subpath == "" {
		source.Subpath = "."
	}
	if source.Mode == "" {
		source.Mode = SourceModeSelected
	}
	if source.ID == "" {
		source.ID = id("source")
	}
	index := slices.IndexFunc(s.Sources, func(existing Source) bool { return existing.ID == source.ID })
	if index >= 0 {
		previous := s.Sources[index]
		if sourceIdentity(previous) != sourceIdentity(source) {
			return Source{}, errors.New("a source's repository, ref and scope cannot be changed")
		}
		previous.Name, previous.Mode = source.Name, source.Mode
		source = previous
		s.Sources[index] = source
	} else {
		// Refresh metadata must come from RecordSourceRefresh, never an edit form.
		source.Revision, source.RefreshedAt, source.LastError = "", "", ""
		source.Warnings = nil
		source.Catalog = []SourceSkill{}
		s.Sources = append(s.Sources, source)
		for i := range s.Skills {
			skill := &s.Skills[i]
			if skill.SourceID == "" && skill.Source == source.Repository && skill.Ref == source.Ref && sourceContains(source.Subpath, skill.Subpath) {
				skill.SourceID = source.ID
			}
		}
	}
	if err = l.validate(s); err != nil {
		return Source{}, err
	}
	if _, err = l.snapshot(before, "before saving source"); err != nil {
		return Source{}, err
	}
	if err = l.writeState(s); err != nil {
		return Source{}, err
	}
	source.Catalog = append([]SourceSkill{}, source.Catalog...)
	source.Warnings = append([]string(nil), source.Warnings...)
	return source, nil
}

// DeleteSource detaches its local skills without removing content or preset members.
func (l *Store) DeleteSource(sourceID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Sources, func(source Source) bool { return source.ID == sourceID })
	if index < 0 {
		return errors.New("source not found")
	}
	if _, err = l.snapshot(s, "before deleting source"); err != nil {
		return err
	}
	s.Sources = slices.Delete(s.Sources, index, index+1)
	for i := range s.Targets {
		previous := s.Targets[i]
		previous.EnabledSourceIDs = append([]string{}, previous.EnabledSourceIDs...)
		s.Targets[i].EnabledSourceIDs = slices.DeleteFunc(s.Targets[i].EnabledSourceIDs, func(id string) bool { return id == sourceID })
		stampTarget(&s.Targets[i], &previous)
	}
	for i := range s.Skills {
		if s.Skills[i].SourceID == sourceID {
			s.Skills[i].SourceID, s.Skills[i].UpstreamRemoved = "", false
		}
	}
	return l.writeState(s)
}

// RecordSourceRefresh updates the discovered inventory without installing content.
func (l *Store) RecordSourceRefresh(source Source) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Sources, func(existing Source) bool { return existing.ID == source.ID })
	if index < 0 {
		return errors.New("source not found")
	}
	previous := s.Sources[index]
	bindDefaultRef := previous.Ref == "" && previous.Revision == "" && previous.RefreshedAt == "" && source.Ref != ""
	if previous.Repository != source.Repository || previous.Subpath != source.Subpath || (previous.Ref != source.Ref && !bindDefaultRef) {
		return errors.New("source changed while refreshing")
	}
	before := cloneState(s)
	previous.Ref = source.Ref
	previous.Catalog = append([]SourceSkill{}, source.Catalog...)
	previous.Warnings = append([]string(nil), source.Warnings...)
	previous.Revision, previous.RefreshedAt, previous.LastError = source.Revision, source.RefreshedAt, ""
	if previous.RefreshedAt == "" {
		previous.RefreshedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	s.Sources[index] = previous
	for i := range s.Skills {
		// Removing a source leaves its skills detached with the resolved ref.
		// Re-registering with the default ref can reconnect them only after
		// the first refresh identifies that same branch.
		if bindDefaultRef && s.Skills[i].SourceID == "" && s.Skills[i].Source == previous.Repository && s.Skills[i].Ref == previous.Ref && sourceContains(previous.Subpath, s.Skills[i].Subpath) {
			s.Skills[i].SourceID = source.ID
		}
		if s.Skills[i].SourceID == source.ID {
			if bindDefaultRef {
				s.Skills[i].Ref = source.Ref
			}
			s.Skills[i].UpstreamRemoved = !slices.ContainsFunc(previous.Catalog, func(entry SourceSkill) bool { return entry.Subpath == s.Skills[i].Subpath && !entry.Removed })
		}
	}
	if err = l.validate(s); err != nil {
		return err
	}
	if _, err = l.snapshot(before, "before refreshing source"); err != nil {
		return err
	}
	return l.writeState(s)
}

func (l *Store) RecordSourceError(sourceID, message string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Sources, func(source Source) bool { return source.ID == sourceID })
	if index < 0 {
		return errors.New("source not found")
	}
	s.Sources[index].LastError = message
	return l.writeState(s)
}

func sourceCandidate(source Source, candidate Candidate) (Candidate, error) {
	skill := candidate.Skill
	if skill.Source != source.Repository || skill.Ref != source.Ref || skill.Revision != source.Revision || (skill.SourceID != "" && skill.SourceID != source.ID) {
		return Candidate{}, errors.New("candidate does not match the source revision")
	}
	index := slices.IndexFunc(source.Catalog, func(entry SourceSkill) bool { return entry.Subpath == skill.Subpath && !entry.Removed })
	if index < 0 {
		return Candidate{}, errors.New("candidate is no longer present in the source")
	}
	entry := source.Catalog[index]
	if entry.SkipReason != "" {
		return Candidate{}, fmt.Errorf("skill %q was skipped during refresh: %s", entry.Subpath, entry.SkipReason)
	}
	if skill.Name != entry.Name || skill.ContentHash != entry.ContentHash || (entry.Revision != "" && skill.Revision != entry.Revision) {
		return Candidate{}, errors.New("candidate does not match the refreshed catalog")
	}
	candidate.Skill.SourceID, candidate.Skill.UpstreamRemoved = source.ID, false
	return candidate, nil
}

// SyncSource commits an entire batch against one refreshed revision. Existing
// paths retain their identity so presets and deployments remain connected.
func (l *Store) SyncSource(sourceID, expectedRevision string, candidates []Candidate) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Sources, func(source Source) bool { return source.ID == sourceID })
	if index < 0 {
		return errors.New("source not found")
	}
	source := s.Sources[index]
	if expectedRevision == "" || source.Revision != expectedRevision {
		return errors.New("source changed; refresh the synchronization plan")
	}
	prepared := make([]Candidate, len(candidates))
	paths := map[string]bool{}
	for i, candidate := range candidates {
		prepared[i], err = sourceCandidate(source, candidate)
		if err != nil {
			return err
		}
		if paths[candidate.Skill.Subpath] {
			return errors.New("duplicate source path in synchronization plan")
		}
		paths[candidate.Skill.Subpath] = true
		previous := slices.IndexFunc(s.Skills, func(skill Skill) bool {
			return skill.SourceID == sourceID && skill.Subpath == candidate.Skill.Subpath
		})
		if previous >= 0 {
			prepared[i].Skill.ID = s.Skills[previous].ID
		} else {
			if slices.ContainsFunc(s.Skills, func(skill Skill) bool {
				return skill.Source == source.Repository && skill.Ref == source.Ref && skill.Subpath == candidate.Skill.Subpath
			}) {
				return errors.New("the source path is already installed outside this source")
			}
			// New source entries get a fresh identity. Caller-controlled or
			// repository-derived IDs must not replace an unrelated local skill.
			prepared[i].Skill.ID = id("skill")
		}
	}
	if len(prepared) == 0 {
		return nil
	}
	before := cloneState(s)
	if _, err = l.installCandidates(&s, prepared); err != nil {
		return err
	}
	if err = l.validate(s); err != nil {
		return err
	}
	for i := range s.Sources[index].Catalog {
		if paths[s.Sources[index].Catalog[i].Subpath] {
			s.Sources[index].Catalog[i].New = false
		}
	}
	if _, err = l.snapshot(before, "before synchronizing source"); err != nil {
		return err
	}
	return l.writeState(s)
}

// RelinkSourceSkill explicitly associates a moved upstream path with its old
// local identity. Preset references and InstalledAt are preserved.
func (l *Store) RelinkSourceSkill(skillID, sourceID string, candidate Candidate) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	skillIndex := slices.IndexFunc(s.Skills, func(skill Skill) bool { return skill.ID == skillID })
	sourceIndex := slices.IndexFunc(s.Sources, func(source Source) bool { return source.ID == sourceID })
	if skillIndex < 0 || sourceIndex < 0 {
		return errors.New("skill or source not found")
	}
	if s.Skills[skillIndex].SourceID != sourceID {
		return errors.New("skills can only be relinked within the same source")
	}
	candidate, err = sourceCandidate(s.Sources[sourceIndex], candidate)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(s.Skills, func(skill Skill) bool {
		return skill.ID != skillID && skill.SourceID == sourceID && skill.Subpath == candidate.Skill.Subpath
	}) {
		return errors.New("the new source path is already installed")
	}
	before := cloneState(s)
	candidate.Skill.ID = skillID
	if _, err = l.installCandidates(&s, []Candidate{candidate}); err != nil {
		return err
	}
	if err = l.validate(s); err != nil {
		return err
	}
	for i := range s.Sources[sourceIndex].Catalog {
		if s.Sources[sourceIndex].Catalog[i].Subpath == candidate.Skill.Subpath {
			s.Sources[sourceIndex].Catalog[i].New = false
		}
	}
	if _, err = l.snapshot(before, "before relinking source skill"); err != nil {
		return err
	}
	return l.writeState(s)
}
