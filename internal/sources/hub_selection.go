package sources

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	skillstore "racomi/internal/skills"
)

// Follow the skills CLI's common search locations for skills.sh name selectors.
// See https://github.com/vercel-labs/skills/blob/main/src/skills.ts.
// A repository can publish different builds of one skill for several agents;
// recursive discovery also finds fixtures that are not the indexed skill.
// All previews group conventional distribution copies and retain different
// contents as selectable variants of the preferred build.
var hubSkillDirectories = []string{
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

func hubSkillPriority(scope, skillPath string) int {
	relative := skillPath
	if skillPath == scope {
		return 0
	}
	if scope != "." {
		relative = strings.TrimPrefix(skillPath, scope+"/")
	}
	if path.Dir(relative) == "." {
		return 1
	}
	best, longest := len(hubSkillDirectories)+2, 0
	for i, directory := range hubSkillDirectories {
		if child, ok := strings.CutPrefix(relative, directory+"/"); ok && strings.Count(child, "/") < 3 {
			if len(directory) > longest {
				best, longest = i+2, len(directory)
			}
		}
	}
	return best
}

// A name selector cannot ask the user to distinguish multiple physical paths
// in one distribution tier. Reject the complete ambiguous family, even when
// the preferred tier itself has only one build.
func validateHubDistribution(roots map[string]*skillExtraction, scope, selected string) error {
	items := make([]CandidateInfo, 0, len(roots))
	for subpath, root := range roots {
		if !repositoryPathWithin(scope, subpath) {
			continue
		}
		name := root.name
		if name == "" {
			name = path.Base(subpath)
		}
		items = append(items, CandidateInfo{Name: name, Subpath: subpath})
	}
	keys := distributionCandidateKeys(items)
	for _, item := range items {
		if !roots[item.Subpath].priority {
			continue
		}
		key := skillstore.DistributionKey(item.Name, item.Subpath)
		if keys[item.Subpath] == key {
			continue
		}
		paths := make([]string, 0)
		for _, candidate := range items {
			if skillstore.DistributionKey(candidate.Name, candidate.Subpath) == key {
				paths = append(paths, candidate.Subpath)
			}
		}
		return ambiguousHubSkillError(selected, paths)
	}
	return nil
}

func prioritizeHubSkillRoots(roots map[string]*skillExtraction, scope string) {
	fallback := len(hubSkillDirectories) + 2
	hasStandardSkills := false
	for rootPath, root := range roots {
		if root.validMetadata && hubSkillPriority(scope, rootPath) < fallback {
			hasStandardSkills = true
			break
		}
	}
	// Keep all matches in the best location tier so genuine ambiguity is
	// still rejected, including when extraction budgets omit one of them.
	best := fallback
	for rootPath, root := range roots {
		// An identified preferred build keeps its identity when its metadata
		// is invalid; a generated copy must not silently replace that build.
		if root.priority {
			best = min(best, hubSkillPriority(scope, rootPath))
		}
	}
	for rootPath, root := range roots {
		priority := hubSkillPriority(scope, rootPath)
		// Like the skills CLI, recurse outside standard locations only when
		// no standard skills exist. Otherwise stale Hub entries can select a
		// test fixture or example bearing the removed skill's name.
		if root.priority && (priority > best || hasStandardSkills && priority == fallback) {
			root.priority = false
		}
	}
}

func prioritizeHubRepositoryRoots(roots map[string]*skillExtraction, scope string) {
	fallback := len(hubSkillDirectories) + 2
	hasStandard := false
	for rootPath, root := range roots {
		if !root.priority {
			continue
		}
		priority := hubSkillPriority(scope, rootPath)
		hasStandard = hasStandard || root.validMetadata && priority < fallback
	}
	for rootPath, root := range roots {
		priority := hubSkillPriority(scope, rootPath)
		// Reserve the preview budget before extracting generated copies or
		// fixtures; filtering afterward cannot restore a skipped source build.
		root.priority = root.priority && (!hasStandard || priority < fallback)
	}
	prioritizeDistributionRoots(roots)
}

func missingHubSkillError(skill string) error {
	return fmt.Errorf("skills.sh skill %q was not found in the current repository revision; the index may be outdated or the skill may have been renamed or removed; preview the repository to see available skills", skill)
}

func ambiguousHubSkillError(skill string, paths []string) error {
	quoted := make([]string, len(paths))
	for i, rootPath := range paths {
		quoted[i] = strconv.Quote(rootPath)
	}
	sort.Strings(quoted)
	return fmt.Errorf("skills.sh skill %q matches multiple repository skills: %s; specify a skill subdirectory", skill, strings.Join(quoted, ", "))
}

func unavailableHubSkillError(skill, reason string) error {
	return fmt.Errorf("skills.sh skill %q is unavailable: %s", skill, reason)
}

func blockedHubSkillError(skill string, paths []string, issues []repositoryIssue) error {
	var first *repositoryIssue
	for i, issue := range issues {
		// Allocation adds a generic marker to retain blocked skill identity.
		// Report the original offending entry or metadata reason instead.
		if issue.SkillOnly && issue.Reason == "unsupported repository file type" {
			continue
		}
		for _, rootPath := range paths {
			if repositoryIssueAffectsSkill(issue, rootPath) && (first == nil || issue.Path < first.Path || issue.Path == first.Path && issue.Reason < first.Reason) {
				first = &issues[i]
			}
		}
	}
	reason := "the selected skill could not be read"
	if first != nil {
		reason = fmt.Sprintf("unsupported skill content at %q: %s", first.Path, first.Reason)
	}
	return unavailableHubSkillError(skill, reason)
}
