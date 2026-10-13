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
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	skillstore "racomi/internal/skills"
)

type hostedEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
	Type        string   `json:"type"`
	URL         string   `json:"url"`
	Digest      string   `json:"digest"`
}

// Hosted requests only read HTTPS data. Redirects must retain HTTPS and cannot
// introduce credentials, even when a publisher serves artifacts through a CDN.
func (c *Client) hostedResponse(ctx context.Context, address string, limit int64) ([]byte, error) {
	body, _, err := c.hostedDownload(ctx, address, limit)
	return body, err
}

func (c *Client) hostedDownload(ctx context.Context, address string, limit int64) ([]byte, *url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, nil, errors.New("hosted skill URLs must use HTTPS without credentials or fragments")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "racomi")
	client := *c.http
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 10 {
			return errors.New("unsafe hosted skill redirect")
		}
		if c.http.CheckRedirect != nil {
			return c.http.CheckRedirect(req, via)
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("hosted skill download failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("hosted skill download returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(body)) > limit {
		return nil, nil, errors.New("hosted skill response exceeds the download limit")
	}
	if response.Request != nil {
		u = response.Request.URL
	}
	return body, u, nil
}

func (c *Client) hostedIndex(ctx context.Context, source string) ([]hostedEntry, *url.URL, error) {
	base := strings.TrimRight(source, "/")
	addresses := []string{base + "/index.json"}
	if !strings.HasSuffix(base, "/.well-known/skills") && !strings.HasSuffix(base, "/.well-known/agent-skills") {
		addresses = []string{base + "/.well-known/agent-skills/index.json", base + "/.well-known/skills/index.json"}
	}
	var lastErr error
	for _, address := range addresses {
		body, indexURL, err := c.hostedDownload(ctx, address, maxSearchBytes)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			continue
		}
		var payload struct {
			Schema string         `json:"$schema"`
			Skills *[]hostedEntry `json:"skills"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Skills == nil {
			// Some hosts return their HTML app for unknown routes. Treat that
			// as missing discovery and try the legacy endpoint as upstream does.
			lastErr = errors.New("hosted source returned an invalid skill index")
			continue
		}
		if payload.Schema != "" && payload.Schema != "https://schemas.agentskills.io/discovery/0.2.0/schema.json" {
			return nil, nil, errors.New("hosted source uses an unsupported discovery schema")
		}
		if len(*payload.Skills) > maxRepositoryEntries {
			return nil, nil, errors.New("hosted source has too many skills")
		}
		seen := map[string]bool{}
		for _, item := range *payload.Skills {
			key := strings.ToLower(item.Name)
			if !safeSkillName.MatchString(item.Name) || seen[key] || strings.TrimSpace(item.Description) == "" {
				return nil, nil, errors.New("hosted source returned invalid or duplicate skill metadata")
			}
			seen[key] = true
			if payload.Schema != "" && item.Type == "" {
				return nil, nil, errors.New("hosted discovery v0.2 requires artifact types")
			}
		}
		return *payload.Skills, indexURL, nil
	}
	return nil, nil, fmt.Errorf("cannot discover hosted skills: %w", lastErr)
}

func hostedFilePath(name string) bool {
	cleaned, err := cleanSubpath(name)
	if err != nil || len(name) > 4096 || cleaned == "." || cleaned != name || strings.Contains(name, ":") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func hostedArtifactURL(index *url.URL, value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || value == "" {
		return "", errors.New("invalid hosted artifact URL")
	}
	u = index.ResolveReference(u)
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", errors.New("hosted artifact URLs must use HTTPS without credentials or fragments")
	}
	return u.String(), nil
}

func (c *Client) downloadHostedSkill(ctx context.Context, index *url.URL, item hostedEntry, root string, bytesRead *int64, entries *int) error {
	if item.Type != "" {
		if item.Type != "archive" && item.Type != "skill-md" {
			return errors.New("unsupported hosted skill artifact type")
		}
		expected := strings.TrimPrefix(item.Digest, "sha256:")
		if !strings.HasPrefix(item.Digest, "sha256:") || len(expected) != 64 || !validRevision(expected) {
			return errors.New("hosted artifact requires a SHA-256 digest")
		}
		address, err := hostedArtifactURL(index, item.URL)
		if err != nil {
			return err
		}
		limit := int64(maxRepositoryBytes)
		if item.Type == "skill-md" {
			limit = 1 << 20
		}
		body, err := c.hostedResponse(ctx, address, limit)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		if !strings.EqualFold(hex.EncodeToString(hash[:]), expected) {
			return errors.New("hosted artifact SHA-256 digest does not match")
		}
		if item.Type == "archive" {
			if err = extractHostedArchive(ctx, body, root); err != nil {
				return err
			}
		} else if err = os.WriteFile(filepath.Join(root, "SKILL.md"), body, 0644); err != nil {
			return err
		}
		return fs.WalkDir(os.DirFS(root), ".", func(name string, entry fs.DirEntry, err error) error {
			if err != nil || name == "." {
				return err
			}
			*entries++
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				*bytesRead += info.Size()
			}
			if *entries > maxRepositoryEntries || *bytesRead > maxRepositoryBytes {
				return errors.New("hosted source exceeds the preview content limit")
			}
			return ctx.Err()
		})
	}
	if len(item.Files) == 0 || len(item.Files) > maxRepositoryEntries {
		return errors.New("hosted skill requires a bounded file manifest")
	}
	paths := map[string]hostedArchivePath{}
	for _, file := range item.Files {
		if !hostedFilePath(file) {
			return fmt.Errorf("unsafe hosted skill file %q", file)
		}
		if err := registerHostedPath(paths, file, false); err != nil {
			return err
		}
	}
	marker, ok := paths[foldHostedArchivePath("SKILL.md")]
	if !ok || marker.name != "SKILL.md" || marker.dir {
		return errors.New("hosted skill manifest must include SKILL.md")
	}
	*entries += len(paths)
	if *entries > maxRepositoryEntries {
		return errors.New("hosted source has too many files")
	}
	// A bounded pool keeps manifests such as lark-doc's 44 files responsive.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	jobs := make(chan string)
	for range 6 {
		wg.Go(func() {
			for file := range jobs {
				if ctx.Err() != nil {
					continue
				}
				address := index.ResolveReference(&url.URL{Path: item.Name + "/" + file}).String()
				limit := int64(maxRepositoryBytes)
				if file == "SKILL.md" {
					limit = 1 << 20
				}
				body, err := c.hostedResponse(ctx, address, limit)
				mu.Lock()
				if err == nil {
					*bytesRead += int64(len(body))
					if *bytesRead > maxRepositoryBytes {
						err = errors.New("hosted source exceeds the 100 MiB preview limit")
					}
				}
				if err == nil {
					target := filepath.Join(root, filepath.FromSlash(file))
					err = os.MkdirAll(filepath.Dir(target), 0755)
					if err == nil {
						var output *os.File
						output, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
						if err == nil {
							_, err = output.Write(body)
							if closeErr := output.Close(); err == nil {
								err = closeErr
							}
						}
					}
				}
				if err != nil && firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		})
	}
	for _, file := range item.Files {
		select {
		case jobs <- file:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (c *Client) hostedSnapshotBase(spec sourceSpec) string {
	hash := sha256.Sum256([]byte(spec.source + "\x00" + spec.subpath))
	return filepath.Join(c.cacheDir, "hosted", hex.EncodeToString(hash[:]))
}

func (c *Client) previewHostedLocked(ctx context.Context, spec sourceSpec, allowEmpty bool) (Preview, error) {
	items, index, err := c.hostedIndex(ctx, spec.source)
	if err != nil {
		return Preview{}, err
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		return Preview{}, err
	}
	id := hex.EncodeToString(idBytes)
	root := filepath.Join(c.cacheDir, "previews", id)
	if err = os.MkdirAll(root, 0700); err != nil {
		return Preview{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(root)
		}
	}()
	var size int64
	entries := 0
	for _, item := range items {
		if spec.subpath != "." && item.Name != spec.subpath {
			continue
		}
		entries++
		if entries > maxRepositoryEntries {
			return Preview{}, errors.New("hosted source has too many entries")
		}
		directory := filepath.Join(root, item.Name)
		if err = os.MkdirAll(directory, 0755); err != nil {
			return Preview{}, err
		}
		if err = c.downloadHostedSkill(ctx, index, item, directory, &size, &entries); err != nil {
			return Preview{}, fmt.Errorf("cannot download hosted skill %q: %w", item.Name, err)
		}
		if _, err := readSkill(root, item.Name, ""); err != nil {
			return Preview{}, fmt.Errorf("hosted skill %q has invalid SKILL.md metadata: %w", item.Name, err)
		}
	}
	revision, err := skillstore.HashDir(root)
	if err != nil {
		return Preview{}, err
	}
	candidates, err := hostedCandidates(root, spec.subpath, revision)
	if err != nil {
		return Preview{}, err
	}
	if !allowEmpty && len(candidates) == 0 {
		return Preview{}, errors.New("hosted source does not contain the selected skill")
	}
	// Persist the reviewed bytes independently of the releasable preview. Sync
	// after a restart uses this exact snapshot and never re-downloads a new head.
	base := c.hostedSnapshotBase(spec)
	if err = os.MkdirAll(base, 0700); err != nil {
		return Preview{}, err
	}
	snapshot := filepath.Join(base, revision)
	if cachedHash, hashErr := skillstore.HashDir(snapshot); hashErr != nil || cachedHash != revision {
		// Build next to the target and publish atomically. Repair a damaged
		// owned cache so refreshing can recover from an interrupted write.
		temporary, err := os.MkdirTemp(base, ".snapshot-")
		if err != nil {
			return Preview{}, err
		}
		defer os.RemoveAll(temporary)
		data := filepath.Join(temporary, "data")
		if err = skillstore.CopyDir(root, data); err != nil {
			return Preview{}, err
		}
		if err = os.RemoveAll(snapshot); err != nil {
			return Preview{}, err
		}
		if err = os.Rename(data, snapshot); err != nil {
			return Preview{}, err
		}
	}
	if err = os.WriteFile(filepath.Join(base, "latest"), []byte(revision), 0600); err != nil {
		return Preview{}, err
	}
	result := Preview{ID: id, Hosted: true, Source: spec.source, Subpath: spec.subpath, Revision: revision, Candidates: candidates, Skipped: []SkippedSkill{}, Warnings: []string{}}
	c.previews[id] = previewRecord{preview: result, root: root}
	keep = true
	return result, nil
}

func (c *Client) prepareHostedSyncLocked(ctx context.Context, source skillstore.Source) (Preview, error) {
	spec, err := parseSource(source.Repository, "", source.Subpath)
	if err != nil {
		return Preview{}, err
	}
	snapshot := filepath.Join(c.hostedSnapshotBase(spec), source.Revision)
	hash, err := skillstore.HashDir(snapshot)
	if err != nil || hash != source.Revision {
		return Preview{}, errors.New("reviewed hosted snapshot is missing or changed; refresh the source again")
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		return Preview{}, err
	}
	id := hex.EncodeToString(idBytes)
	root := filepath.Join(c.cacheDir, "previews", id)
	var size int64
	entries := 0
	if err = copyLocalSkill(ctx, snapshot, root, &size, &entries); err != nil {
		_ = os.RemoveAll(root)
		return Preview{}, err
	}
	candidates, err := hostedCandidates(root, spec.subpath, source.Revision)
	if err != nil {
		_ = os.RemoveAll(root)
		return Preview{}, err
	}
	result := Preview{ID: id, Hosted: true, Source: source.Repository, Subpath: spec.subpath, Revision: source.Revision, Candidates: candidates, Skipped: []SkippedSkill{}, Warnings: []string{}}
	c.previews[id] = previewRecord{preview: result, root: root}
	return result, nil
}

// Each discovery entry denotes exactly one skill. Nested SKILL.md examples or
// bundled documentation do not become additional installable candidates.
func hostedCandidates(root, scope, revision string) ([]CandidateInfo, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	items := make([]CandidateInfo, 0, len(entries))
	for _, entry := range entries {
		if scope != "." && scope != entry.Name() {
			continue
		}
		item, err := readSkill(root, entry.Name(), revision)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
