package sources

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	skillstore "racomi/internal/skills"
)

const maxSearchBytes = 2 << 20
const skillListPageSize = 200

// Clients in different application services must not race over the same bare
// repository. A preview holds this cache lock until its commit is extracted.
var cacheLocks sync.Map

type previewRecord struct {
	preview Preview
	root    string
	issues  []repositoryIssue
}

type Client struct {
	cacheDir   string
	http       *http.Client
	searchBase string
	mu         *sync.Mutex
	previews   map[string]previewRecord
}

func New(cacheDir string) *Client {
	if absolute, err := filepath.Abs(cacheDir); err == nil {
		cacheDir = absolute
	}
	lock, _ := cacheLocks.LoadOrStore(cacheDir, &sync.Mutex{})
	return &Client{
		cacheDir: cacheDir, http: &http.Client{Timeout: 20 * time.Second},
		searchBase: "https://skills.sh", previews: make(map[string]previewRecord), mu: lock.(*sync.Mutex),
	}
}

func (c *Client) Search(ctx context.Context, q string) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		page, err := c.ListSkills(ctx, 0)
		return page.Skills, err
	}
	if len(q) < 2 || len(q) > 200 {
		return nil, errors.New("search query must contain between 2 and 200 bytes")
	}
	params := url.Values{"q": {q}, "limit": {"20"}}
	body, err := c.searchResponse(ctx, "/api/search?"+params.Encode())
	if err != nil {
		return nil, err
	}
	var payload struct {
		Skills *[]searchSkill `json:"skills"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Skills == nil {
		return nil, errors.New("skills.sh returned an invalid search response")
	}
	results, err := searchResults(*payload.Skills)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Installs > results[j].Installs })
	return results, nil
}

// ListSkills uses the same public all-time leaderboard as skills.sh. Its pages
// contain 200 entries and the server order is preserved across loading pages.
func (c *Client) ListSkills(ctx context.Context, page int) (SkillListPage, error) {
	if page < 0 {
		return SkillListPage{}, errors.New("skills.sh leaderboard page must be non-negative")
	}
	body, err := c.searchResponse(ctx, fmt.Sprintf("/api/skills/all-time/%d", page))
	if err != nil {
		return SkillListPage{}, err
	}
	var payload struct {
		Skills  *[]searchSkill `json:"skills"`
		Total   *int           `json:"total"`
		HasMore *bool          `json:"hasMore"`
		Page    *int           `json:"page"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Skills == nil || payload.Total == nil || payload.HasMore == nil || payload.Page == nil {
		return SkillListPage{}, errors.New("skills.sh returned an invalid leaderboard response")
	}
	if *payload.Page != page || *payload.Total < len(*payload.Skills) || len(*payload.Skills) > skillListPageSize || (*payload.HasMore && len(*payload.Skills) == 0) {
		return SkillListPage{}, errors.New("skills.sh returned invalid leaderboard pagination")
	}
	for i := range *payload.Skills {
		item := &(*payload.Skills)[i]
		item.ID = item.Source + "/" + item.SkillID
	}
	results, err := searchResults(*payload.Skills)
	if err != nil {
		return SkillListPage{}, err
	}
	return SkillListPage{Skills: results, Total: *payload.Total, HasMore: *payload.HasMore, Page: *payload.Page}, nil
}

type searchSkill struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	Installs int    `json:"installs"`
}

func (c *Client) searchResponse(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.searchBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "racomi")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("skills.sh search failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skills.sh search returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSearchBytes+1))
	if err != nil || len(body) > maxSearchBytes {
		return nil, errors.New("skills.sh search response could not be read or exceeds 2 MiB")
	}
	return body, nil
}

func searchResults(items []searchSkill) ([]SearchResult, error) {
	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		parts := strings.Split(item.ID, "/")
		if (len(parts) != 2 && len(parts) != 3) || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) || !safeSkillName.MatchString(parts[len(parts)-1]) || item.Name == "" || strings.ContainsAny(item.Name, "\x00\r\n") || item.Installs < 0 {
			return nil, errors.New("skills.sh returned malformed skill metadata")
		}
		prefix := strings.Join(parts[:len(parts)-1], "/")
		if item.Source == "" {
			item.Source = prefix
		}
		if item.Source != prefix {
			return nil, errors.New("skills.sh returned an invalid repository source")
		}
		if len(parts) == 2 {
			if !validHostedDomain(item.Source) {
				return nil, errors.New("skills.sh returned an invalid hosted source")
			}
		} else if _, err := parseSource(item.Source, "", ""); err != nil {
			return nil, errors.New("skills.sh returned an invalid repository source")
		}
		result := SearchResult{Name: item.Name, Slug: item.ID, Source: item.Source, Installs: item.Installs, URL: "https://skills.sh/" + item.ID, Supported: true}
		results = append(results, result)
	}
	return results, nil
}

// Preview refreshes a repository once and discovers skills in an immutable commit
// snapshot. Subsequent previews cannot change files offered by an earlier one.
func (c *Client) Preview(ctx context.Context, input, ref, subpath string) (Preview, error) {
	spec, err := parseSource(input, ref, subpath)
	if err != nil {
		return Preview{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return c.previewLocked(ctx, spec, true)
}

func (c *Client) previewLocked(ctx context.Context, spec sourceSpec, refresh bool) (Preview, error) {
	return c.previewOptionsLocked(ctx, spec, refresh, false)
}

func (c *Client) previewOptionsLocked(ctx context.Context, spec sourceSpec, refresh, allowEmpty bool, priorityPaths ...string) (Preview, error) {
	if spec.hosted {
		return c.previewHostedLocked(ctx, spec, allowEmpty)
	}
	repo, err := c.repository(ctx, spec.source, refresh)
	if err != nil {
		return Preview{}, err
	}
	if spec.treeTail != "" {
		// GitHub branches can contain '/'. Resolve the longest existing ref prefix.
		parts := strings.Split(spec.treeTail, "/")
		matched := false
		for n := len(parts); n >= 1; n-- {
			treeRef := strings.Join(parts[:n], "/")
			if spec.ref != "" && spec.ref != treeRef {
				continue
			}
			if _, err := resolveRevision(ctx, repo, treeRef); err == nil {
				spec.ref = treeRef
				if spec.subpath == "." && n < len(parts) {
					spec.subpath = strings.Join(parts[n:], "/")
				}
				matched = true
				break
			}
		}
		if !matched {
			return Preview{}, errors.New("GitHub tree URL does not identify an available branch, tag, or commit")
		}
	}
	resolvedRef := spec.ref
	if resolvedRef == "" {
		// Store the actual default branch so future checks follow the same source.
		if out, err := gitCommand(ctx, repo, "symbolic-ref", "--short", "HEAD"); err == nil {
			resolvedRef = strings.TrimSpace(out)
		} else {
			resolvedRef = "HEAD"
		}
	}
	revision, err := resolveRevision(ctx, repo, resolvedRef)
	if err != nil {
		return Preview{}, fmt.Errorf("cannot resolve Git ref %q: %w", resolvedRef, err)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Preview{}, err
	}
	id := hex.EncodeToString(idBytes)
	root := filepath.Join(c.cacheDir, "previews", id)
	if err := os.MkdirAll(root, 0700); err != nil {
		return Preview{}, err
	}
	issues, err := extractCommit(ctx, repo, revision, root, spec.subpath, spec.skill, priorityPaths...)
	if err != nil {
		os.RemoveAll(root)
		return Preview{}, err
	}
	items, skipped, warnings, err := scanSkills(root, spec.subpath, revision, issues, allowEmpty)
	if err != nil {
		os.RemoveAll(root)
		return Preview{}, err
	}
	if spec.skill != "" {
		selected := make([]CandidateInfo, 0, 1)
		for _, item := range items {
			if item.Name == spec.skill || filepath.Base(item.Subpath) == spec.skill {
				selected = append(selected, item)
			}
		}
		if len(selected) != 1 {
			os.RemoveAll(root)
			return Preview{}, fmt.Errorf("skills.sh skill %q did not match exactly one repository skill", spec.skill)
		}
		items = selected
	}
	result := Preview{ID: id, Source: spec.source, Ref: resolvedRef, Subpath: spec.subpath, Revision: revision, Candidates: items, Skipped: skipped, Warnings: warnings}
	c.previews[id] = previewRecord{preview: result, root: root, issues: issues}
	return result, nil
}

// Candidates accepts only subpaths offered by a successful preview. An empty
// selection intentionally installs nothing; callers must select explicitly.
func (c *Client) Candidates(previewID string, paths []string) ([]skillstore.Candidate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.previews[previewID]
	if !ok {
		return nil, errors.New("preview is no longer available; preview the source again")
	}
	result := make([]skillstore.Candidate, 0, len(paths))
	seen := make(map[string]bool)
	for _, selected := range paths {
		cleaned, err := cleanSubpath(selected)
		if err != nil {
			return nil, err
		}
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		var found *CandidateInfo
		for i := range record.preview.Candidates {
			if record.preview.Candidates[i].Subpath == cleaned {
				found = &record.preview.Candidates[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("skill %q was not offered by this preview", cleaned)
		}
		candidatePath := filepath.Join(record.root, filepath.FromSlash(cleaned))
		if err := validateTree(candidatePath); err != nil {
			return nil, err
		}
		result = append(result, skillstore.Candidate{Skill: skillstore.Skill{
			Name: found.Name, Description: found.Description, Source: record.preview.Source,
			Ref: record.preview.Ref, Subpath: cleaned, Revision: record.preview.Revision, ContentHash: found.ContentHash,
		}, Path: candidatePath})
		if record.preview.Local {
			// Keep identity tied to the original skill directory, including when
			// it is previewed through its parent rather than on its own. The
			// source stays empty because local imports have no Git provenance.
			origin := filepath.Join(record.preview.Source, filepath.FromSlash(cleaned))
			hash := sha256.Sum256([]byte(origin))
			result[len(result)-1].Skill.ID = "skill-local-" + hex.EncodeToString(hash[:12])
			result[len(result)-1].Skill.Source = ""
		}
	}
	return result, nil
}

// Release discards an immutable preview after its selected candidates have been
// copied into the store. Unknown IDs are harmless and never touch the disk.
func (c *Client) Release(previewID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.releaseLocked(previewID)
}

// ReleaseCandidate releases only a preview owned by this Client whose offered
// candidate path matches exactly. It never deletes an arbitrary caller path.
func (c *Client) ReleaseCandidate(candidate skillstore.Candidate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, record := range c.previews {
		for _, offered := range record.preview.Candidates {
			expected := filepath.Join(record.root, filepath.FromSlash(offered.Subpath))
			if candidate.Path == expected {
				return c.releaseLocked(id)
			}
		}
	}
	return nil
}

func (c *Client) releaseLocked(id string) error {
	record, ok := c.previews[id]
	if !ok {
		return nil
	}
	// The deletion target comes exclusively from a record created by Preview,
	// and must be the immediate preview root, never a selected skill subtree.
	expected := filepath.Join(c.cacheDir, "previews", id)
	if record.root != expected || filepath.Dir(record.root) != filepath.Join(c.cacheDir, "previews") {
		return errors.New("preview cache path is invalid")
	}
	if err := os.RemoveAll(record.root); err != nil {
		return fmt.Errorf("cannot release preview cache: %w", err)
	}
	delete(c.previews, id)
	return nil
}

// CheckUpdates reports failures per skill and refreshes each source only once.
// It never edits installed content or the immutable preview snapshots.
func (c *Client) CheckUpdates(ctx context.Context, skills []skillstore.Skill) ([]Update, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	type repoResult struct {
		path string
		err  error
	}
	repos := make(map[string]repoResult)
	type hostedResult struct {
		preview Preview
		err     error
	}
	hosted := make(map[string]hostedResult)
	result := make([]Update, 0, len(skills))
	for _, skill := range skills {
		update := Update{SkillID: skill.ID}
		if !validRevision(skill.Revision) {
			update.Error = "installed skill has an invalid Git revision; import it again"
			result = append(result, update)
			continue
		}
		spec, err := parseSource(skill.Source, skill.Ref, skill.Subpath)
		if err != nil {
			update.Error = err.Error()
			result = append(result, update)
			continue
		}
		if spec.hosted {
			key := spec.source + "\x00" + spec.subpath
			r, ok := hosted[key]
			if !ok {
				r.preview, r.err = c.previewHostedLocked(ctx, spec, true)
				hosted[key] = r
				if r.err == nil {
					defer c.releaseLocked(r.preview.ID)
				}
			}
			if r.err != nil {
				update.Error = r.err.Error()
			} else {
				update.Revision, update.Removed = r.preview.Revision, true
				for _, item := range r.preview.Candidates {
					if item.Subpath == skill.Subpath {
						update.Removed = false
						update.Available = item.ContentHash != skill.ContentHash
						if update.Available {
							update.Summary = "Skill files changed."
						}
						break
					}
				}
			}
			result = append(result, update)
			continue
		}
		r, ok := repos[spec.source]
		if !ok {
			r.path, r.err = c.repository(ctx, spec.source, true)
			repos[spec.source] = r
		}
		if r.err != nil {
			update.Error = r.err.Error()
			result = append(result, update)
			continue
		}
		ref := spec.ref
		if ref == "" {
			ref = "HEAD"
		}
		revision, err := resolveRevision(ctx, r.path, ref)
		if err != nil {
			update.Error = err.Error()
		} else {
			update.Revision = revision
			metadataPath := "SKILL.md"
			if spec.subpath != "." {
				metadataPath = spec.subpath + "/SKILL.md"
			}
			metadata, treeErr := gitCommand(ctx, r.path, "ls-tree", revision, "--", metadataPath)
			if treeErr != nil {
				update.Error = treeErr.Error()
				result = append(result, update)
				continue
			}
			if strings.TrimSpace(metadata) == "" {
				update.Removed = true
				update.Summary = "上游已移除，已保留本地版本。"
				result = append(result, update)
				continue
			}
			if revision != skill.Revision {
				changed, diffErr := gitCommand(ctx, r.path, "diff", "--name-only", skill.Revision, revision, "--", spec.subpath)
				if diffErr != nil {
					// An old commit can disappear after a source history rewrite.
					update.Available = true
					update.Summary = "Source history changed; download the current version to review it."
				} else if strings.TrimSpace(changed) != "" {
					update.Available = true
					summary, _ := gitCommand(ctx, r.path, "log", "--format=%h %s", "-5", skill.Revision+".."+revision, "--", spec.subpath)
					update.Summary = strings.TrimSpace(summary)
					if update.Summary == "" {
						update.Summary = "Skill files changed."
					}
				}
			}
		}
		result = append(result, update)
	}
	return result, nil
}

func (c *Client) PrepareUpdate(ctx context.Context, skill skillstore.Skill) (skillstore.Candidate, error) {
	preview, err := c.Preview(ctx, skill.Source, skill.Ref, skill.Subpath)
	if err != nil {
		return skillstore.Candidate{}, err
	}
	candidates, err := c.Candidates(preview.ID, []string{skill.Subpath})
	if err != nil || len(candidates) != 1 {
		if err == nil {
			err = errors.New("updated repository no longer contains this skill")
		}
		return skillstore.Candidate{}, err
	}
	if candidates[0].Skill.Name != skill.Name {
		return skillstore.Candidate{}, errors.New("updated skill changed its name; import it separately")
	}
	candidates[0].Skill.ID = skill.ID
	return candidates[0], nil
}

func (c *Client) repository(ctx context.Context, source string, refresh bool) (string, error) {
	hash := sha256.Sum256([]byte(source))
	parent := filepath.Join(c.cacheDir, "repositories")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	repo := filepath.Join(parent, hex.EncodeToString(hash[:]))
	if _, err := os.Stat(filepath.Join(repo, "HEAD")); errors.Is(err, os.ErrNotExist) {
		tmp, err := os.MkdirTemp(parent, ".clone-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		if _, err := gitCommand(ctx, "", "clone", "--bare", "--no-local", "--", source, tmp); err != nil {
			return "", fmt.Errorf("cannot clone repository: %w", err)
		}
		if err := os.Rename(tmp, repo); err != nil {
			return "", err
		}
		return repo, nil
	} else if err != nil {
		return "", err
	}
	if refresh {
		if _, err := gitCommand(ctx, repo, "fetch", "--prune", "--force", "--tags", "--", "origin", "+refs/heads/*:refs/heads/*"); err != nil {
			return "", fmt.Errorf("cannot refresh repository: %w", err)
		}
	}
	return repo, nil
}

func resolveRevision(ctx context.Context, repo, ref string) (string, error) {
	if !validRef(ref) || ref == "" {
		return "", errors.New("invalid Git ref")
	}
	// --end-of-options makes even hostile ref values data rather than options.
	out, err := gitCommand(ctx, repo, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	revision := strings.TrimSpace(out)
	if !validRevision(revision) {
		return "", errors.New("Git returned an invalid commit")
	}
	return revision, nil
}

func validRevision(revision string) bool {
	if len(revision) != 40 && len(revision) != 64 {
		return false
	}
	_, err := hex.DecodeString(revision)
	return err == nil
}

type limitedBuffer struct {
	data  []byte
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if left := b.limit - len(b.data); left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func gitExec(ctx context.Context, repo string, args ...string) *exec.Cmd {
	base := []string{"-c", "core.hooksPath=/dev/null", "-c", "credential.interactive=never", "-c", "protocol.ext.allow=never", "-c", "core.sshCommand=ssh -oBatchMode=yes -oConnectTimeout=15"}
	if repo != "" {
		base = append(base, "--git-dir", repo)
	}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = make([]string, 0, len(os.Environ())+4)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GIT_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C")
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

func gitCommand(parent context.Context, repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := gitExec(ctx, repo, args...)
	out := &limitedBuffer{limit: 64 << 10}
	errOut := &limitedBuffer{limit: 4 << 10}
	cmd.Stdout, cmd.Stderr = out, errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("Git operation timed out: %w", ctx.Err())
			}
			return "", fmt.Errorf("Git operation was canceled: %w", ctx.Err())
		}
		message := strings.TrimSpace(string(errOut.data))
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}
	return string(out.data), nil
}
