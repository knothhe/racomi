package sources

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var repoPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var safeSkillName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var scpSource = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9_./-]+$`)
var domainLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

type sourceSpec struct {
	source   string
	ref      string
	subpath  string
	skill    string
	treeTail string
	hosted   bool
}

func parseSource(input, ref, subpath string) (sourceSpec, error) {
	s := sourceSpec{ref: strings.TrimSpace(ref)}
	if !validRef(s.ref) {
		return s, errors.New("invalid Git ref")
	}
	var err error
	s.subpath, err = cleanSubpath(subpath)
	if err != nil {
		return s, err
	}
	input = strings.TrimSpace(input)
	if input == "" || strings.HasPrefix(input, "-") || strings.ContainsAny(input, "\x00\r\n") {
		return s, errors.New("enter a Git repository or skills.sh link")
	}
	if filepath.IsAbs(input) {
		info, err := os.Lstat(input)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return s, errors.New("local Git source must be an existing directory, not a symlink")
		}
		resolved, err := filepath.EvalSymlinks(input)
		if err != nil {
			return s, errors.New("local Git source cannot be resolved")
		}
		// Canonicalize ancestor aliases such as macOS /tmp -> /private/tmp.
		// The selected root itself and every file extracted from Git must
		// still be real directories/files, never symlinks.
		s.source = resolved
		return s, nil
	}
	if scpSource.MatchString(input) {
		userHost, _, _ := strings.Cut(input, ":")
		username, host, _ := strings.Cut(userHost, "@")
		if !repoPart.MatchString(username) || strings.HasPrefix(host, "-") {
			return s, errors.New("invalid SSH repository address")
		}
		if strings.Contains(input, "..") {
			return s, errors.New("invalid SSH repository path")
		}
		s.source = input
		return s, nil
	}
	if strings.HasPrefix(input, "skills.sh/") {
		input = "https://" + input
	}
	if strings.Contains(input, "://") {
		u, err := url.Parse(input)
		if err != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
			return s, errors.New("invalid repository URL")
		}
		if u.Scheme != "https" && u.Scheme != "ssh" {
			return s, errors.New("repository URLs must use HTTPS or SSH")
		}
		if u.Scheme == "https" && u.User != nil {
			return s, errors.New("HTTPS repository URLs cannot include credentials")
		}
		if u.User != nil {
			if !repoPart.MatchString(u.User.Username()) {
				return s, errors.New("invalid SSH repository username")
			}
			if _, hasPassword := u.User.Password(); hasPassword {
				return s, errors.New("repository URLs cannot include passwords")
			}
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if u.Hostname() == "skills.sh" || u.Hostname() == "www.skills.sh" {
			if len(parts) == 3 && parts[0] == "site" && validHostedDomain(parts[1]) {
				parts = parts[1:]
			}
			if u.Scheme == "https" && u.Port() == "" && len(parts) == 2 && validHostedDomain(parts[0]) && safeSkillName.MatchString(parts[1]) {
				if s.ref != "" {
					return s, errors.New("hosted sources do not support Git refs")
				}
				s.source, s.skill, s.hosted = "https://"+strings.ToLower(parts[0]), parts[1], true
				s.subpath = parts[1]
				return s, nil
			}
			if u.Scheme != "https" || len(parts) != 3 || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) || !safeSkillName.MatchString(parts[2]) {
				return s, errors.New("skills.sh links must identify owner/repository/skill")
			}
			s.source = "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
			s.skill = parts[2]
			return s, nil
		}
		if strings.HasPrefix(u.Hostname(), "-") || strings.ContainsAny(u.Hostname(), " \t\\") {
			return s, errors.New("invalid repository hostname")
		}
		hostedPath := strings.TrimRight(u.Path, "/")
		if strings.HasSuffix(hostedPath, "/.well-known/skills/index.json") || strings.HasSuffix(hostedPath, "/.well-known/agent-skills/index.json") {
			hostedPath = strings.TrimSuffix(hostedPath, "/index.json")
		}
		if u.Scheme == "https" && u.Hostname() != "github.com" && u.Hostname() != "gitlab.com" && (hostedPath == "" || strings.HasSuffix(hostedPath, "/.well-known/skills") || strings.HasSuffix(hostedPath, "/.well-known/agent-skills")) {
			if !validHostedDomain(u.Hostname()) && net.ParseIP(u.Hostname()) == nil && u.Hostname() != "localhost" {
				return s, errors.New("invalid hosted source hostname")
			}
			if s.ref != "" {
				return s, errors.New("hosted sources do not support Git refs")
			}
			if strings.Contains(u.Path, "..") || strings.ContainsAny(u.Path, "\\\x00\r\n") || u.RawPath != "" {
				return s, errors.New("invalid hosted source URL")
			}
			u.Host = strings.ToLower(u.Host)
			u.Path = hostedPath
			s.source, s.hosted = u.String(), true
			return s, nil
		}
		if u.Hostname() == "github.com" && u.Scheme == "https" {
			if len(parts) < 2 || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) {
				return s, errors.New("GitHub URL must identify owner/repository")
			}
			s.source = "https://github.com/" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git") + ".git"
			if len(parts) > 2 {
				if len(parts) < 4 || parts[2] != "tree" {
					return s, errors.New("use a repository URL or a GitHub tree URL")
				}
				s.treeTail = strings.Join(parts[3:], "/")
				if _, err := cleanSubpath(s.treeTail); err != nil {
					return s, errors.New("invalid GitHub tree path")
				}
			}
			return s, nil
		}
		if u.Path == "" || u.Path == "/" || strings.Contains(u.Path, "..") {
			return s, errors.New("invalid Git repository path")
		}
		s.source = u.String()
		return s, nil
	}
	parts := strings.Split(input, "/")
	if len(parts) != 2 || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) {
		return s, errors.New("use owner/repository, an HTTPS/SSH Git URL, or a skills.sh link")
	}
	s.source = "https://github.com/" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git") + ".git"
	return s, nil
}

func validHostedDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasSuffix(label, "-") || !domainLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func validRef(ref string) bool {
	return len(ref) <= 256 && !strings.HasPrefix(ref, "-") && !strings.ContainsAny(ref, "\x00\r\n ~^:?*[\\") && !strings.Contains(ref, "..") && !strings.Contains(ref, "@{")
}

func cleanSubpath(p string) (string, error) {
	if p == "" || p == "." {
		return ".", nil
	}
	if strings.ContainsAny(p, "\\\x00\r\n") || strings.HasPrefix(p, "/") {
		return "", errors.New("skill subpath must be a relative repository directory")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == ".git" || part == "" {
			return "", fmt.Errorf("invalid skill subpath %q", p)
		}
	}
	return path.Clean(p), nil
}
