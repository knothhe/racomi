package sources

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	skillstore "racomi/internal/skills"

	"gopkg.in/yaml.v3"
)

const maxRepositoryBytes = 100 << 20
const maxRepositoryEntries = 20000

// repositoryIssue records entries that were intentionally not materialized.
// Keeping the original path lets catalog refresh protect previous skills below
// an unsupported directory as well as skills with unsupported child files.
type repositoryIssue struct {
	Path      string
	Reason    string
	SkillOnly bool // A valid data file can still be an invalid nested skill marker.
}

func extractCommit(parent context.Context, repo, revision, root, subpath, skill string, priorityPaths ...string) ([]repositoryIssue, error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	entries, issues, err := inspectCommit(ctx, repo, revision)
	if err != nil {
		return nil, err
	}
	paths, issues, err := planSkillExtraction(ctx, repo, entries, issues, subpath, skill, priorityPaths...)
	if err != nil {
		return nil, err
	}
	// Preserve a real but empty selected scope, without inventing directories
	// for missing paths or links that replaced previously known skill trees.
	for _, entry := range entries {
		if entry.Path == subpath && entry.Kind == "tree" {
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(subpath)), 0755); err != nil {
				return nil, err
			}
			break
		}
	}
	var extractedBytes int64
	// Bounded argument batches work on macOS even with long repository paths.
	for len(paths) > 0 {
		n, size := 0, 0
		for n < len(paths) && (size+len(paths[n]) < 32<<10 || n == 0) {
			size += len(paths[n]) + 1
			n++
		}
		batchIssues, err := extractPaths(ctx, repo, revision, root, paths[:n], &extractedBytes)
		if err != nil {
			return nil, err
		}
		issues = append(issues, batchIssues...)
		paths = paths[n:]
	}
	return issues, nil
}

func extractPaths(parent context.Context, repo, revision, root string, paths []string, extractedBytes *int64) ([]repositoryIssue, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	args := append([]string{"--literal-pathspecs", "archive", "--format=tar", revision, "--"}, paths...)
	cmd := gitExec(ctx, repo, args...)
	errOut := &limitedBuffer{limit: 4 << 10}
	cmd.Stderr = errOut
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	issues, archiveErr := unpackArchiveBudget(pipe, root, extractedBytes)
	if archiveErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if archiveErr != nil {
		return nil, archiveErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("cannot read Git commit: %s", strings.TrimSpace(string(errOut.data)))
	}
	return issues, nil
}

func unpackArchive(reader io.Reader, root string) ([]repositoryIssue, error) {
	var bytesRead int64
	return unpackArchiveBudget(reader, root, &bytesRead)
}

func unpackArchiveBudget(reader io.Reader, root string, bytesRead *int64) ([]repositoryIssue, error) {
	tr := tar.NewReader(reader)
	issues := make([]repositoryIssue, 0)
	count := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return issues, nil
		}
		if err != nil {
			return nil, fmt.Errorf("invalid Git archive: %w", err)
		}
		count++
		if count > maxRepositoryEntries {
			return nil, errors.New("repository has too many files (maximum 20,000)")
		}
		// Git writes the commit ID as a global PAX metadata header. It is
		// archive metadata, never a repository file to materialize.
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(header.Name, "/")
		cleaned, err := cleanSubpath(name)
		if err != nil || cleaned == "." || cleaned != name {
			return nil, fmt.Errorf("unsafe repository archive path %q", header.Name)
		}
		for _, part := range strings.Split(cleaned, "/") {
			if strings.EqualFold(part, ".git") {
				return nil, errors.New("repository archive contains a reserved .git path")
			}
		}
		if header.Size < 0 || header.Size > maxRepositoryBytes-*bytesRead {
			return nil, errors.New("repository content exceeds the 100 MiB preview limit")
		}
		*bytesRead += header.Size
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			reason := "unsupported repository file type"
			if header.Typeflag == tar.TypeSymlink {
				reason = "symlink is not supported"
			}
			issues = append(issues, repositoryIssue{Path: cleaned, Reason: reason})
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(cleaned))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, err
		}
		mode := os.FileMode(0644)
		if header.Mode&0111 != 0 {
			mode = 0755
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return nil, err
		}
		// OpenFile applies the process umask. Restore the normalized Git mode
		// explicitly so executable files keep their deployment permissions.
		if err := file.Chmod(mode); err != nil {
			file.Close()
			return nil, err
		}
		_, copyErr := io.CopyN(file, tr, header.Size)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
}

func discover(root, subpath, revision string) ([]CandidateInfo, error) {
	return discoverSkills(root, subpath, revision, false)
}

// These wrappers keep ordinary discovery strict about an empty selection.
func discoverSkills(root, subpath, revision string, allowEmpty bool) ([]CandidateInfo, error) {
	items, _, _, err := scanSkills(root, subpath, revision, nil, allowEmpty)
	return items, err
}

// scanSkills reads each skill independently. Unsupported entries are never
// followed, and any issue in a skill's full tree excludes that skill, including
// entries in directories omitted from discovery such as node_modules.
func scanSkills(root, subpath, revision string, issues []repositoryIssue, allowEmpty bool) ([]CandidateInfo, []SkippedSkill, []string, error) {
	cleaned, err := cleanSubpath(subpath)
	if err != nil {
		return nil, nil, nil, err
	}
	subpath = cleaned
	start := filepath.Join(root, filepath.FromSlash(subpath))
	// Own the slice so discoveries cannot mutate a stored preview's issues.
	scanIssues := append([]repositoryIssue{}, issues...)
	result, skipped, warnings := make([]CandidateInfo, 0), make([]SkippedSkill, 0), make([]string, 0)
	skillPaths := make(map[string]bool)
	blocked := false
	current := root
	parts := []string{"."}
	if subpath != "." {
		parts = append(parts, strings.Split(subpath, "/")...)
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if !allowEmpty || !errors.Is(statErr, os.ErrNotExist) {
				return nil, nil, nil, fmt.Errorf("skill subpath %q is not a repository directory", subpath)
			}
			blocked = true
			break
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			rel, _ := filepath.Rel(root, current)
			reason := "unsupported repository file type"
			if info.Mode()&os.ModeSymlink != 0 {
				reason = "symlink is not supported"
			}
			scanIssues = append(scanIssues, repositoryIssue{Path: filepath.ToSlash(rel), Reason: reason})
			blocked = true
			break
		}
	}
	if !blocked {
		err = filepath.WalkDir(start, func(filePath string, entry fs.DirEntry, walkErr error) error {
			rel, _ := filepath.Rel(root, filePath)
			rel = filepath.ToSlash(rel)
			if walkErr != nil {
				// A failed subtree cannot establish that previously known skills
				// have been removed. Report its path so catalog merging retains it.
				reason := fmt.Sprintf("cannot read repository path %q: %v", rel, walkErr)
				scanIssues = append(scanIssues, repositoryIssue{Path: rel, Reason: reason})
				skipped = append(skipped, SkippedSkill{Subpath: rel, Reason: reason, blockedSubtree: true})
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				scanIssues = append(scanIssues, repositoryIssue{Path: rel, Reason: "symlink is not supported"})
				return nil
			}
			if entry.IsDir() && ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if !entry.IsDir() && !entry.Type().IsRegular() {
				scanIssues = append(scanIssues, repositoryIssue{Path: rel, Reason: "unsupported repository file type"})
				return nil
			}
			if entry.Name() != "SKILL.md" {
				return nil
			}
			skillPaths[filepath.ToSlash(filepath.Dir(rel))] = true
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}
	// A skipped SKILL.md still identifies a skill even though no file exists
	// in the extracted tree. Preserve this identity for catalog refresh.
	for _, issue := range scanIssues {
		if filepath.Base(issue.Path) == "SKILL.md" && repositoryPathWithin(subpath, issue.Path) {
			skillPaths[filepath.ToSlash(filepath.Dir(issue.Path))] = true
		}
	}
	paths := make([]string, 0, len(skillPaths))
	for path := range skillPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	relevant := make([]bool, len(scanIssues))
	for _, path := range paths {
		reason := ""
		for i, issue := range scanIssues {
			if repositoryIssueAffectsSkill(issue, path) {
				relevant[i] = true
				if reason == "" {
					reason = fmt.Sprintf("unsupported skill content at %q: %s", issue.Path, issue.Reason)
				}
			}
		}
		if reason == "" {
			item, skillErr := readSkill(root, path, revision)
			if skillErr == nil {
				result = append(result, item)
				continue
			}
			reason = skillErr.Error()
		}
		skipped = append(skipped, SkippedSkill{Subpath: path, Reason: reason})
	}
	for i, issue := range scanIssues {
		if !relevant[i] && (repositoryPathWithin(subpath, issue.Path) || repositoryPathWithin(issue.Path, subpath)) {
			// Unrelated aliases are common in agent repositories. Keep the
			// issue for catalog retention, but only surface a link warning when
			// it blocks the explicitly selected scope. Links inside a skill
			// already explain that skill's skipped status above.
			if issue.Reason == "symlink is not supported" && !repositoryPathWithin(issue.Path, subpath) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("skipped repository entry %q: %s", issue.Path, issue.Reason))
		}
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Subpath < skipped[j].Subpath })
	sort.Strings(warnings)
	if len(result) == 0 && !allowEmpty {
		message := "no SKILL.md files were found in the selected repository directory"
		if len(skipped) > 0 {
			message = fmt.Sprintf("no usable skills were found; skipped %q: %s", skipped[0].Subpath, skipped[0].Reason)
		}
		return nil, skipped, warnings, errors.New(message)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Subpath < result[j].Subpath })
	return result, skipped, warnings, nil
}

func repositoryPathWithin(parent, path string) bool {
	// A Git tree can contain names that differ only in case even when the
	// preview filesystem cannot. Keep an unsupported case variant from
	// silently producing an incomplete candidate on those hosts.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		parent, path = strings.ToLower(parent), strings.ToLower(path)
	}
	return parent == "." || parent == path || strings.HasPrefix(path, parent+"/")
}

func repositoryIssueAffectsSkill(issue repositoryIssue, skillPath string) bool {
	if issue.SkillOnly {
		owner := filepath.ToSlash(filepath.Dir(issue.Path))
		return repositoryPathWithin(owner, skillPath) && repositoryPathWithin(skillPath, owner)
	}
	return repositoryPathsOverlap(skillPath, issue.Path)
}

func readSkill(root, subpath, revision string) (CandidateInfo, error) {
	path := filepath.Join(root, filepath.FromSlash(subpath))
	// Validate the full tree before reading metadata so a linked SKILL.md
	// cannot be followed in a caller-provided tree.
	if err := validateTree(path); err != nil {
		return CandidateInfo{}, fmt.Errorf("%s: %w", subpath, err)
	}
	info, err := os.Lstat(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return CandidateInfo{}, fmt.Errorf("%s/SKILL.md: %w", subpath, err)
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return CandidateInfo{}, errors.New("SKILL.md must be a regular file smaller than 1 MiB")
	}
	body, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return CandidateInfo{}, fmt.Errorf("%s/SKILL.md: %w", subpath, err)
	}
	name, description, err := metadata(body)
	if err != nil {
		return CandidateInfo{}, fmt.Errorf("%s/SKILL.md: %w", subpath, err)
	}
	hash, err := skillstore.HashDir(path)
	if err != nil {
		return CandidateInfo{}, fmt.Errorf("%s: %w", subpath, err)
	}
	return CandidateInfo{Name: name, Description: description, Subpath: subpath, Revision: revision, ContentHash: hash}, nil
}

func ignoredDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", ".venv", "venv", "__pycache__":
		return true
	}
	return false
}

func metadata(body []byte) (string, string, error) {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(body, []byte("\n"))
	if len(lines) == 0 || string(lines[0]) != "---" {
		return "", "", errors.New("missing YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if string(lines[i]) == "---" || string(lines[i]) == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return "", "", errors.New("YAML frontmatter is not closed")
	}
	document, err := parseFrontmatter(bytes.Join(lines[1:end], []byte("\n")))
	if err != nil {
		return "", "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return "", "", errors.New("YAML frontmatter must be a mapping")
	}
	fields := make(map[string]*yaml.Node)
	content := document.Content[0].Content
	for i := 0; i < len(content); i += 2 {
		key, value := content[i], content[i+1]
		if _, exists := fields[key.Value]; exists {
			return "", "", fmt.Errorf("duplicate frontmatter field %q", key.Value)
		}
		fields[key.Value] = value
	}
	nameNode, descriptionNode := fields["name"], fields["description"]
	if nameNode == nil || descriptionNode == nil || nameNode.Kind != yaml.ScalarNode || descriptionNode.Kind != yaml.ScalarNode || nameNode.Tag != "!!str" || descriptionNode.Tag != "!!str" {
		return "", "", errors.New("frontmatter requires string name and description fields")
	}
	name, description := strings.TrimSpace(nameNode.Value), strings.TrimSpace(descriptionNode.Value)
	if !safeSkillName.MatchString(name) || name == "." || name == ".." {
		return "", "", errors.New("skill name must be a safe directory name using letters, numbers, '.', '-' or '_'")
	}
	if description == "" {
		return "", "", errors.New("skill description cannot be empty")
	}
	return name, description, nil
}

func validateTree(root string) error {
	return filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in skill content: %s", entry.Name())
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("skill content contains an unsupported file")
		}
		return nil
	})
}
