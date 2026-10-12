package skills

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 2
const maxJSONBytes = 32 << 20

// Store keeps immutable content objects and a small, atomically replaced
// index. Handles opened for the same root share their mutation lock.
type Store struct {
	root string
	mu   *sync.Mutex
}

var locks sync.Map
var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var localIconPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

type snapshotFile struct {
	Info  SnapshotInfo `json:"info"`
	State State        `json:"state"`
}

func Open(root string) (*Store, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Resolve existing ancestors once so the application's own data root has
	// one identity even when macOS exposes /var through /private/var.
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	lock, _ := locks.LoadOrStore(absolute, &sync.Mutex{})
	store := &Store{root: absolute, mu: lock.(*sync.Mutex)}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, dir := range []string{"objects", "snapshots"} {
		path := filepath.Join(store.root, dir)
		if err = noSymlinks(path); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
	}
	_, err = store.readState()
	if errors.Is(err, os.ErrNotExist) {
		for _, dir := range []string{"objects", "snapshots"} {
			entries, readErr := os.ReadDir(filepath.Join(store.root, dir))
			if readErr != nil {
				return nil, readErr
			}
			if len(entries) != 0 {
				return nil, errors.New("skill index is missing but existing skill data was found")
			}
		}
		err = store.writeState(emptyState())
	}
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (l *Store) Root() string { return l.root }
func emptyState() State {
	return State{Schema: schemaVersion, Skills: []Skill{}, Presets: []Preset{}, Targets: []Target{}, Sources: []Source{}}
}
func cloneState(s State) State {
	s.Settings = normalizeSettings(s.Settings)
	s.Skills = append([]Skill{}, s.Skills...)
	s.Sources = append([]Source{}, s.Sources...)
	for i := range s.Sources {
		s.Sources[i].Catalog = append([]SourceSkill{}, s.Sources[i].Catalog...)
		s.Sources[i].Warnings = append([]string(nil), s.Sources[i].Warnings...)
	}
	s.Presets = append([]Preset{}, s.Presets...)
	for i := range s.Presets {
		s.Presets[i].SkillIDs = append([]string{}, s.Presets[i].SkillIDs...)
	}
	s.Targets = append([]Target{}, s.Targets...)
	for i := range s.Targets {
		s.Targets[i].EnabledSkillIDs = append([]string{}, s.Targets[i].EnabledSkillIDs...)
		s.Targets[i].EnabledPresetIDs = append([]string{}, s.Targets[i].EnabledPresetIDs...)
		s.Targets[i].Deployments = append([]Deployment{}, s.Targets[i].Deployments...)
		s.Targets[i] = normalizeTarget(s.Targets[i])
	}
	return s
}
func readJSON(path string, result any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular JSON file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxJSONBytes))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(result); err != nil {
		return fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("unexpected trailing JSON in %s", path)
	}
	return nil
}
func (l *Store) readState() (State, error) {
	for _, path := range []string{l.root, filepath.Join(l.root, "objects"), filepath.Join(l.root, "snapshots")} {
		if err := noSymlinks(path); err != nil {
			return State{}, err
		}
	}
	var s State
	if err := readJSON(filepath.Join(l.root, "state.json"), &s); err != nil {
		return State{}, err
	}
	s = cloneState(s)
	migrated := s.Schema == 1
	if migrated {
		s = migrateState(s)
	}
	if err := l.validate(s); err != nil {
		return State{}, fmt.Errorf("invalid store state: %w", err)
	}
	recomputePending(&s)
	if migrated {
		if err := l.writeState(s); err != nil {
			return State{}, fmt.Errorf("persist store migration: %w", err)
		}
	}
	return s, nil
}
func (l *Store) writeState(s State) error {
	s = cloneState(s)
	recomputePending(&s)
	if err := l.validate(s); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > maxJSONBytes {
		return errors.New("技能与来源清单超过 32 MiB，请缩小来源扫描范围")
	}
	return atomicJSON(filepath.Join(l.root, "state.json"), append(data, '\n'))
}
func (l *Store) State() (State, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.readState() }
func id(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}
func validName(name string) bool {
	if !safeName.MatchString(name) {
		return false
	}
	base := strings.ToUpper(strings.Split(name, ".")[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return !strings.HasSuffix(name, ".")
}
func unique(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	return result
}
func (l *Store) validate(s State) error {
	if s.Schema != schemaVersion {
		return fmt.Errorf("unsupported schema %d", s.Schema)
	}
	if err := validateSettings(s.Settings, s.Targets); err != nil {
		return err
	}
	if err := validateSources(s); err != nil {
		return err
	}
	skillIDs, presetIDs, targetIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, skill := range s.Skills {
		if !safeName.MatchString(skill.ID) || !validName(skill.Name) || !hashPattern.MatchString(skill.ContentHash) || skillIDs[skill.ID] {
			return fmt.Errorf("invalid or duplicate skill %q", skill.ID)
		}
		skillIDs[skill.ID] = true
	}
	for _, preset := range s.Presets {
		if !safeName.MatchString(preset.ID) || strings.TrimSpace(preset.Name) == "" || presetIDs[preset.ID] {
			return fmt.Errorf("invalid or duplicate preset %q", preset.ID)
		}
		// Only local glyph keys are persisted. Empty keeps the default layers icon.
		if preset.Icon != "" && !localIconPattern.MatchString(preset.Icon) {
			return errors.New("invalid preset icon")
		}
		presetIDs[preset.ID] = true
		if len(unique(preset.SkillIDs)) != len(preset.SkillIDs) {
			return fmt.Errorf("duplicate members in preset %q", preset.ID)
		}
		for _, member := range preset.SkillIDs {
			if !skillIDs[member] {
				return fmt.Errorf("preset %q refers to missing skill %q", preset.ID, member)
			}
		}
	}
	for i, target := range s.Targets {
		if !safeName.MatchString(target.ID) || targetIDs[target.ID] || strings.TrimSpace(target.Name) == "" {
			return fmt.Errorf("invalid or duplicate target %q", target.ID)
		}
		targetIDs[target.ID] = true
		if target.Kind != "global" && target.Kind != "project" {
			return fmt.Errorf("invalid target kind %q", target.Kind)
		}
		// The UI resolves this local glyph key; empty keeps legacy/default icons.
		if target.Icon != "" && !localIconPattern.MatchString(target.Icon) {
			return errors.New("invalid target icon")
		}
		paths := targetPaths(target)
		if len(paths) == 0 || target.Path != paths[0] || len(unique(paths)) != len(paths) {
			return errors.New("invalid target directories")
		}
		for pathIndex, path := range allTargetPaths(target) {
			for _, earlier := range allTargetPaths(target)[:pathIndex] {
				if within(path, earlier) || within(earlier, path) {
					return errors.New("target directories overlap")
				}
			}
			if err := l.checkTargetPath(path, false); err != nil {
				return err
			}
			for _, other := range s.Targets[:i] {
				for _, otherPath := range allTargetPaths(other) {
					if within(path, otherPath) || within(otherPath, path) {
						return errors.New("target paths overlap")
					}
				}
			}
		}
		if target.Kind == "project" {
			if !filepath.IsAbs(target.ProjectRoot) || filepath.Clean(target.ProjectRoot) != target.ProjectRoot {
				return errors.New("project root must be an absolute directory")
			}
			if len(target.DirectoryIDs)+len(target.CustomPaths) != len(paths) {
				return errors.New("invalid project directory selection")
			}
			for j, directoryID := range target.DirectoryIDs {
				directory, ok := projectDirectoryNames[directoryID]
				if !ok || paths[j] != filepath.Join(target.ProjectRoot, directory, "skills") {
					return errors.New("invalid project directory selection")
				}
			}
			for j, custom := range target.CustomPaths {
				stored, resolved, pathErr := projectCustomPath(target.ProjectRoot, custom)
				if pathErr != nil {
					return pathErr
				}
				if stored != custom || paths[len(target.DirectoryIDs)+j] != resolved {
					return errors.New("invalid custom project directory selection")
				}
			}
		}
		if len(unique(target.EnabledSkillIDs)) != len(target.EnabledSkillIDs) {
			return fmt.Errorf("duplicate enabled skills for %q", target.ID)
		}
		for _, skillID := range target.EnabledSkillIDs {
			if !skillIDs[skillID] {
				return fmt.Errorf("target %q refers to missing skill %q", target.ID, skillID)
			}
		}
		if len(unique(target.EnabledPresetIDs)) != len(target.EnabledPresetIDs) {
			return fmt.Errorf("duplicate enabled presets for %q", target.ID)
		}
		for _, presetID := range target.EnabledPresetIDs {
			if !presetIDs[presetID] {
				return fmt.Errorf("target %q refers to missing preset %q", target.ID, presetID)
			}
		}
		names, members := map[string]bool{}, map[string]bool{}
		for _, dep := range target.Deployments {
			if target.Kind == "global" && !slices.Contains(paths, deploymentPath(target, dep)) {
				return errors.New("deployment directory does not belong to the target")
			}
			if !validName(dep.Name) || !safeName.MatchString(dep.SkillID) || !hashPattern.MatchString(dep.ContentHash) || names[deploymentPath(target, dep)+"\x00"+strings.ToLower(dep.Name)] || members[deploymentPath(target, dep)+"\x00"+dep.SkillID] || (deploymentMode(dep) != DeploymentCopy && deploymentMode(dep) != DeploymentSymlink) {
				return fmt.Errorf("invalid deployment in target %q", target.ID)
			}
			names[deploymentPath(target, dep)+"\x00"+strings.ToLower(dep.Name)], members[deploymentPath(target, dep)+"\x00"+dep.SkillID] = true, true
		}
	}
	return nil
}
func (l *Store) checkTargetPath(path string, inspect bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return errors.New("target must be an absolute directory other than the filesystem root")
	}
	if within(l.root, path) || within(path, l.root) {
		return errors.New("target cannot overlap the racomi data directory")
	}
	if inspect {
		return noSymlinks(path)
	}
	return nil
}

func (l *Store) Install(candidates []Candidate) ([]Skill, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []Skill{}, nil
	}
	before := cloneState(s)
	installed, err := l.installCandidates(&s, candidates)
	if err != nil {
		return nil, err
	}
	if err = l.validate(s); err != nil {
		return nil, err
	}
	if _, err = l.snapshot(before, "before installing or updating skills"); err != nil {
		return nil, err
	}
	if err = l.writeState(s); err != nil {
		return nil, err
	}
	return installed, nil
}

// installCandidates only changes a private state copy. Content objects may be
// staged before all candidates are checked, but become visible to the store
// together when its index is committed.
func (l *Store) installCandidates(s *State, candidates []Candidate) ([]Skill, error) {
	installed := []Skill{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		skill := candidate.Skill
		if !validName(skill.Name) {
			return nil, fmt.Errorf("invalid skill directory name: %q", skill.Name)
		}
		if skill.ID == "" && skill.Source != "" {
			matches := []Skill{}
			for _, existing := range s.Skills {
				if existing.Source == skill.Source && existing.Ref == skill.Ref && existing.Subpath == skill.Subpath && (skill.SourceID == "" || existing.SourceID == skill.SourceID) {
					matches = append(matches, existing)
				}
			}
			if len(matches) > 1 {
				return nil, errors.New("multiple installed skills match this source path; specify a skill ID")
			}
			if len(matches) == 1 {
				skill.ID = matches[0].ID
			}
		}
		if skill.ID == "" {
			key := skill.Source + "\x00" + skill.Ref + "\x00" + skill.Subpath
			if skill.Source == "" {
				key += "\x00" + skill.Name
			}
			hash := sha256.Sum256([]byte(key))
			skill.ID = "skill-" + hex.EncodeToString(hash[:12])
		}
		if !safeName.MatchString(skill.ID) || seen[skill.ID] {
			return nil, fmt.Errorf("invalid or duplicate candidate ID %q", skill.ID)
		}
		seen[skill.ID] = true
		if err := noSymlinks(candidate.Path); err != nil {
			return nil, err
		}
		metadata, metadataErr := os.Lstat(filepath.Join(candidate.Path, "SKILL.md"))
		if metadataErr != nil {
			return nil, fmt.Errorf("skill must contain SKILL.md: %w", metadataErr)
		}
		if !metadata.Mode().IsRegular() {
			return nil, errors.New("skill SKILL.md must be a regular file")
		}
		actualHash, err := HashDir(candidate.Path)
		if err != nil {
			return nil, err
		}
		if skill.ContentHash != "" && skill.ContentHash != actualHash {
			return nil, errors.New("skill content differs from the inspected revision")
		}
		skill.ContentHash = actualHash
		object := filepath.Join(l.root, "objects", skill.ContentHash)
		if _, statErr := os.Lstat(object); errors.Is(statErr, os.ErrNotExist) {
			stage, stageErr := os.MkdirTemp(filepath.Join(l.root, "objects"), ".incoming-")
			if stageErr != nil {
				return nil, stageErr
			}
			content := filepath.Join(stage, "content")
			copyErr := CopyDir(candidate.Path, content)
			if copyErr == nil {
				var copied string
				copied, copyErr = HashDir(content)
				if copyErr == nil && copied != skill.ContentHash {
					copyErr = errors.New("skill changed while installing")
				}
			}
			if copyErr == nil {
				copyErr = os.Rename(content, object)
			}
			_ = os.RemoveAll(stage)
			if copyErr != nil {
				return nil, copyErr
			}
		} else if statErr != nil {
			return nil, statErr
		} else {
			actual, hashErr := HashDir(object)
			if hashErr != nil {
				return nil, hashErr
			}
			if actual != skill.ContentHash {
				return nil, errors.New("existing content object is damaged")
			}
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		skill.InstalledAt, skill.UpdatedAt = now, now
		index := slices.IndexFunc(s.Skills, func(existing Skill) bool { return existing.ID == skill.ID })
		if index >= 0 {
			previous := s.Skills[index]
			skill.InstalledAt = previous.InstalledAt
			if skill.SourceID == "" && skill.Source == previous.Source && skill.Ref == previous.Ref && skill.Subpath == previous.Subpath {
				skill.SourceID, skill.UpstreamRemoved = previous.SourceID, previous.UpstreamRemoved
			}
			s.Skills[index] = skill
		} else {
			s.Skills = append(s.Skills, skill)
		}
		installed = append(installed, skill)
	}
	return installed, nil
}

func (l *Store) RemoveSkill(skillID string) error {
	return l.RemoveSkills([]string{skillID})
}
func (l *Store) SavePreset(preset Preset) (Preset, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return Preset{}, err
	}
	before := cloneState(s)
	preset.Name = strings.TrimSpace(preset.Name)
	preset.Icon = strings.TrimSpace(preset.Icon)
	if preset.ID == "" {
		preset.ID = id("preset")
	}
	preset.SkillIDs = unique(preset.SkillIDs)
	index := slices.IndexFunc(s.Presets, func(p Preset) bool { return p.ID == preset.ID })
	if index < 0 {
		s.Presets = append(s.Presets, preset)
	} else {
		s.Presets[index] = preset
	}
	if err = l.validate(s); err != nil {
		return Preset{}, err
	}
	if _, err = l.snapshot(before, "before saving preset"); err != nil {
		return Preset{}, err
	}
	if err = l.writeState(s); err != nil {
		return Preset{}, err
	}
	return preset, nil
}
func (l *Store) DeletePreset(presetID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Presets, func(p Preset) bool { return p.ID == presetID })
	if index < 0 {
		return errors.New("preset not found")
	}
	if _, err = l.snapshot(s, "before deleting preset"); err != nil {
		return err
	}
	s.Presets = slices.Delete(s.Presets, index, index+1)
	for i := range s.Targets {
		s.Targets[i].EnabledPresetIDs = slices.DeleteFunc(s.Targets[i].EnabledPresetIDs, func(v string) bool { return v == presetID })
	}
	return l.writeState(s)
}
func (l *Store) SaveTarget(target Target) (Target, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return Target{}, err
	}
	before := cloneState(s)
	target, err = l.prepareTarget(s, target)
	if err != nil {
		return Target{}, err
	}
	index := slices.IndexFunc(s.Targets, func(t Target) bool { return t.ID == target.ID })
	if index >= 0 {
		s.Targets[index] = target
	} else {
		s.Targets = append(s.Targets, target)
	}
	target.EnabledSkillIDs = unique(target.EnabledSkillIDs)
	target.EnabledPresetIDs = unique(target.EnabledPresetIDs)
	s.Targets[slices.IndexFunc(s.Targets, func(t Target) bool { return t.ID == target.ID })] = target
	recomputePending(&s)
	if err = l.validate(s); err != nil {
		return Target{}, err
	}
	if _, err = l.snapshot(before, "before saving target"); err != nil {
		return Target{}, err
	}
	if err = l.writeState(s); err != nil {
		return Target{}, err
	}
	return s.Targets[slices.IndexFunc(s.Targets, func(t Target) bool { return t.ID == target.ID })], nil
}
func (l *Store) DeleteTarget(targetID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Targets, func(t Target) bool { return t.ID == targetID })
	if index < 0 {
		return errors.New("target not found")
	}
	if len(s.Targets[index].Deployments) != 0 {
		return errors.New("clear selected skills and presets and apply the target before deleting it")
	}
	if _, err = l.snapshot(s, "before deleting target"); err != nil {
		return err
	}
	s.Targets = slices.Delete(s.Targets, index, index+1)
	s.Settings.DefaultGlobalDirectories = slices.DeleteFunc(s.Settings.DefaultGlobalDirectories, func(value string) bool { return value == targetID })
	if len(s.Settings.DefaultGlobalDirectories) == 0 {
		s.Settings.DefaultGlobalDirectories = []string{"codex-user"}
	}
	return l.writeState(s)
}
func (l *Store) SetTargetPresets(targetID string, presetIDs []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Targets, func(t Target) bool { return t.ID == targetID })
	if index < 0 {
		return errors.New("target not found")
	}
	return l.setTargetSelection(s, index, s.Targets[index].EnabledSkillIDs, presetIDs)
}

// SetTargetSelection saves direct skills and Presets together without changing
// any deployed files. Both lists are validated before the index is replaced.
func (l *Store) SetTargetSelection(targetID string, skillIDs, presetIDs []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(s.Targets, func(t Target) bool { return t.ID == targetID })
	if index < 0 {
		return errors.New("target not found")
	}
	return l.setTargetSelection(s, index, skillIDs, presetIDs)
}

func (l *Store) setTargetSelection(s State, index int, skillIDs, presetIDs []string) error {
	before := cloneState(s)
	s.Targets[index].EnabledSkillIDs = unique(skillIDs)
	s.Targets[index].EnabledPresetIDs = unique(presetIDs)
	if err := l.validate(s); err != nil {
		return err
	}
	if _, err := l.snapshot(before, "before changing target presets"); err != nil {
		return err
	}
	return l.writeState(s)
}

func desiredSkills(s State, target Target) []Skill {
	ids := map[string]bool{}
	for _, skillID := range target.EnabledSkillIDs {
		ids[skillID] = true
	}
	for _, preset := range s.Presets {
		if slices.Contains(target.EnabledPresetIDs, preset.ID) {
			for _, skillID := range preset.SkillIDs {
				ids[skillID] = true
			}
		}
	}
	result := []Skill{}
	for _, skill := range s.Skills {
		if ids[skill.ID] {
			result = append(result, skill)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
func recomputePending(s *State) {
	for i := range s.Targets {
		target := &s.Targets[i]
		desired := desiredSkills(*s, *target)
		paths := targetPaths(*target)
		target.Pending = len(desired)*len(paths) != len(target.Deployments)
		for _, path := range paths {
			for _, skill := range desired {
				if !slices.ContainsFunc(target.Deployments, func(d Deployment) bool {
					return deploymentPath(*target, d) == path && d.SkillID == skill.ID && d.Name == skill.Name && d.ContentHash == skill.ContentHash && deploymentMode(d) == s.Settings.DeploymentMode
				}) {
					target.Pending = true
				}
			}
		}
	}
}
func (l *Store) snapshot(s State, reason string) (SnapshotInfo, error) {
	info := SnapshotInfo{ID: id("snapshot"), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: reason, SkillCount: len(s.Skills), PresetCount: len(s.Presets)}
	data, err := json.MarshalIndent(snapshotFile{Info: info, State: cloneState(s)}, "", "  ")
	if err != nil {
		return SnapshotInfo{}, err
	}
	if len(data)+1 > maxJSONBytes {
		return SnapshotInfo{}, errors.New("快照清单超过 32 MiB")
	}
	if err = atomicJSON(filepath.Join(l.root, "snapshots", info.ID+".json"), append(data, '\n')); err != nil {
		return SnapshotInfo{}, err
	}
	return info, nil
}
func (l *Store) Snapshot(reason string) (SnapshotInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.readState()
	if err != nil {
		return SnapshotInfo{}, err
	}
	return l.snapshot(s, reason)
}
func (l *Store) Snapshots() ([]SnapshotInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(l.root, "snapshots"))
	if err != nil {
		return nil, err
	}
	result := []SnapshotInfo{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var file snapshotFile
		if err = readJSON(filepath.Join(l.root, "snapshots", entry.Name()), &file); err != nil {
			return nil, err
		}
		if file.Info.ID+".json" != entry.Name() || !safeName.MatchString(file.Info.ID) {
			return nil, errors.New("invalid snapshot identity")
		}
		result = append(result, file.Info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}
func (l *Store) RestoreSnapshot(snapshotID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !safeName.MatchString(snapshotID) {
		return errors.New("invalid snapshot ID")
	}
	var file snapshotFile
	if err := readJSON(filepath.Join(l.root, "snapshots", snapshotID+".json"), &file); err != nil {
		return err
	}
	if file.Info.ID != snapshotID {
		return errors.New("snapshot identity mismatch")
	}
	return l.restore(file.State)
}
func (l *Store) Restore(state State) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.restore(state)
}
func (l *Store) restore(imported State) error {
	current, err := l.readState()
	if err != nil {
		return err
	}
	// Targets and deployment ownership belong to this computer. Restoring a
	// store does not write to any target and requires explicit re-enabling.
	imported = migrateState(cloneState(imported))
	if imported.Schema != schemaVersion {
		return errors.New("unsupported backup schema")
	}
	imported.Settings = current.Settings
	imported.Targets = current.Targets
	for i := range imported.Targets {
		imported.Targets[i].EnabledSkillIDs = []string{}
		imported.Targets[i].EnabledPresetIDs = []string{}
	}
	if err = l.validate(imported); err != nil {
		return err
	}
	for _, skill := range imported.Skills {
		actual, hashErr := HashDir(filepath.Join(l.root, "objects", skill.ContentHash))
		if hashErr != nil {
			return hashErr
		}
		if actual != skill.ContentHash {
			return fmt.Errorf("content object is damaged: %s", skill.Name)
		}
	}
	if _, err = l.snapshot(current, "before restoring skills"); err != nil {
		return err
	}
	return l.writeState(imported)
}
