package sources

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	skillstore "racomi/internal/skills"
)

// NormalizeSource validates a registration without requiring a network request.
// Tree URLs with ambiguous branch/path boundaries are resolved by Preview first.
func NormalizeSource(source skillstore.Source) (skillstore.Source, error) {
	spec, err := parseSource(source.Repository, source.Ref, source.Subpath)
	if err != nil {
		return source, err
	}
	if spec.treeTail != "" {
		return source, errors.New("请填写仓库地址，并在分支和扫描范围中分别指定 Git 引用与子目录")
	}
	source.Repository, source.Ref, source.Subpath = spec.source, spec.ref, spec.subpath
	source.Name = strings.TrimSpace(source.Name)
	if source.Name == "" {
		source.Name = strings.TrimSuffix(filepath.Base(spec.source), ".git")
		if spec.hosted {
			u, _ := url.Parse(spec.source)
			source.Name = u.Hostname()
		}
	}
	return source, nil
}

func (c *Client) DescribePreview(previewID string) (Preview, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.previews[previewID]
	if !ok {
		return Preview{}, errors.New("preview is no longer available; preview the source again")
	}
	preview := record.preview
	preview.Candidates = append([]CandidateInfo{}, preview.Candidates...)
	preview.Skipped = append([]SkippedSkill(nil), preview.Skipped...)
	preview.Warnings = append([]string(nil), preview.Warnings...)
	return preview, nil
}

// CatalogForPreview scans the complete source scope in an already extracted
// immutable commit, including when the original discovery selected one skill.
func (c *Client) CatalogForPreview(previewID string, source skillstore.Source) (skillstore.Source, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.previews[previewID]
	if !ok {
		return source, errors.New("preview is no longer available; refresh the source again")
	}
	var items []CandidateInfo
	var skipped []SkippedSkill
	var warnings []string
	var err error
	if record.preview.Hosted {
		items, err = hostedCandidates(record.root, source.Subpath, record.preview.Revision)
		skipped, warnings = []SkippedSkill{}, []string{}
	} else {
		items, skipped, warnings, err = scanSkills(record.root, source.Subpath, record.preview.Revision, record.issues, true)
	}
	if err != nil {
		return source, err
	}
	previous := make(map[string]skillstore.SourceSkill)
	for _, item := range source.Catalog {
		previous[item.Subpath] = item
	}
	catalog := make([]skillstore.SourceSkill, 0, len(items))
	for _, item := range items {
		old, exists := previous[item.Subpath]
		catalog = append(catalog, skillstore.SourceSkill{
			Name: item.Name, Description: item.Description, Subpath: item.Subpath,
			Revision: item.Revision, ContentHash: item.ContentHash,
			New: !exists || old.Removed || old.New || old.ContentHash == "",
		})
		delete(previous, item.Subpath)
	}
	for _, item := range skipped {
		old, exists := previous[item.Subpath]
		if !exists {
			// Invalid metadata cannot provide a trusted, portable skill name.
			old = skillstore.SourceSkill{Name: "unreadable-skill", Subpath: item.Subpath}
		}
		old.Removed, old.New, old.SkipReason = false, false, item.Reason
		catalog = append(catalog, old)
		delete(previous, item.Subpath)
	}
	for _, old := range previous {
		old.SkipReason = ""
		// A link replacing a directory hides its descendants from discovery.
		// Preserve known skills below it, rather than interpreting them as deleted.
		for _, issue := range record.issues {
			if repositoryIssueAffectsSkill(issue, old.Subpath) {
				old.SkipReason = fmt.Sprintf("unsupported skill content at %q: %s", issue.Path, issue.Reason)
				break
			}
		}
		// A failed subtree walk can also hide previously known descendants.
		if old.SkipReason == "" {
			for _, item := range skipped {
				if item.blockedSubtree && repositoryPathsOverlap(old.Subpath, item.Subpath) {
					old.SkipReason = item.Reason
					break
				}
			}
		}
		old.Removed, old.New = old.SkipReason == "", false
		if old.Removed && old.ContentHash == "" {
			// A never-readable placeholder has no trusted version to retain.
			continue
		}
		catalog = append(catalog, old)
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].Subpath < catalog[j].Subpath })
	// Keep the registered identity, including legacy noncanonical addresses.
	// Git transport normalization must not change skill/source associations.
	source.Revision = record.preview.Revision
	// Registrations with an unspecified ref adopt the actual default branch on
	// first successful refresh; subsequent refreshes follow that branch.
	if source.Ref == "" {
		source.Ref = record.preview.Ref
	}
	source.Catalog, source.LastError = catalog, ""
	source.Warnings = warnings
	source.RefreshedAt = time.Now().UTC().Format(time.RFC3339Nano)
	record.preview.Candidates = items
	record.preview.Skipped, record.preview.Warnings = skipped, warnings
	c.previews[previewID] = record
	return source, nil
}

func repositoryPathsOverlap(a, b string) bool {
	return repositoryPathWithin(a, b) || repositoryPathWithin(b, a)
}

func (c *Client) RefreshSource(ctx context.Context, source skillstore.Source) (skillstore.Source, string, error) {
	return c.refreshSource(ctx, source, true)
}

// RefreshCachedSource scans another ref/scope after the same repository was
// successfully refreshed during the current batch.
func (c *Client) RefreshCachedSource(ctx context.Context, source skillstore.Source) (skillstore.Source, string, error) {
	return c.refreshSource(ctx, source, false)
}

func (c *Client) refreshSource(ctx context.Context, source skillstore.Source, refresh bool) (skillstore.Source, string, error) {
	spec, err := parseSource(source.Repository, source.Ref, source.Subpath)
	if err != nil {
		return source, "", err
	}
	c.mu.Lock()
	preview, err := c.previewOptionsLocked(ctx, spec, refresh, true)
	c.mu.Unlock()
	if err != nil {
		return source, "", err
	}
	updated, err := c.CatalogForPreview(preview.ID, source)
	if err != nil {
		_ = c.Release(preview.ID)
		return source, "", err
	}
	return updated, preview.ID, nil
}

// PrepareSourceSync extracts the catalog's exact revision. It never fetches a
// newer head between reviewing the plan and installing it, and works after a
// restart whenever the source's revision remains in the local Git cache.
func (c *Client) PrepareSourceSync(ctx context.Context, source skillstore.Source, paths []string) ([]skillstore.Candidate, string, error) {
	if !validRevision(source.Revision) {
		return nil, "", errors.New("请先成功刷新来源，再同步技能")
	}
	if len(paths) == 0 {
		return []skillstore.Candidate{}, "", nil
	}
	priorityPaths := make([]string, len(paths))
	for i, selected := range paths {
		cleaned, err := cleanSubpath(selected)
		if err != nil {
			return nil, "", err
		}
		priorityPaths[i] = cleaned
		for _, item := range source.Catalog {
			if item.Subpath == cleaned && item.SkipReason != "" {
				return nil, "", fmt.Errorf("skill %q was skipped during refresh: %s", cleaned, item.SkipReason)
			}
		}
	}
	spec, err := parseSource(source.Repository, source.Ref, source.Subpath)
	if err != nil {
		return nil, "", err
	}
	c.mu.Lock()
	var preview Preview
	if spec.hosted {
		preview, err = c.prepareHostedSyncLocked(ctx, source)
	} else {
		spec.ref = source.Revision
		preview, err = c.previewOptionsLocked(ctx, spec, false, true, priorityPaths...)
	}
	c.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	candidates, err := c.Candidates(preview.ID, paths)
	if err != nil {
		_ = c.Release(preview.ID)
		return nil, "", err
	}
	for i := range candidates {
		candidates[i].Skill.SourceID = source.ID
		candidates[i].Skill.Source = source.Repository
		candidates[i].Skill.Ref = source.Ref
	}
	return candidates, preview.ID, nil
}

func (c *Client) PreviewAdvances(ctx context.Context, previewID, oldRevision string) bool {
	if !validRevision(oldRevision) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.previews[previewID]
	if !ok {
		return false
	}
	if record.preview.Hosted {
		spec := sourceSpec{source: record.preview.Source, subpath: record.preview.Subpath}
		latest, err := os.ReadFile(filepath.Join(c.hostedSnapshotBase(spec), "latest"))
		return err == nil && string(latest) == record.preview.Revision
	}
	repo, err := c.repository(ctx, record.preview.Source, false)
	if err != nil {
		return false
	}
	_, err = gitCommand(ctx, repo, "merge-base", "--is-ancestor", oldRevision, record.preview.Revision)
	return err == nil
}

func (c *Client) UpdateSummary(ctx context.Context, source skillstore.Source, skill skillstore.Skill) string {
	if !validRevision(skill.Revision) || !validRevision(source.Revision) {
		return ""
	}
	spec, err := parseSource(source.Repository, source.Ref, skill.Subpath)
	if err != nil {
		return ""
	}
	if spec.hosted {
		if skill.Revision != source.Revision {
			return "Skill files changed."
		}
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	repo, err := c.repository(ctx, spec.source, false)
	if err != nil {
		return ""
	}
	summary, err := gitCommand(ctx, repo, "log", "--format=%h %s", "-5", skill.Revision+".."+source.Revision, "--", spec.subpath)
	if err != nil {
		return "来源历史已变化，请在来源查看当前版本。"
	}
	return strings.TrimSpace(summary)
}
