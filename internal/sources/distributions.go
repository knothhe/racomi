package sources

import (
	"sort"

	skillstore "racomi/internal/skills"
)

func distributionCandidateKeys(items []CandidateInfo, skipped ...SkippedSkill) map[string]string {
	catalog := make([]skillstore.SourceSkill, 0, len(items))
	for _, item := range items {
		catalog = append(catalog, skillstore.SourceSkill{Name: item.Name, Subpath: item.Subpath})
	}
	for _, item := range skipped {
		if item.name != "" {
			catalog = append(catalog, skillstore.SourceSkill{Name: item.name, Subpath: item.Subpath})
		}
	}
	return skillstore.DistributionCatalogKeys(catalog)
}

// Reserve extraction capacity for the preferred copy of every logical skill
// before spending it on alternative builds of those skills.
func prioritizeDistributionRoots(roots map[string]*skillExtraction) {
	items := make([]CandidateInfo, 0, len(roots))
	for subpath, root := range roots {
		if root.priority {
			items = append(items, CandidateInfo{Name: root.name, Subpath: subpath})
		}
	}
	keys := distributionCandidateKeys(items)
	best := make(map[string]int)
	for _, item := range items {
		key, priority := keys[item.Subpath], skillstore.DistributionPriority(item.Subpath)
		if previous, exists := best[key]; !exists || priority < previous {
			best[key] = priority
		}
	}
	for subpath, root := range roots {
		if root.priority {
			root.priority = skillstore.DistributionPriority(subpath) == best[keys[subpath]]
		}
	}
}

// Git and Hub previews use the same logical identities. Physical paths and hashes
// remain attached to each choice, while byte-identical copies add no choice.
func groupDistributionCandidates(items []CandidateInfo, skipped ...SkippedSkill) []CandidateInfo {
	items = flattenCandidates(items)
	keys := distributionCandidateKeys(items, skipped...)
	sort.SliceStable(items, func(i, j int) bool {
		left, right := skillstore.DistributionPriority(items[i].Subpath), skillstore.DistributionPriority(items[j].Subpath)
		if left != right {
			return left < right
		}
		return items[i].Subpath < items[j].Subpath
	})
	result := make([]CandidateInfo, 0, len(items))
	groups := make(map[string]int)
	hashes := make(map[string]map[string]bool)
	for _, item := range items {
		key := keys[item.Subpath]
		if index, exists := groups[key]; exists {
			if item.ContentHash == "" || !hashes[key][item.ContentHash] {
				result[index].Variants = append(result[index].Variants, item)
			}
		} else {
			groups[key] = len(result)
			result = append(result, item)
			hashes[key] = make(map[string]bool)
		}
		hashes[key][item.ContentHash] = true
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Subpath < result[j].Subpath })
	return result
}

func flattenCandidates(items []CandidateInfo) []CandidateInfo {
	result := make([]CandidateInfo, 0, len(items))
	for _, item := range items {
		variants := item.Variants
		item.Variants = nil
		result = append(result, item)
		result = append(result, flattenCandidates(variants)...)
	}
	return result
}

func cloneCandidates(items []CandidateInfo) []CandidateInfo {
	result := append([]CandidateInfo{}, items...)
	for i := range result {
		if len(result[i].Variants) > 0 {
			result[i].Variants = cloneCandidates(result[i].Variants)
		}
	}
	return result
}

func findPreviewCandidate(items []CandidateInfo, subpath string) (*CandidateInfo, string) {
	for i := range items {
		if items[i].Subpath == subpath {
			return &items[i], items[i].Subpath
		}
		for j := range items[i].Variants {
			if items[i].Variants[j].Subpath == subpath {
				return &items[i].Variants[j], items[i].Subpath
			}
		}
	}
	return nil, ""
}

func recordPhysicalCandidates(record previewRecord) []CandidateInfo {
	if record.candidates != nil {
		return record.candidates
	}
	return flattenCandidates(record.preview.Candidates)
}

func selectDistributionCandidates(items []CandidateInfo, selectedPath string, skipped ...SkippedSkill) []CandidateInfo {
	keys := distributionCandidateKeys(items, skipped...)
	key, found := keys[selectedPath]
	if !found {
		return nil
	}
	selected := make([]CandidateInfo, 0, 1)
	for _, item := range items {
		if keys[item.Subpath] == key {
			selected = append(selected, item)
		}
	}
	return selected
}
