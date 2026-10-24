package skills

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// Keep the public skill-distribution directories in preference order. The
// specialized skills directories are matched before their enclosing directory.
var distributionDirectories = []string{
	"skills", "skills/.curated", "skills/.experimental", "skills/.system",
	".agents/skills", ".claude/skills", ".cline/skills", ".codebuddy/skills",
	".codex/skills", ".commandcode/skills", ".continue/skills", ".factory/skills",
	".github/skills", ".goose/skills", ".grok/skills", ".iflow/skills",
	".junie/skills", ".kilo/skills", ".kilocode/skills", ".kimchi/skills",
	".kiro/skills", ".minimax/skills", ".mux/skills", ".neovate/skills",
	".opencode/skills", ".openhands/skills", ".pi/skills", ".posit/assistant/skills",
	".qoder/skills", ".roo/skills", ".trae/skills", ".windsurf/skills",
	".zcode/skills", ".zencoder/skills",
}

func distributionLocation(subpath string) (scope, parent, tier string, priority int, ok bool) {
	parts := strings.Split(subpath, "/")
	for start := range parts {
		best, bestLength := -1, 0
		for index, directory := range distributionDirectories {
			directoryParts := strings.Split(directory, "/")
			end := start + len(directoryParts)
			if end >= len(parts) || len(directoryParts) <= bestLength || strings.Join(parts[start:end], "/") != directory {
				continue
			}
			best, bestLength = index, len(directoryParts)
		}
		if best >= 0 {
			scope = strings.Join(parts[:start], "/")
			parent = path.Dir(strings.Join(parts[start+bestLength:], "/"))
			return scope, parent, distributionDirectories[best], best, true
		}
	}
	return "", "", "", len(distributionDirectories), false
}

// DistributionKey identifies conventional distribution copies within one
// repository and ref. It retains the repository scope and relative child parent
// but uses the declared skill name instead of the child directory basename.
// Nonstandard paths retain their physical identity. Callers must also check
// DistributionTier: two paths in the same tier make the whole family ambiguous.
func DistributionKey(name, subpath string) string {
	scope, parent, _, _, ok := distributionLocation(subpath)
	if !ok {
		return "path\x00" + subpath
	}
	return "distribution\x00" + scope + "\x00" + parent + "\x00" + name
}

// DistributionTier distinguishes the physical distribution directory, including
// its repository scope, so two different skills in one tier cannot be merged.
func DistributionTier(subpath string) string {
	scope, _, tier, _, ok := distributionLocation(subpath)
	if !ok {
		return "path\x00" + subpath
	}
	return scope + "\x00" + tier
}

// DistributionPriority chooses the preferred physical copy of a family.
func DistributionPriority(subpath string) int {
	_, _, _, priority, _ := distributionLocation(subpath)
	return priority
}

// DistributionCatalogKeys resolves families against a flat physical catalog.
// A family with two live paths in one tier remains separate at every path.
// Removed paths do not introduce new ambiguity into a current catalog.
func DistributionCatalogKeys(catalog []SourceSkill) map[string]string {
	paths := map[string]map[string]string{}
	ambiguous := map[string]bool{}
	for _, entry := range catalog {
		if entry.Removed {
			continue
		}
		key, tier := DistributionKey(entry.Name, entry.Subpath), DistributionTier(entry.Subpath)
		if paths[key] == nil {
			paths[key] = map[string]string{}
		}
		if previous, exists := paths[key][tier]; exists && previous != entry.Subpath {
			ambiguous[key] = true
		}
		paths[key][tier] = entry.Subpath
	}
	keys := make(map[string]string, len(catalog))
	for _, entry := range catalog {
		key := DistributionKey(entry.Name, entry.Subpath)
		if ambiguous[key] {
			key = "path\x00" + entry.Subpath
		}
		keys[entry.Subpath] = key
	}
	return keys
}

func distributionOrigin(skill Skill) string {
	return skill.Source + "\x00" + skill.Ref + "\x00" + skill.SourceID
}

// Include the complete source inventory when deciding whether a family is
// ambiguous, even if the caller selected only one of its physical paths.
func candidateDistributionKeys(state State, candidates []Candidate) map[string]map[string]string {
	catalogs := map[string][]SourceSkill{}
	for _, candidate := range candidates {
		if candidate.Skill.Source != "" {
			catalogs[distributionOrigin(candidate.Skill)] = nil
		}
	}
	for origin := range catalogs {
		for _, source := range state.Sources {
			if origin == distributionOrigin(Skill{Source: source.Repository, Ref: source.Ref, SourceID: source.ID}) {
				catalogs[origin] = append(catalogs[origin], source.Catalog...)
			}
		}
		known := map[string]bool{}
		for _, entry := range catalogs[origin] {
			known[entry.Subpath] = true
		}
		for _, skill := range state.Skills {
			if distributionOrigin(skill) == origin && !known[skill.Subpath] {
				catalogs[origin] = append(catalogs[origin], SourceSkill{Name: skill.Name, Subpath: skill.Subpath, Removed: skill.UpstreamRemoved})
				known[skill.Subpath] = true
			}
		}
	}
	for _, candidate := range candidates {
		if candidate.Skill.Source != "" {
			origin := distributionOrigin(candidate.Skill)
			catalogs[origin] = append(catalogs[origin], SourceSkill{Name: candidate.Skill.Name, Subpath: candidate.Skill.Subpath})
		}
	}
	keys := make(map[string]map[string]string, len(catalogs))
	for origin, catalog := range catalogs {
		keys[origin] = DistributionCatalogKeys(catalog)
	}
	return keys
}

func installedDistributionMatch(state State, skill Skill, keys map[string]string) (int, error) {
	for index, existing := range state.Skills {
		if distributionOrigin(existing) == distributionOrigin(skill) && existing.Subpath == skill.Subpath {
			return index, nil
		}
	}
	matched := -1
	for index, existing := range state.Skills {
		if distributionOrigin(existing) != distributionOrigin(skill) {
			continue
		}
		if keys[existing.Subpath] != keys[skill.Subpath] {
			continue
		}
		if matched >= 0 {
			return -1, fmt.Errorf("multiple installed variants match skill %q; select an installed path or relink a specific skill", skill.Name)
		}
		matched = index
	}
	return matched, nil
}

// Coalesce only when an explicit import/synchronization selects a family.
// The normal snapshot transaction retains all old records and content objects.
func coalesceInstalledDistribution(state *State, skill Skill, keys map[string]string) string {
	key := keys[skill.Subpath]
	if !strings.HasPrefix(key, "distribution\x00") {
		return ""
	}
	matches := []Skill{}
	for _, existing := range state.Skills {
		if distributionOrigin(existing) == distributionOrigin(skill) && keys[existing.Subpath] == key {
			matches = append(matches, existing)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	// Removed inventory no longer contributes to current-catalog ambiguity,
	// but two installed historical paths from one tier are still distinct.
	tiers := map[string]string{}
	for _, existing := range matches {
		tier := DistributionTier(existing.Subpath)
		if previous, exists := tiers[tier]; exists && previous != existing.Subpath {
			return ""
		}
		tiers[tier] = existing.Subpath
	}
	keeper := matches[0]
	for _, existing := range matches {
		if existing.Subpath == skill.Subpath {
			keeper = existing
			break
		}
		if keeper.InstalledAt == "" && existing.InstalledAt != "" || existing.InstalledAt != "" && existing.InstalledAt < keeper.InstalledAt || existing.InstalledAt == keeper.InstalledAt && existing.ID < keeper.ID {
			keeper = existing
		}
	}
	aliases := map[string]bool{}
	for _, existing := range matches {
		if existing.ID != keeper.ID {
			aliases[existing.ID] = true
		}
	}
	if len(aliases) == 0 {
		return keeper.ID
	}
	remap := func(ids []string) []string {
		for index, id := range ids {
			if aliases[id] {
				ids[index] = keeper.ID
			}
		}
		return unique(ids)
	}
	for index := range state.Presets {
		state.Presets[index].SkillIDs = remap(state.Presets[index].SkillIDs)
	}
	for index := range state.Targets {
		previous := state.Targets[index]
		previous.EnabledSkillIDs = append([]string{}, previous.EnabledSkillIDs...)
		target := &state.Targets[index]
		target.EnabledSkillIDs = remap(target.EnabledSkillIDs)
		occupied := map[string]bool{}
		for _, deployment := range target.Deployments {
			if deployment.SkillID == keeper.ID {
				occupied[deploymentPath(*target, deployment)] = true
			}
		}
		for deployment := range target.Deployments {
			if aliases[target.Deployments[deployment].SkillID] {
				path := deploymentPath(*target, target.Deployments[deployment])
				if !occupied[path] {
					target.Deployments[deployment].SkillID = keeper.ID
					occupied[path] = true
				}
				// Old names can coexist in one directory after metadata changes.
				// Keep an obsolete deployment's original ownership ID until Apply
				// removes it rather than discarding ownership of its directory.
			}
		}
		stampTarget(target, &previous)
	}
	state.Skills = slices.DeleteFunc(state.Skills, func(existing Skill) bool { return aliases[existing.ID] })
	return keeper.ID
}
