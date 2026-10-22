package sources

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// PreviewHubRepository lists the available skills behind a Git-backed skills.sh
// link when its indexed selector no longer exists. The original link supplies
// the repository identity; arbitrary Git and hosted sources cannot enter this
// recovery flow.
func (c *Client) PreviewHubRepository(ctx context.Context, input, ref, subpath string) (Preview, error) {
	invalidLink := errors.New("enter a Git-backed skills.sh skill link")
	if strings.ContainsAny(input, "\\%?#") || strings.IndexFunc(input, unicode.IsSpace) >= 0 {
		return Preview{}, invalidLink
	}
	if strings.HasPrefix(input, "skills.sh/") {
		input = "https://" + input
	}
	link, err := url.Parse(input)
	if err != nil || link.Scheme != "https" || link.User != nil || link.Host != link.Hostname() || (link.Hostname() != "skills.sh" && link.Hostname() != "www.skills.sh") {
		return Preview{}, invalidLink
	}
	parts := strings.Split(strings.TrimPrefix(link.Path, "/"), "/")
	if len(parts) != 3 || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) || !safeSkillName.MatchString(parts[2]) {
		return Preview{}, invalidLink
	}
	spec, err := parseSource(input, ref, subpath)
	if err != nil {
		return Preview{}, err
	}
	if spec.skill == "" || spec.hosted {
		return Preview{}, invalidLink
	}
	spec.skill = ""
	spec.hubRepository = true
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, SourceOperationTimeout)
	defer cancel()
	preview, err := c.previewLocked(ctx, spec, true)
	if err != nil {
		return Preview{}, err
	}
	preview = filterHubRepositoryPreview(preview)
	// Candidates checks this stored allowlist, so hidden generated copies and
	// fixtures cannot be installed by submitting their paths directly.
	record := c.previews[preview.ID]
	record.preview = preview
	c.previews[preview.ID] = record
	return preview, nil
}

func filterHubRepositoryPreview(preview Preview) Preview {
	fallback := len(hubSkillDirectories) + 2
	best := make(map[string]int)
	hasStandard := false
	for _, candidate := range preview.Candidates {
		priority := hubSkillPriority(preview.Subpath, candidate.Subpath)
		if previous, ok := best[candidate.Name]; !ok || priority < previous {
			best[candidate.Name] = priority
		}
		hasStandard = hasStandard || priority < fallback
	}
	candidates := make([]CandidateInfo, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		priority := hubSkillPriority(preview.Subpath, candidate.Subpath)
		if priority == best[candidate.Name] && (!hasStandard || priority < fallback) {
			// Equal-priority paths remain separate choices for explicit review.
			candidates = append(candidates, candidate)
		}
	}
	preview.Candidates = candidates
	if hasStandard {
		skipped := make([]SkippedSkill, 0, len(preview.Skipped))
		for _, candidate := range preview.Skipped {
			if hubSkillPriority(preview.Subpath, candidate.Subpath) < fallback {
				skipped = append(skipped, candidate)
			}
		}
		preview.Skipped = skipped
	}
	return preview
}
