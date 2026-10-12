package sources

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Tree metadata is cheap to inspect even when unrelated blobs are enormous.
// Its own limits bound discovery independently of the materialized preview.
const maxTreeBytes = 32 << 20
const maxTreeEntries = 200000

type repositoryEntry struct {
	Path, Kind, Mode, Object string
	Size                     int64
}

func inspectCommit(ctx context.Context, repo, revision string) ([]repositoryEntry, []repositoryIssue, error) {
	cmd := gitExec(ctx, repo, "ls-tree", "--full-tree", "-r", "-t", "-l", "-z", revision)
	errOut := &limitedBuffer{limit: 4 << 10}
	cmd.Stderr = errOut
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, nil, err
	}
	entries, issues, readErr := readCommitTree(pipe)
	if readErr != nil {
		_ = pipe.Close()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, nil, readErr
	}
	if waitErr != nil {
		return nil, nil, fmt.Errorf("cannot read Git tree: %s", strings.TrimSpace(string(errOut.data)))
	}
	return entries, issues, nil
}

func readCommitTree(reader io.Reader) ([]repositoryEntry, []repositoryIssue, error) {
	limited := &io.LimitedReader{R: reader, N: maxTreeBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Split(splitTreeEntry)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	entries, issues := make([]repositoryEntry, 0), make([]repositoryIssue, 0)
	for scanner.Scan() {
		if len(entries) >= maxTreeEntries {
			return nil, nil, errors.New("repository has too many tree entries (maximum 200,000)")
		}
		metadata, name, ok := strings.Cut(scanner.Text(), "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 4 || !validRevision(fields[2]) {
			return nil, nil, errors.New("invalid Git tree entry")
		}
		cleaned, err := cleanSubpath(name)
		if err != nil || cleaned == "." || cleaned != name {
			return nil, nil, fmt.Errorf("unsafe repository archive path %q", name)
		}
		for _, part := range strings.Split(cleaned, "/") {
			if strings.EqualFold(part, ".git") {
				return nil, nil, errors.New("repository archive contains a reserved .git path")
			}
		}
		entry := repositoryEntry{Path: cleaned, Mode: fields[0], Kind: fields[1], Object: fields[2]}
		if entry.Kind == "blob" {
			entry.Size, err = strconv.ParseInt(fields[3], 10, 64)
			if err != nil || entry.Size < 0 {
				return nil, nil, errors.New("invalid Git blob size")
			}
		}
		if entry.Mode == "120000" {
			issues = append(issues, repositoryIssue{Path: cleaned, Reason: "symlink is not supported"})
		} else if !entry.regular() && entry.Kind != "tree" {
			issues = append(issues, repositoryIssue{Path: cleaned, Reason: "unsupported repository file type"})
		}
		entries = append(entries, entry)
	}
	if limited.N <= 0 {
		return nil, nil, errors.New("repository tree metadata exceeds the 32 MiB preview limit")
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("invalid Git tree: %w", err)
	}
	return entries, issues, nil
}

func splitTreeEntry(data []byte, atEOF bool) (int, []byte, error) {
	if end := bytes.IndexByte(data, 0); end >= 0 {
		return end + 1, data[:end], nil
	}
	if atEOF && len(data) != 0 {
		return 0, nil, errors.New("unterminated Git tree entry")
	}
	return 0, nil, nil
}

func (entry repositoryEntry) regular() bool {
	return entry.Kind == "blob" && (entry.Mode == "100644" || entry.Mode == "100755")
}

type skillExtraction struct {
	marker   repositoryEntry
	files    []int
	bytes    int64
	count    int
	blocked  bool
	priority bool
}

func planSkillExtraction(ctx context.Context, repo string, entries []repositoryEntry, issues []repositoryIssue, subpath, skill string, priorityPaths ...string) ([]string, []repositoryIssue, error) {
	roots, issues := discoverSkillRoots(entries, issues, subpath)
	if len(priorityPaths) > 0 {
		// A scoped discovery can later register a wider source. Rebuilding its
		// reviewed revision must reserve the budget for the requested sync paths.
		selected := make(map[string]bool, len(priorityPaths))
		for _, rootPath := range priorityPaths {
			selected[rootPath] = true
		}
		for rootPath, root := range roots {
			root.priority = selected[rootPath]
		}
	}
	// Validate in-scope frontmatter before allocating content: an invalid
	// skill must not consume the budget needed by a healthy neighbor.
	metadataIssues, err := inspectSkillMetadata(ctx, repo, roots, skill)
	if err != nil {
		return nil, nil, err
	}
	issues = append(issues, metadataIssues...)
	files, issues := allocateSkillExtraction(entries, issues, roots)
	if skill != "" {
		matches := 0
		for _, root := range roots {
			if root.priority && !root.blocked {
				matches++
			}
		}
		// Budget skips cannot turn an ambiguous skills.sh selector into an
		// apparently unique match. Count before any files are materialized.
		if matches != 1 {
			return nil, nil, fmt.Errorf("skills.sh skill %q did not match exactly one repository skill", skill)
		}
	}
	return files, issues, nil
}

func discoverSkillRoots(entries []repositoryEntry, issues []repositoryIssue, subpath string) (map[string]*skillExtraction, []repositoryIssue) {
	roots := make(map[string]*skillExtraction)
	for _, entry := range entries {
		if path.Base(entry.Path) != "SKILL.md" {
			continue
		}
		root := path.Dir(entry.Path)
		ignored := false
		for _, part := range strings.Split(root, "/") {
			ignored = ignored || ignoredDirectory(part)
		}
		if !ignored {
			blocked := entry.Kind == "tree" || entry.regular() && entry.Size > 1<<20
			if blocked {
				issues = append(issues, repositoryIssue{Path: entry.Path, Reason: "SKILL.md must be a regular file smaller than 1 MiB", SkillOnly: true})
			}
			roots[root] = &skillExtraction{marker: entry, blocked: blocked, priority: repositoryPathWithin(subpath, root)}
		}
	}
	return roots, issues
}

func allocateSkillExtraction(entries []repositoryEntry, issues []repositoryIssue, roots map[string]*skillExtraction) ([]string, []repositoryIssue) {
	for i, entry := range entries {
		for parent := path.Dir(entry.Path); ; parent = path.Dir(parent) {
			if root := roots[parent]; root != nil {
				root.count++
				if entry.regular() {
					// Saturate totals: gigantic Git blobs must not overflow a budget.
					if entry.Size > maxRepositoryBytes-root.bytes {
						root.bytes = maxRepositoryBytes + 1
					} else {
						root.bytes += entry.Size
					}
					root.files = append(root.files, i)
				} else if entry.Kind != "tree" {
					root.blocked = true
				}
			}
			if parent == "." {
				break
			}
		}
	}
	paths := make([]string, 0, len(roots))
	for root := range roots {
		paths = append(paths, root)
	}
	sort.Slice(paths, func(i, j int) bool {
		if roots[paths[i]].priority != roots[paths[j]].priority {
			return roots[paths[i]].priority
		}
		return paths[i] < paths[j]
	})
	selected := make(map[string]bool)
	materialized := make(map[string]bool)
	var total int64
	for _, rootPath := range paths {
		root := roots[rootPath]
		if root.blocked {
			// Keep its identity even when none of the blocked tree is extracted.
			// Earlier entry issues retain the precise offending path and reason.
			issues = append(issues, repositoryIssue{Path: root.marker.Path, Reason: "unsupported repository file type", SkillOnly: true})
			continue
		}
		reason := ""
		if root.bytes > maxRepositoryBytes {
			reason = "skill content exceeds the 100 MiB preview limit"
		} else if root.count > maxRepositoryEntries {
			reason = "skill has too many files (maximum 20,000)"
		}
		additional := make(map[string]bool)
		var extraBytes int64
		if reason == "" {
			for _, i := range root.files {
				entry := entries[i]
				if !selected[entry.Path] {
					extraBytes += entry.Size
				}
				for p := entry.Path; p != "."; p = path.Dir(p) {
					if !materialized[p] {
						additional[p] = true
					}
				}
			}
			if extraBytes > maxRepositoryBytes-total {
				reason = "skill was skipped because the preview reached its 100 MiB content limit"
			} else if len(materialized)+len(additional) >= maxRepositoryEntries {
				// Reserve one archive entry for Git's global PAX commit header.
				reason = "skill was skipped because the preview reached its 20,000 file limit"
			}
		}
		if reason != "" {
			// The marker excludes this skill without excluding valid nested skills.
			issues = append(issues, repositoryIssue{Path: root.marker.Path, Reason: reason, SkillOnly: true})
			continue
		}
		for _, i := range root.files {
			selected[entries[i].Path] = true
		}
		for p := range additional {
			materialized[p] = true
		}
		total += extraBytes
	}
	files := make([]string, 0, len(selected))
	for p := range selected {
		files = append(files, p)
	}
	sort.Strings(files)
	return files, issues
}

func inspectSkillMetadata(parent context.Context, repo string, roots map[string]*skillExtraction, selected string) ([]repositoryIssue, error) {
	markers := make([]*skillExtraction, 0, len(roots))
	for _, root := range roots {
		if root.priority && root.marker.regular() && root.marker.Size <= 1<<20 {
			markers = append(markers, root)
		}
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].marker.Path < markers[j].marker.Path })
	if len(markers) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var input strings.Builder
	for _, root := range markers {
		input.WriteString(root.marker.Object + "\n")
	}
	cmd := gitExec(ctx, repo, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(input.String())
	errOut := &limitedBuffer{limit: 4 << 10}
	cmd.Stderr = errOut
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(pipe)
	issues := make([]repositoryIssue, 0)
	var total int64
	for _, root := range markers {
		header, readErr := reader.ReadString('\n')
		fields := strings.Fields(header)
		if readErr != nil || len(fields) != 3 || fields[0] != root.marker.Object || fields[1] != "blob" || fields[2] != strconv.FormatInt(root.marker.Size, 10) {
			err = errors.New("cannot read skill metadata from Git")
			break
		}
		total += root.marker.Size
		if total > maxRepositoryBytes {
			err = errors.New("skill metadata exceeds the 100 MiB preview limit")
			break
		}
		body := make([]byte, int(root.marker.Size)+1)
		if _, err = io.ReadFull(reader, body); err != nil || body[len(body)-1] != '\n' {
			err = errors.New("cannot read skill metadata from Git")
			break
		}
		name, _, metadataErr := metadata(body[:len(body)-1])
		if metadataErr != nil {
			root.blocked = true
			issues = append(issues, repositoryIssue{Path: root.marker.Path, Reason: metadataErr.Error(), SkillOnly: true})
		}
		// A skills.sh name may match the frontmatter or the directory. Scope
		// selection retains its original priority when there is no name selector.
		if selected != "" {
			root.priority = name == selected || path.Base(path.Dir(root.marker.Path)) == selected
		}
	}
	if err != nil {
		cancel()
		_ = pipe.Close()
	}
	waitErr := cmd.Wait()
	if err != nil {
		return nil, err
	}
	if waitErr != nil {
		return nil, fmt.Errorf("cannot read skill metadata from Git: %s", strings.TrimSpace(string(errOut.data)))
	}
	return issues, nil
}
