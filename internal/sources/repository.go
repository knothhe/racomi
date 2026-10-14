package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Allow five minutes for a snapshot download plus time for discovery/extraction.
const SourceOperationTimeout = 6 * time.Minute
const gitDownloadTimeout = 5 * time.Minute
const gitConnectionTimeout = 30 * time.Second

func (c *Client) repository(ctx context.Context, source string, refresh bool, ref string) (string, error) {
	hash := sha256.Sum256([]byte(source))
	parent := filepath.Join(c.cacheDir, "repositories")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	repo := filepath.Join(parent, hex.EncodeToString(hash[:]))
	_, statErr := os.Stat(filepath.Join(repo, "HEAD"))
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if statErr == nil && (!refresh || validRevision(ref)) {
		if _, err := resolveRevision(ctx, repo, defaultRef(ref)); err == nil {
			return repo, nil
		}
		// Another scope may have populated the cache without this ref. Historical
		// synchronization fetches the requested commit, never the newest head.
	}
	remoteRef, objectFormat, err := resolveRemoteRef(ctx, source, ref)
	if err != nil {
		return "", repositoryError(statErr != nil, err)
	}
	work := repo
	if statErr != nil {
		work, err = os.MkdirTemp(parent, ".clone-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(work)
		if _, err = gitCommand(ctx, "", "init", "--bare", "--object-format="+objectFormat, "--", work); err != nil {
			return "", err
		}
		if _, err = gitCommand(ctx, work, "remote", "add", "origin", source); err != nil {
			return "", err
		}
	} else if previous, err := resolveRevision(ctx, work, defaultRef(ref)); err == nil {
		if err := retainRevision(ctx, work, previous); err != nil {
			return "", err
		}
	}
	if remoteRef == "" {
		// An abbreviated object ID cannot be fetched by Git's wire protocol.
		// Only this explicit historical request needs history to disambiguate it.
		if err = fetchHistory(ctx, work); err == nil {
			_, err = resolveRevision(ctx, work, ref)
		}
	} else {
		destination := remoteRef
		if validRevision(remoteRef) {
			destination = "refs/racomi/revisions/" + remoteRef
		}
		_, err = gitDownload(ctx, work, "fetch", "--depth=1", "--no-tags", "--no-auto-gc", "--force", "--", "origin", "+"+remoteRef+":"+destination)
		if err != nil && ctx.Err() == nil && validRevision(remoteRef) && rejectsObjectFetch(err) {
			// Some servers forbid fetching an unadvertised historical object ID.
			// Preserve the old full-clone compatibility only for this explicit
			// historical request, and still require that exact commit to exist.
			if err = fetchHistory(ctx, work); err == nil {
				_, err = resolveRevision(ctx, work, remoteRef)
			}
		}
		if err == nil && remoteRef == "refs/heads/"+ref {
			// A deleted tag in an older cache must not shadow the branch just
			// selected from upstream. Keep its commit before removing the alias.
			tag := "refs/tags/" + ref
			if previous, tagErr := resolveRevision(ctx, work, tag); tagErr == nil {
				if err = retainRevision(ctx, work, previous); err == nil {
					_, err = gitCommand(ctx, work, "update-ref", "-d", tag)
				}
			}
		}
		if err == nil && (ref == "" || ref == "HEAD") {
			if strings.HasPrefix(remoteRef, "refs/heads/") {
				_, err = gitCommand(ctx, work, "symbolic-ref", "HEAD", remoteRef)
			} else {
				_, err = gitCommand(ctx, work, "update-ref", "--no-deref", "HEAD", remoteRef)
			}
		}
	}
	if err != nil {
		return "", repositoryError(statErr != nil, err)
	}
	revision, err := resolveRevision(ctx, work, defaultRef(ref))
	if err != nil {
		return "", err
	}
	if err = retainRevision(ctx, work, revision); err != nil {
		return "", err
	}
	if statErr != nil {
		if err := os.Rename(work, repo); err != nil {
			return "", err
		}
	}
	return repo, nil
}

func rejectsObjectFetch(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not our ref") || strings.Contains(message, "unadvertised object") || strings.Contains(message, "couldn't find remote ref") || strings.Contains(message, "does not allow request")
}

func defaultRef(ref string) string {
	if ref == "" {
		return "HEAD"
	}
	return ref
}

func repositoryError(initial bool, err error) error {
	if initial {
		return fmt.Errorf("cannot clone repository: %w", err)
	}
	return fmt.Errorf("cannot refresh repository: %w", err)
}

func retainRevision(ctx context.Context, repo, revision string) error {
	_, err := gitCommand(ctx, repo, "update-ref", "refs/racomi/revisions/"+revision, revision)
	return err
}

func gitObjectFormat(revision string) string {
	if len(revision) == 64 {
		return "sha256"
	}
	return "sha1"
}

func remoteObjectFormat(ctx context.Context, source string) (string, error) {
	out, err := gitConnection(ctx, "", "ls-remote", "--", source)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && validRevision(fields[0]) {
			return gitObjectFormat(fields[0]), nil
		}
	}
	return "", errors.New("repository has no advertised Git revision")
}

func resolveRemoteRef(ctx context.Context, source, ref string) (string, string, error) {
	if validRevision(ref) {
		if len(ref) == 40 {
			// Forty hex digits can also abbreviate a SHA-256 commit. Fetch
			// history in that case instead of initializing a SHA-1 cache.
			objectFormat, err := remoteObjectFormat(ctx, source)
			if err != nil {
				return "", "", err
			}
			if objectFormat == "sha256" {
				return "", objectFormat, nil
			}
		}
		return ref, gitObjectFormat(ref), nil
	}
	if ref == "" || ref == "HEAD" {
		out, err := gitConnection(ctx, "", "ls-remote", "--symref", "--", source, "HEAD")
		if err != nil {
			return "", "", err
		}
		objectFormat := "sha1"
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == "HEAD" && validRevision(fields[0]) {
				objectFormat = gitObjectFormat(fields[0])
				break
			}
		}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" && strings.HasPrefix(fields[1], "refs/heads/") && validRef(fields[1]) {
				return fields[1], objectFormat, nil
			}
		}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == "HEAD" && validRevision(fields[0]) {
				return fields[0], objectFormat, nil
			}
		}
		return "", "", errors.New("repository has no default Git ref")
	}
	// Match Git's namespace and tag-before-branch precedence, including
	// heads/main and tags/v1 abbreviations persisted by older versions.
	patterns := []string{ref}
	if !strings.HasPrefix(ref, "refs/") {
		patterns = []string{"refs/" + ref, "refs/tags/" + ref, "refs/heads/" + ref}
	}
	args := append([]string{"ls-remote", "--refs", "--", source}, patterns...)
	out, err := gitConnection(ctx, "", args...)
	if err != nil {
		return "", "", err
	}
	for _, pattern := range patterns {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == pattern && validRevision(fields[0]) {
				return pattern, gitObjectFormat(fields[0]), nil
			}
		}
	}
	if len(ref) >= 4 && len(ref) < 64 && strings.Trim(ref, "0123456789abcdefABCDEF") == "" {
		// Abbreviations require history, but the cache must first use the
		// advertised object format even though no remote ref matched.
		objectFormat, err := remoteObjectFormat(ctx, source)
		if err != nil {
			return "", "", err
		}
		if len(ref) < 40 || objectFormat == "sha256" {
			return "", objectFormat, nil
		}
	}
	return "", "", fmt.Errorf("cannot resolve Git ref %q: remote ref does not exist", ref)
}

func resolveTreeSource(ctx context.Context, spec sourceSpec) (sourceSpec, error) {
	parts := strings.Split(spec.treeTail, "/")
	patterns := []string{}
	for n := len(parts); n >= 1; n-- {
		ref := strings.Join(parts[:n], "/")
		if validRef(ref) && (spec.ref == "" || spec.ref == ref) {
			patterns = append(patterns, "refs/heads/"+ref, "refs/tags/"+ref)
		}
	}
	if len(patterns) != 0 {
		out, err := gitConnection(ctx, "", append([]string{"ls-remote", "--refs", "--", spec.source}, patterns...)...)
		if err != nil {
			return spec, err
		}
		refs := map[string]bool{}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && validRevision(fields[0]) {
				refs[fields[1]] = true
			}
		}
		for n := len(parts); n >= 1; n-- {
			ref := strings.Join(parts[:n], "/")
			if (spec.ref == "" || spec.ref == ref) && (refs["refs/heads/"+ref] || refs["refs/tags/"+ref] || (n == 1 && validRevision(ref))) {
				spec.ref, spec.treeTail = ref, ""
				if spec.subpath == "." && n < len(parts) {
					spec.subpath = strings.Join(parts[n:], "/")
				}
				return spec, nil
			}
		}
	}
	return spec, errors.New("GitHub tree URL does not identify an available branch, tag, or commit")
}

func fetchHistory(ctx context.Context, repo string) error {
	args := []string{"fetch", "--no-auto-gc", "--force", "--tags"}
	if _, err := os.Stat(filepath.Join(repo, "shallow")); err == nil {
		args = append(args, "--unshallow")
	}
	_, err := gitDownload(ctx, repo, append(args, "--", "origin", "+refs/heads/*:refs/heads/*")...)
	return err
}

// Ancestry is needed only when merging a discovery preview into an existing
// catalog. Deepen that commit on demand; ordinary refresh/sync stays depth one.
func revisionAdvances(ctx context.Context, repo, old, current string) bool {
	for depth := 32; ; depth *= 4 {
		if _, err := gitCommand(ctx, repo, "merge-base", "--is-ancestor", old, current); err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		if _, err := os.Stat(filepath.Join(repo, "shallow")); err != nil {
			return false
		}
		before, err := gitCommand(ctx, repo, "rev-list", "--count", current)
		if err != nil {
			return false
		}
		_, err = gitDownload(ctx, repo, "fetch", fmt.Sprintf("--deepen=%d", depth), "--no-tags", "--no-auto-gc", "--force", "--", "origin", "+"+current+":refs/racomi/revisions/"+current)
		if err != nil {
			return false
		}
		after, err := gitCommand(ctx, repo, "rev-list", "--count", current)
		if err != nil || before == after {
			return false
		}
	}
}
