package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// SharedState is the portable library. Deployment ownership, target paths and
// credentials remain on their device; content is transferred separately by hash.
type SharedState struct {
	Settings Settings `json:"settings"`
	Skills   []Skill  `json:"skills"`
	Sources  []Source `json:"sources"`
	Presets  []Preset `json:"presets"`
}

type SyncConflict struct {
	Key    string          `json:"key"`
	Kind   string          `json:"kind"`
	Name   string          `json:"name"`
	Local  json.RawMessage `json:"local"`
	Remote json.RawMessage `json:"remote"`
}

var builtinGlobalDirectories = []string{"codex-user", "codex-legacy", "claude-user", "cursor-user"}

func localSource(repository string) bool {
	value := strings.TrimSpace(repository)
	return strings.HasPrefix(strings.ToLower(value), "file:") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, `.\`) || strings.HasPrefix(value, `..\`) || !relativeProjectTemplatePath(value)
}

// ExportShared makes a detached, deterministic snapshot without local paths or
// refresh diagnostics. Local skills still travel, with their origin detached.
func ExportShared(state State) SharedState {
	state = cloneState(state)
	result := SharedState{Settings: state.Settings, Skills: state.Skills, Sources: []Source{}, Presets: state.Presets}
	localIDs := map[string]bool{}
	for _, source := range state.Sources {
		if localSource(source.Repository) {
			localIDs[source.ID] = true
			continue
		}
		source.LastError, source.RefreshedAt, source.Warnings = "", "", nil
		source.Catalog = slices.DeleteFunc(source.Catalog, func(entry SourceSkill) bool { return entry.SkipReason != "" && entry.ContentHash == "" })
		for i := range source.Catalog {
			source.Catalog[i].SkipReason, source.Catalog[i].New = "", false
		}
		result.Sources = append(result.Sources, source)
	}
	for i := range result.Skills {
		skill := &result.Skills[i]
		if localSource(skill.Source) || localIDs[skill.SourceID] {
			skill.Source, skill.Ref, skill.Subpath, skill.SourceID, skill.Revision = "", "", "", "", ""
			skill.UpstreamRemoved = false
		}
	}
	result.Settings.CustomProjectDirectories = slices.DeleteFunc(result.Settings.CustomProjectDirectories, func(directory ProjectDirectory) bool { return !relativeProjectTemplatePath(directory.Path) })
	result.Settings.DefaultProjectDirectories = slices.DeleteFunc(result.Settings.DefaultProjectDirectories, func(value string) bool {
		_, builtin := projectDirectoryNames[value]
		return !builtin && !slices.ContainsFunc(result.Settings.CustomProjectDirectories, func(directory ProjectDirectory) bool { return directory.ID == value })
	})
	result.Settings.DefaultGlobalDirectories = slices.DeleteFunc(result.Settings.DefaultGlobalDirectories, func(value string) bool { return !slices.Contains(builtinGlobalDirectories, value) })
	return canonicalShared(result)
}

func EmptyShared() SharedState { return ExportShared(emptyState()) }

func canonicalShared(shared SharedState) SharedState {
	s := cloneState(State{Settings: shared.Settings, Skills: shared.Skills, Sources: shared.Sources, Presets: shared.Presets})
	// A fresh device's implicit UI preferences are the common default, not
	// three independent edits against an empty checkpoint. Keep these defaults
	// in the shared representation so old local states can still migrate their
	// saved WebView preferences before enabling synchronization.
	if s.Settings.Theme == "" {
		s.Settings.Theme = "system"
	}
	if s.Settings.Locale == "" {
		s.Settings.Locale = "system"
	}
	if s.Settings.NavLayout == "" {
		s.Settings.NavLayout = "auto"
	}
	for i := range s.Presets {
		sort.Strings(s.Presets[i].SkillIDs)
	}
	for i := range s.Sources {
		sort.Slice(s.Sources[i].Catalog, func(a, b int) bool { return s.Sources[i].Catalog[a].Subpath < s.Sources[i].Catalog[b].Subpath })
	}
	sort.Slice(s.Skills, func(i, j int) bool { return s.Skills[i].ID < s.Skills[j].ID })
	sort.Slice(s.Sources, func(i, j int) bool { return s.Sources[i].ID < s.Sources[j].ID })
	sort.Slice(s.Presets, func(i, j int) bool { return s.Presets[i].ID < s.Presets[j].ID })
	sort.Slice(s.Settings.CustomProjectDirectories, func(i, j int) bool {
		return s.Settings.CustomProjectDirectories[i].ID < s.Settings.CustomProjectDirectories[j].ID
	})
	sort.Strings(s.Settings.DefaultProjectDirectories)
	sort.Strings(s.Settings.DefaultGlobalDirectories)
	return SharedState{Settings: s.Settings, Skills: s.Skills, Sources: s.Sources, Presets: s.Presets}
}

func validateSharedStructure(shared SharedState) error {
	s := State{Schema: schemaVersion, Settings: normalizeSettings(shared.Settings), Skills: shared.Skills, Sources: shared.Sources, Presets: shared.Presets}
	// A portable selection can be empty when all chosen directories belong to
	// one device. Validate the rest without inserting defaults into shared data.
	if len(s.Settings.DefaultProjectDirectories) == 0 {
		s.Settings.DefaultProjectDirectories = []string{"agents"}
	}
	if len(s.Settings.DefaultGlobalDirectories) == 0 {
		s.Settings.DefaultGlobalDirectories = []string{"codex-user"}
	}
	for _, source := range shared.Sources {
		if localSource(source.Repository) {
			return errors.New("shared sources cannot contain local repository paths")
		}
	}
	for _, skill := range shared.Skills {
		if localSource(skill.Source) {
			return errors.New("shared skills cannot contain local origin paths")
		}
		if strings.ContainsAny(skill.Source+skill.Ref+skill.Revision, "\x00\r\n") || (skill.Source != "" && (!sourcePath(skill.Subpath) || strings.TrimSpace(skill.Source) == "")) {
			return fmt.Errorf("invalid shared origin metadata for skill %q", skill.ID)
		}
		for _, timestamp := range []string{skill.InstalledAt, skill.UpdatedAt} {
			if err := validateImportTime(timestamp); err != nil {
				return fmt.Errorf("invalid shared timestamp for skill %q: %w", skill.ID, err)
			}
		}
	}
	for _, directory := range shared.Settings.CustomProjectDirectories {
		if !relativeProjectTemplatePath(directory.Path) {
			return errors.New("shared directory templates must use relative paths")
		}
	}
	for _, directory := range shared.Settings.DefaultGlobalDirectories {
		if !slices.Contains(builtinGlobalDirectories, directory) {
			return errors.New("shared defaults cannot contain custom global directories")
		}
	}
	return (&Store{}).validate(s)
}

// alignSharedIdentities preserves IDs already in the common baseline. Two
// independently registered copies of one source/path otherwise use the same
// deterministic ID, with all preset references remapped before comparison.
func alignSharedIdentities(states ...*SharedState) {
	sourceIDs, skillIDs := map[string]string{}, map[string]string{}
	type originTimes struct {
		count                  int
		inBase, equivalent     bool
		record                 Skill
		installedAt, updatedAt string
	}
	times := map[string]*originTimes{}
	for n, state := range states {
		for _, source := range state.Sources {
			key := sourceIdentity(source)
			if previous, exists := sourceIDs[key]; !exists || (n != 0 && !sourceIDIn(*states[0], previous) && source.ID < previous) {
				sourceIDs[key] = source.ID
			}
		}
		for _, skill := range state.Skills {
			if key := sharedSkillIdentity(skill); key != "" {
				if previous, exists := skillIDs[key]; !exists || (n != 0 && !skillIDIn(*states[0], previous) && skill.ID < previous) {
					skillIDs[key] = skill.ID
				}
			}
		}
	}
	for n, state := range states {
		sourceMap, skillMap := map[string]string{}, map[string]string{}
		for i := range state.Sources {
			source := &state.Sources[i]
			sourceMap[source.ID] = sourceIDs[sourceIdentity(*source)]
			source.ID = sourceMap[source.ID]
		}
		for i := range state.Skills {
			skill := &state.Skills[i]
			if mapped := sourceMap[skill.SourceID]; mapped != "" {
				skill.SourceID = mapped
			}
			if key := sharedSkillIdentity(*skill); key != "" {
				skillMap[skill.ID] = skillIDs[key]
				skill.ID = skillMap[skill.ID]
				record := *skill
				record.InstalledAt, record.UpdatedAt = "", ""
				metadata := times[key]
				if metadata == nil {
					metadata = &originTimes{equivalent: true, record: record, installedAt: skill.InstalledAt, updatedAt: skill.UpdatedAt}
					times[key] = metadata
				}
				metadata.count++
				metadata.inBase = metadata.inBase || n == 0
				metadata.equivalent = metadata.equivalent && record == metadata.record
				if skill.InstalledAt < metadata.installedAt {
					metadata.installedAt = skill.InstalledAt
				}
				if skill.UpdatedAt > metadata.updatedAt {
					metadata.updatedAt = skill.UpdatedAt
				}
			}
		}
		for i := range state.Presets {
			for j, member := range state.Presets[i].SkillIDs {
				if mapped := skillMap[member]; mapped != "" {
					state.Presets[i].SkillIDs[j] = mapped
				}
			}
			state.Presets[i].SkillIDs = unique(state.Presets[i].SkillIDs)
		}
		*state = canonicalShared(*state)
	}
	for _, state := range states {
		for i := range state.Skills {
			skill := &state.Skills[i]
			metadata := times[sharedSkillIdentity(*skill)]
			// Independent installations can have the same deterministic ID but
			// different clocks. Only normalize otherwise equivalent additions;
			// records in the common baseline retain their edit history.
			if metadata != nil && metadata.count > 1 && !metadata.inBase && metadata.equivalent {
				skill.InstalledAt, skill.UpdatedAt = metadata.installedAt, metadata.updatedAt
			}
		}
	}
}

func sourceIDIn(state SharedState, id string) bool {
	return slices.ContainsFunc(state.Sources, func(source Source) bool { return source.ID == id })
}
func skillIDIn(state SharedState, id string) bool {
	return slices.ContainsFunc(state.Skills, func(skill Skill) bool { return skill.ID == id })
}
func sharedSkillIdentity(skill Skill) string {
	if skill.Source == "" || skill.Subpath == "" {
		return ""
	}
	return skill.Source + "\x00" + skill.Ref + "\x00" + skill.Subpath
}

type syncMerger struct {
	resolutions map[string]string
	conflicts   map[string]SyncConflict
}

func (m *syncMerger) conflict(key, kind, name string, local, remote any) string {
	if resolution := m.resolutions[key]; resolution != "" {
		return resolution
	}
	l, _ := json.Marshal(local)
	r, _ := json.Marshal(remote)
	m.conflicts[key] = SyncConflict{Key: key, Kind: kind, Name: name, Local: l, Remote: r}
	return "local"
}

func mergeValue[T any](m *syncMerger, key, kind, name string, base, local, remote *T) *T {
	if reflect.DeepEqual(local, remote) || reflect.DeepEqual(base, remote) {
		return local
	}
	if reflect.DeepEqual(base, local) {
		return remote
	}
	if m.conflict(key, kind, name, local, remote) == "remote" {
		return remote
	}
	return local
}

func mergeRecords[T any](m *syncMerger, kind string, base, local, remote []T, identity func(T) string, name func(T) string) []T {
	index := func(values []T) map[string]*T {
		result := map[string]*T{}
		for i := range values {
			result[identity(values[i])] = &values[i]
		}
		return result
	}
	b, l, r := index(base), index(local), index(remote)
	ids := []string{}
	for _, records := range []map[string]*T{b, l, r} {
		for id := range records {
			ids = append(ids, id)
		}
	}
	ids = unique(ids)
	sort.Strings(ids)
	result := []T{}
	for _, id := range ids {
		label := id
		for _, value := range []*T{l[id], r[id], b[id]} {
			if value != nil {
				label = name(*value)
				break
			}
		}
		if merged := mergeValue(m, kind+":"+id, kind, label, b[id], l[id], r[id]); merged != nil {
			result = append(result, *merged)
		}
	}
	return result
}

// MergeShared performs three-way merging against the last acknowledged shared
// state. Absence is a deletion, so an old offline device cannot resurrect it.
// Unresolved conflicts are returned without guessing which edit to discard.
func MergeShared(base, local, remote SharedState, resolutions map[string]string) (SharedState, []SyncConflict, error) {
	for _, value := range resolutions {
		if value != "local" && value != "remote" {
			return SharedState{}, nil, errors.New("sync conflict resolution must be local or remote")
		}
	}
	for _, shared := range []SharedState{base, local, remote} {
		if err := validateSharedStructure(shared); err != nil {
			return SharedState{}, nil, fmt.Errorf("invalid shared library: %w", err)
		}
	}
	base, local, remote = canonicalShared(base), canonicalShared(local), canonicalShared(remote)
	alignSharedIdentities(&base, &local, &remote)
	for _, shared := range []SharedState{base, local, remote} {
		if err := validateSharedStructure(shared); err != nil {
			return SharedState{}, nil, fmt.Errorf("ambiguous shared identities: %w", err)
		}
	}
	m := &syncMerger{resolutions: resolutions, conflicts: map[string]SyncConflict{}}
	result := SharedState{}
	result.Skills = mergeRecords(m, "skill", base.Skills, local.Skills, remote.Skills, func(v Skill) string { return v.ID }, func(v Skill) string { return v.Name })
	result.Sources = mergeRecords(m, "source", base.Sources, local.Sources, remote.Sources, func(v Source) string { return v.ID }, func(v Source) string { return v.Name })
	result.Presets = mergeRecords(m, "preset", base.Presets, local.Presets, remote.Presets, func(v Preset) string { return v.ID }, func(v Preset) string { return v.Name })
	b, l, r := reflect.ValueOf(base.Settings), reflect.ValueOf(local.Settings), reflect.ValueOf(remote.Settings)
	out := reflect.ValueOf(&result.Settings).Elem()
	for i := 0; i < out.NumField(); i++ {
		field := strings.Split(out.Type().Field(i).Tag.Get("json"), ",")[0]
		bv, lv, rv := b.Field(i).Interface(), l.Field(i).Interface(), r.Field(i).Interface()
		merged := mergeValue(m, "settings:"+field, "settings", field, &bv, &lv, &rv)
		out.Field(i).Set(reflect.ValueOf(*merged))
	}
	protectSharedReferences(m, base, local, remote, &result)
	for _, directory := range result.Settings.DefaultProjectDirectories {
		_, builtin := projectDirectoryNames[directory]
		if builtin || slices.ContainsFunc(result.Settings.CustomProjectDirectories, func(d ProjectDirectory) bool { return d.ID == directory }) {
			continue
		}
		if m.conflict("settings:customProjectDirectories", "settings", "customProjectDirectories", local.Settings.CustomProjectDirectories, remote.Settings.CustomProjectDirectories) == "remote" {
			result.Settings.CustomProjectDirectories = remote.Settings.CustomProjectDirectories
		} else {
			result.Settings.CustomProjectDirectories = local.Settings.CustomProjectDirectories
		}
		break
	}
	result.Settings.DefaultProjectDirectories = slices.DeleteFunc(result.Settings.DefaultProjectDirectories, func(id string) bool {
		_, builtin := projectDirectoryNames[id]
		return !builtin && !slices.ContainsFunc(result.Settings.CustomProjectDirectories, func(d ProjectDirectory) bool { return d.ID == id })
	})
	result = canonicalShared(result)
	conflicts := make([]SyncConflict, 0, len(m.conflicts))
	for _, conflict := range m.conflicts {
		conflicts = append(conflicts, conflict)
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Key < conflicts[j].Key })
	if len(conflicts) == 0 {
		if err := validateSharedStructure(result); err != nil {
			return SharedState{}, nil, fmt.Errorf("merged library is invalid: %w", err)
		}
	}
	return result, conflicts, nil
}

func protectSharedReferences(m *syncMerger, base, local, remote SharedState, result *SharedState) {
	findSkill := func(state SharedState, id string) *Skill {
		for i := range state.Skills {
			if state.Skills[i].ID == id {
				return &state.Skills[i]
			}
		}
		return nil
	}
	findSource := func(state SharedState, id string) *Source {
		for i := range state.Sources {
			if state.Sources[i].ID == id {
				return &state.Sources[i]
			}
		}
		return nil
	}
	// A deletion also conflicts with new/edited records that depend on it.
	// Otherwise merging an added preset could silently remove its new member.
	for _, original := range base.Skills {
		ls, rs := findSkill(local, original.ID), findSkill(remote, original.ID)
		if (ls == nil) == (rs == nil) || skillIDIn(*result, original.ID) {
			continue
		}
		other := remote
		if rs == nil {
			other = local
		}
		for _, preset := range other.Presets {
			previous := slices.IndexFunc(base.Presets, func(p Preset) bool { return p.ID == preset.ID })
			if slices.Contains(preset.SkillIDs, original.ID) && (previous < 0 || !reflect.DeepEqual(base.Presets[previous], preset)) {
				choice := m.conflict("skill:"+original.ID, "skill", original.Name, ls, rs)
				selected := ls
				if choice == "remote" {
					selected = rs
				}
				if selected != nil {
					result.Skills = append(result.Skills, *selected)
				}
				break
			}
		}
	}
	for _, original := range base.Sources {
		ls, rs := findSource(local, original.ID), findSource(remote, original.ID)
		if (ls == nil) == (rs == nil) || sourceIDIn(*result, original.ID) {
			continue
		}
		other := remote
		if rs == nil {
			other = local
		}
		for _, skill := range other.Skills {
			if skill.SourceID == original.ID && !reflect.DeepEqual(findSkill(base, skill.ID), &skill) {
				choice := m.conflict("source:"+original.ID, "source", original.Name, ls, rs)
				selected := ls
				if choice == "remote" {
					selected = rs
				}
				if selected != nil {
					result.Sources = append(result.Sources, *selected)
				}
				break
			}
		}
	}
	for i := range result.Presets {
		result.Presets[i].SkillIDs = slices.DeleteFunc(result.Presets[i].SkillIDs, func(id string) bool { return !skillIDIn(*result, id) })
	}
	for i := range result.Skills {
		if skill := &result.Skills[i]; skill.SourceID != "" && !sourceIDIn(*result, skill.SourceID) {
			skill.SourceID, skill.UpstreamRemoved = "", false
		}
	}
}

func (l *Store) validateSharedObjects(shared SharedState) error {
	if err := validateSharedStructure(shared); err != nil {
		return err
	}
	checked := map[string]bool{}
	for _, skill := range shared.Skills {
		if checked[skill.ContentHash] {
			continue
		}
		actual, err := HashDir(filepath.Join(l.root, "objects", skill.ContentHash))
		if err != nil {
			return fmt.Errorf("read synchronized skill %q: %w", skill.Name, err)
		}
		if actual != skill.ContentHash {
			return fmt.Errorf("synchronized content object is damaged: %s", skill.Name)
		}
		checked[skill.ContentHash] = true
	}
	return nil
}

func (l *Store) ValidateShared(shared SharedState) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, err := l.readState()
	if err != nil {
		return err
	}
	if err = l.validateSharedObjects(shared); err != nil {
		return err
	}
	_, err = l.prepareShared(shared, current)
	return err
}

func settingsWithLocalPaths(shared, current Settings) (Settings, error) {
	result := normalizeSettings(shared)
	for _, directory := range current.CustomProjectDirectories {
		if relativeProjectTemplatePath(directory.Path) {
			continue
		}
		if slices.ContainsFunc(result.CustomProjectDirectories, func(d ProjectDirectory) bool { return d.ID == directory.ID }) {
			return Settings{}, fmt.Errorf("shared template %q conflicts with a local absolute template; edit or remove the local template before synchronizing", directory.Name)
		}
		result.CustomProjectDirectories = append(result.CustomProjectDirectories, directory)
		if slices.Contains(current.DefaultProjectDirectories, directory.ID) {
			result.DefaultProjectDirectories = append(result.DefaultProjectDirectories, directory.ID)
		}
	}
	for _, directory := range current.DefaultGlobalDirectories {
		if !slices.Contains(builtinGlobalDirectories, directory) {
			result.DefaultGlobalDirectories = append(result.DefaultGlobalDirectories, directory)
		}
	}
	result.DefaultProjectDirectories = unique(result.DefaultProjectDirectories)
	result.DefaultGlobalDirectories = unique(result.DefaultGlobalDirectories)
	// Apply fallbacks only after restoring this device's selections, so sync
	// does not enable a built-in alongside an intentionally local-only default.
	if len(result.DefaultProjectDirectories) == 0 {
		result.DefaultProjectDirectories = []string{"agents"}
	}
	if len(result.DefaultGlobalDirectories) == 0 {
		result.DefaultGlobalDirectories = []string{"codex-user"}
	}
	return result, nil
}

func (l *Store) ApplyShared(shared SharedState) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.applyShared(shared, nil)
}

// ApplySharedIfCurrent prevents a network round trip from overwriting an edit
// made after the client exported its local state.
func (l *Store) ApplySharedIfCurrent(shared, expected SharedState) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.applyShared(shared, &expected)
}

func (l *Store) applyShared(shared SharedState, expected *SharedState) error {
	current, err := l.readState()
	if err != nil {
		return err
	}
	if expected != nil && !reflect.DeepEqual(ExportShared(current), canonicalShared(*expected)) {
		return errors.New("local library changed while synchronizing; synchronize again")
	}
	if err = l.validateSharedObjects(shared); err != nil {
		return err
	}
	next, err := l.prepareShared(shared, current)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(next, current) {
		return nil
	}
	if _, err = l.snapshot(current, "before applying cloud synchronization"); err != nil {
		return err
	}
	return l.writeState(next)
}

func (l *Store) prepareShared(shared SharedState, current State) (State, error) {
	shared = canonicalShared(shared)
	next := cloneState(current)
	settings, err := settingsWithLocalPaths(shared.Settings, current.Settings)
	if err != nil {
		return State{}, err
	}
	next.Settings = settings
	next.Skills, next.Sources, next.Presets = shared.Skills, shared.Sources, shared.Presets
	for i := range next.Sources {
		incoming := &next.Sources[i]
		for _, previous := range current.Sources {
			if previous.ID != incoming.ID || previous.Revision != incoming.Revision {
				continue
			}
			// Diagnostics belong to the device that performed the refresh.
			incoming.LastError, incoming.RefreshedAt = previous.LastError, previous.RefreshedAt
			incoming.Warnings = append([]string(nil), previous.Warnings...)
			for j := range incoming.Catalog {
				entry := &incoming.Catalog[j]
				for _, old := range previous.Catalog {
					if old.Subpath == entry.Subpath && old.ContentHash == entry.ContentHash && old.Removed == entry.Removed {
						entry.New, entry.SkipReason = old.New, old.SkipReason
						break
					}
				}
			}
			break
		}
	}
	for _, source := range current.Sources {
		if localSource(source.Repository) {
			if sourceIDIn(shared, source.ID) {
				return State{}, fmt.Errorf("synchronized source ID conflicts with a local source: %s", source.ID)
			}
			next.Sources = append(next.Sources, source)
		}
	}
	skillMap := map[string]string{}
	for _, old := range current.Skills {
		for i := range next.Skills {
			skill := &next.Skills[i]
			if skill.ID == old.ID || (sharedSkillIdentity(old) != "" && sharedSkillIdentity(old) == sharedSkillIdentity(*skill)) {
				skillMap[old.ID] = skill.ID
				if skill.ID == old.ID && skill.Source == "" && localSource(old.Source) {
					skill.Source, skill.Ref, skill.Subpath, skill.SourceID, skill.Revision, skill.UpstreamRemoved = old.Source, old.Ref, old.Subpath, old.SourceID, old.Revision, old.UpstreamRemoved
				}
				break
			}
		}
	}
	for i := range next.Targets {
		target := &next.Targets[i]
		members := []string{}
		for _, id := range target.EnabledSkillIDs {
			if mapped := skillMap[id]; mapped != "" {
				members = append(members, mapped)
			}
		}
		target.EnabledSkillIDs = unique(members)
		target.EnabledPresetIDs = slices.DeleteFunc(target.EnabledPresetIDs, func(id string) bool {
			return !slices.ContainsFunc(next.Presets, func(p Preset) bool { return p.ID == id })
		})
	}
	if err := l.validate(next); err != nil {
		return State{}, err
	}
	recomputePending(&next)
	encoded, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return State{}, err
	}
	if len(encoded)+1 > maxJSONBytes {
		return State{}, errors.New("技能与来源清单超过 32 MiB，请缩小来源扫描范围")
	}
	return next, nil
}
