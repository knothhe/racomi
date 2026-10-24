package sources

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const hubAvailabilityTTL = 15 * time.Minute
const hubAvailabilityBudget = 15 * time.Second
const maxHubAvailabilityCacheBytes = 4 << 20
const maxHubAvailabilityCacheTotal = 64 << 20
const maxHubAvailabilityCacheFiles = 256

type hubAvailabilityRoot struct {
	Path            string `json:"path"`
	Name            string `json:"name,omitempty"`
	ValidMetadata   bool   `json:"validMetadata"`
	MetadataBlocked bool   `json:"metadataBlocked"`
	Blocked         bool   `json:"blocked"`
	Reason          string `json:"reason,omitempty"`
}

// Store discovery facts rather than requested selectors: one checked revision
// can answer subsequent leaderboard pages and searches without more downloads.
type hubAvailabilitySnapshot struct {
	Version   int                   `json:"version"`
	Source    string                `json:"source"`
	Revision  string                `json:"revision"`
	CheckedAt time.Time             `json:"checkedAt"`
	Roots     []hubAvailabilityRoot `json:"roots"`
}

type hubAvailabilityFetch func(context.Context, string) (string, error)

// Invalid descriptions must not erase an otherwise readable skill identity.
// Selector preview and availability both retain this identity while strict
// metadata validation continues to block the invalid content.
func hubAvailabilityMetadataName(body []byte) string {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	lines := bytes.Split(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")), []byte("\n"))
	if len(lines) == 0 || string(lines[0]) != "---" {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		if string(lines[i]) != "---" && string(lines[i]) != "..." {
			continue
		}
		document, err := parseFrontmatter(bytes.Join(lines[1:i], []byte("\n")))
		if err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
			return ""
		}
		name, found := "", false
		fields := document.Content[0].Content
		for j := 0; j < len(fields); j += 2 {
			if fields[j].Value != "name" {
				continue
			}
			value := fields[j+1]
			if found || value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return ""
			}
			name, found = strings.TrimSpace(value.Value), true
		}
		if !safeSkillName.MatchString(name) {
			return ""
		}
		return name
	}
	return ""
}

// CheckHubAvailability inspects Git tree entries and SKILL.md metadata, without
// extracting assets or creating installable previews. Operational failures are
// unknown, never evidence that an indexed skill was removed.
func (c *Client) CheckHubAvailability(ctx context.Context, inputs []string) ([]HubAvailability, error) {
	return c.checkHubAvailability(ctx, inputs, time.Now, func(ctx context.Context, source string) (string, error) {
		return c.repository(ctx, source, true, "")
	})
}

func (c *Client) checkHubAvailability(ctx context.Context, inputs []string, now func() time.Time, fetch hubAvailabilityFetch) ([]HubAvailability, error) {
	if len(inputs) > 200 {
		return nil, errors.New("check at most 200 skills.sh links at a time")
	}
	type request struct {
		index int
		skill string
	}
	groups := make(map[string][]request)
	order := make([]string, 0)
	results := make([]HubAvailability, len(inputs))
	for i, input := range inputs {
		results[i] = HubAvailability{URL: input, State: "unknown", CheckedAt: now().UTC().Format(time.RFC3339Nano)}
		spec, err := parseHubAvailabilitySource(input)
		if err != nil {
			results[i].Reason = err.Error()
			continue
		}
		if _, exists := groups[spec.source]; !exists {
			order = append(order, spec.source)
		}
		groups[spec.source] = append(groups[spec.source], request{index: i, skill: spec.skill})
	}
	for _, source := range order {
		// Each repository releases the shared cache lock before the next one,
		// allowing a foreground preview to interrupt a background batch.
		repositoryCtx, cancel := context.WithTimeout(ctx, hubAvailabilityBudget)
		snapshot, err := c.hubAvailabilityRepository(repositoryCtx, source, now, fetch)
		for _, item := range groups[source] {
			result := &results[item.index]
			if repositoryCtx.Err() != nil {
				err = repositoryCtx.Err()
			}
			if err != nil {
				result.Reason = err.Error()
				continue
			}
			result.State, result.Reason = snapshot.availability(item.skill)
			if repositoryCtx.Err() != nil {
				result.State, result.Reason = "unknown", repositoryCtx.Err().Error()
				continue
			}
			result.Revision = snapshot.Revision
			result.CheckedAt = snapshot.CheckedAt.UTC().Format(time.RFC3339Nano)
		}
		cancel()
	}
	return results, nil
}

func parseHubAvailabilitySource(input string) (sourceSpec, error) {
	invalid := errors.New("enter a Git-backed skills.sh skill link")
	if len(input) > 2048 || strings.ContainsAny(input, "\\%?#") || strings.IndexFunc(input, unicode.IsSpace) >= 0 {
		return sourceSpec{}, invalid
	}
	if strings.HasPrefix(input, "skills.sh/") {
		input = "https://" + input
	}
	link, err := url.Parse(input)
	if err != nil || link.Scheme != "https" || link.User != nil || link.Host != link.Hostname() || (link.Hostname() != "skills.sh" && link.Hostname() != "www.skills.sh") {
		return sourceSpec{}, invalid
	}
	parts := strings.Split(strings.TrimPrefix(link.Path, "/"), "/")
	if len(parts) != 3 || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) || !safeSkillName.MatchString(parts[2]) {
		return sourceSpec{}, invalid
	}
	spec, err := parseSource(input, "", "")
	if err != nil {
		return sourceSpec{}, err
	}
	if spec.skill == "" || spec.hosted {
		return sourceSpec{}, invalid
	}
	return spec, nil
}

func (c *Client) hubAvailabilityRepository(parent context.Context, source string, now func() time.Time, fetch hubAvailabilityFetch) (hubAvailabilitySnapshot, error) {
	ctx, cancel := context.WithTimeout(parent, hubAvailabilityBudget)
	defer cancel()
	// Mutex.Lock cannot observe cancellation while a foreground operation owns
	// the repository cache. Never queue a canceled background scan behind it.
	for !c.mu.TryLock() {
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return hubAvailabilitySnapshot{}, ctx.Err()
		case <-timer.C:
		}
	}
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return hubAvailabilitySnapshot{}, err
	}
	if snapshot, ok := c.cachedHubAvailability(ctx, source, now()); ok {
		return snapshot, nil
	}
	repo, err := fetch(ctx, source)
	if err != nil {
		return hubAvailabilitySnapshot{}, err
	}
	revision, err := resolveRevision(ctx, repo, "HEAD")
	if err != nil {
		return hubAvailabilitySnapshot{}, err
	}
	entries, issues, err := inspectCommit(ctx, repo, revision)
	if err != nil {
		return hubAvailabilitySnapshot{}, err
	}
	roots, issues := discoverSkillRoots(entries, issues, ".")
	metadataIssues, err := inspectSkillMetadata(ctx, repo, roots, "")
	if err != nil {
		return hubAvailabilitySnapshot{}, err
	}
	issues = append(issues, metadataIssues...)
	metadataBlocked := make(map[string]bool, len(roots))
	for rootPath, root := range roots {
		metadataBlocked[rootPath] = root.blocked
	}
	// Reuse the preview's unsafe-file and budget accounting without archiving
	// or reading asset blobs. Cross-skill budget contention is not intrinsic to
	// one skill, since a foreground selector reserves its own budget first.
	_, issues = allocateSkillExtraction(entries, issues, roots)
	snapshot := hubAvailabilitySnapshot{Version: 1, Source: source, Revision: revision, CheckedAt: now().UTC(), Roots: make([]hubAvailabilityRoot, 0, len(roots))}
	for rootPath, root := range roots {
		if err := ctx.Err(); err != nil {
			return hubAvailabilitySnapshot{}, err
		}
		reason := ""
		for _, issue := range issues {
			if issue.SkillOnly && (issue.Reason == "unsupported repository file type" || strings.HasPrefix(issue.Reason, "skill was skipped because the preview reached its ")) {
				continue
			}
			if repositoryIssueAffectsSkill(issue, rootPath) {
				reason = fmt.Sprintf("unsupported skill content at %q: %s", issue.Path, issue.Reason)
				break
			}
		}
		if reason == "" && root.bytes > maxRepositoryBytes {
			reason = "skill content exceeds the 100 MiB preview limit"
		}
		if reason == "" && root.count > maxRepositoryEntries {
			reason = "skill has too many files (maximum 20,000)"
		}
		ancestors := 0
		if rootPath != "." {
			ancestors = strings.Count(rootPath, "/") + 1
		}
		if reason == "" && root.count+ancestors >= maxRepositoryEntries {
			reason = "skill was skipped because the preview reached its 20,000 file limit"
		}
		snapshot.Roots = append(snapshot.Roots, hubAvailabilityRoot{Path: rootPath, Name: root.name, ValidMetadata: root.validMetadata, MetadataBlocked: metadataBlocked[rootPath], Blocked: root.blocked, Reason: reason})
	}
	sort.Slice(snapshot.Roots, func(i, j int) bool { return snapshot.Roots[i].Path < snapshot.Roots[j].Path })
	if err := ctx.Err(); err != nil {
		return hubAvailabilitySnapshot{}, err
	}
	c.saveHubAvailability(snapshot)
	return snapshot, nil
}

func (snapshot hubAvailabilitySnapshot) availability(skill string) (string, string) {
	fallback := len(hubSkillDirectories) + 2
	hasStandard := false
	best := fallback
	for _, root := range snapshot.Roots {
		priority := hubSkillPriority(".", root.Path)
		hasStandard = hasStandard || root.ValidMetadata && priority < fallback
		if root.Name == skill || path.Base(root.Path) == skill {
			best = min(best, priority)
		}
	}
	matches, blocked := make([]hubAvailabilityRoot, 0), make([]hubAvailabilityRoot, 0)
	distributionRoots := make(map[string]*skillExtraction, len(snapshot.Roots))
	for _, root := range snapshot.Roots {
		priority := hubSkillPriority(".", root.Path)
		selected := (root.Name == skill || path.Base(root.Path) == skill) && priority <= best && (!hasStandard || priority != fallback)
		distributionRoots[root.Path] = &skillExtraction{name: root.Name, priority: selected}
		if !selected {
			continue
		}
		if root.Blocked {
			blocked = append(blocked, root)
		} else {
			matches = append(matches, root)
		}
	}
	if err := validateHubDistribution(distributionRoots, ".", skill); err != nil {
		return "unavailable", err.Error()
	}
	if len(matches)+len(blocked) > 1 {
		paths := make([]string, 0, len(matches)+len(blocked))
		for _, root := range matches {
			paths = append(paths, root.Path)
		}
		for _, root := range blocked {
			paths = append(paths, root.Path)
		}
		return "unavailable", ambiguousHubSkillError(skill, paths).Error()
	}
	if len(matches) == 0 {
		if len(blocked) > 0 {
			return "unavailable", unavailableHubSkillError(skill, blocked[0].Reason).Error()
		}
		return "missing", missingHubSkillError(skill).Error()
	}
	if matches[0].Reason != "" {
		return "unavailable", unavailableHubSkillError(skill, matches[0].Reason).Error()
	}
	return "available", ""
}

func (c *Client) hubAvailabilityPath(source string) string {
	hash := sha256.Sum256([]byte(source))
	return filepath.Join(c.cacheDir, "hub-availability", hex.EncodeToString(hash[:])+".json")
}

func (c *Client) cachedHubAvailability(ctx context.Context, source string, now time.Time) (hubAvailabilitySnapshot, bool) {
	file, err := os.Open(c.hubAvailabilityPath(source))
	if err != nil {
		return hubAvailabilitySnapshot{}, false
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxHubAvailabilityCacheBytes+1))
	if err != nil || len(body) > maxHubAvailabilityCacheBytes {
		return hubAvailabilitySnapshot{}, false
	}
	var snapshot hubAvailabilitySnapshot
	if json.Unmarshal(body, &snapshot) != nil || snapshot.Version != 1 || snapshot.Source != source || !validRevision(snapshot.Revision) || snapshot.CheckedAt.After(now) || now.Sub(snapshot.CheckedAt) >= hubAvailabilityTTL || len(snapshot.Roots) > maxTreeEntries {
		return hubAvailabilitySnapshot{}, false
	}
	for _, root := range snapshot.Roots {
		cleaned, err := cleanSubpath(root.Path)
		if err != nil || cleaned != root.Path || root.Name != "" && !safeSkillName.MatchString(root.Name) {
			return hubAvailabilitySnapshot{}, false
		}
	}
	// Foreground refreshes may have advanced HEAD before this TTL expires.
	// Existing local knowledge always supersedes a persisted availability view.
	hash := sha256.Sum256([]byte(source))
	repo := filepath.Join(c.cacheDir, "repositories", hex.EncodeToString(hash[:]))
	if _, err := os.Stat(filepath.Join(repo, "HEAD")); err == nil {
		current, err := resolveRevision(ctx, repo, "HEAD")
		if err != nil || current != snapshot.Revision {
			return hubAvailabilitySnapshot{}, false
		}
	} else if !os.IsNotExist(err) {
		return hubAvailabilitySnapshot{}, false
	}
	return snapshot, ctx.Err() == nil
}

func (c *Client) saveHubAvailability(snapshot hubAvailabilitySnapshot) {
	body, err := json.Marshal(snapshot)
	if err != nil || len(body) > maxHubAvailabilityCacheBytes {
		return
	}
	target := c.hubAvailabilityPath(snapshot.Source)
	directory := filepath.Dir(target)
	if os.MkdirAll(directory, 0700) != nil {
		return
	}
	file, err := os.CreateTemp(directory, ".snapshot-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || os.Rename(file.Name(), target) != nil {
		return
	}
	// Bound total disk use as repositories accumulate across leaderboard pages.
	entries, _ := os.ReadDir(directory)
	type cacheFile struct {
		name string
		info os.FileInfo
	}
	files := make([]cacheFile, 0, len(entries))
	var total int64
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 69 || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := hex.DecodeString(name[:64]); err != nil {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			files = append(files, cacheFile{name: name, info: info})
			total += info.Size()
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().Before(files[j].info.ModTime()) })
	for len(files) > 0 && (len(files) > maxHubAvailabilityCacheFiles || total > maxHubAvailabilityCacheTotal) {
		file := files[0]
		if os.Remove(filepath.Join(directory, file.name)) == nil {
			total -= file.info.Size()
		}
		files = files[1:]
	}
}
