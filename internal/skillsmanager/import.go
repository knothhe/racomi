// Package skillsmanager reads a legacy skills-manager workspace without changing it.
package skillsmanager

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	skillstore "racomi/internal/skills"

	_ "github.com/mattn/go-sqlite3"
)

type Bundle struct {
	DatabasePath string
	Candidates   []skillstore.Candidate
	Presets      []skillstore.Preset
	Sources      []skillstore.Source
	Warnings     []string
}

var skillName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var githubRepo = regexp.MustCompile(`^(?:https://github\.com/|ssh://git@github\.com/|git@github\.com:)([A-Za-z0-9._-]+/[A-Za-z0-9._-]+?)(?:\.git)?/?$`)

func stableID(kind, value string) string {
	hash := sha256.Sum256([]byte(value))
	return "sm-" + kind + "-" + hex.EncodeToString(hash[:12])
}

func repositoryKey(repository string) string {
	if parts := githubRepo.FindStringSubmatch(repository); parts != nil {
		return "github.com/" + strings.ToLower(parts[1])
	}
	return repository
}

func canonicalRepository(repository string) string {
	if parts := githubRepo.FindStringSubmatch(repository); parts != nil {
		return "https://github.com/" + parts[1] + ".git"
	}
	return repository
}

func timestamp(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

// Read uses one read-only SQLite transaction, including any committed WAL data.
// Content is restricted to the skills/ directory alongside the chosen database.
// The returned hashes pin the preview; MergeImport rejects subsequent file changes.
func Read(filename string, existing skillstore.State) (*Bundle, error) {
	if !filepath.IsAbs(filename) {
		return nil, errors.New("请选择 skills-manager 数据库的绝对路径")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return nil, errors.New("数据库必须为不超过 128 MiB 的普通文件")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	filename = filepath.Join(parent, filepath.Base(filename))
	uriPath := filepath.ToSlash(filename)
	if filepath.VolumeName(filename) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{"mode": {"ro"}, "_query_only": {"1"}, "_busy_timeout": {"3000"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("无法只读打开 skills-manager 数据库：%w", err)
	}
	defer tx.Rollback()
	for _, table := range []string{"skills", "scenarios", "scenario_skills"} {
		var kind string
		if err = tx.QueryRow("SELECT type FROM sqlite_master WHERE name = ?", table).Scan(&kind); err != nil || kind != "table" {
			return nil, fmt.Errorf("不是支持的 skills-manager 数据库：缺少 %s 表", table)
		}
	}
	bundle := &Bundle{DatabasePath: filename, Candidates: []skillstore.Candidate{}, Presets: []skillstore.Preset{}, Sources: []skillstore.Source{}, Warnings: []string{}}
	rows, err := tx.Query(`SELECT id, name, COALESCE(description,''), source_type,
		COALESCE(source_ref,''), COALESCE(source_ref_resolved,''), COALESCE(source_subpath,''),
		COALESCE(source_branch,''), COALESCE(source_revision,''), central_path,
		COALESCE(created_at,0), COALESCE(updated_at,0) FROM skills ORDER BY name, id LIMIT 10001`)
	if err != nil {
		return nil, fmt.Errorf("skills-manager 技能表不兼容：%w", err)
	}
	skillIDs := map[string]string{}
	sourceIndexes := map[string]int{}
	var totalBytes int64
	var totalEntries int
	for rows.Next() {
		var legacyID, name, description, sourceType, sourceRef, resolved, subpath, branch, revision, central string
		var created, updated int64
		if err = rows.Scan(&legacyID, &name, &description, &sourceType, &sourceRef, &resolved, &subpath, &branch, &revision, &central, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		if len(bundle.Candidates) >= 10000 || legacyID == "" || !skillName.MatchString(name) {
			rows.Close()
			return nil, fmt.Errorf("技能数量超过上限或名称无效：%q", name)
		}
		if _, exists := skillIDs[legacyID]; exists {
			rows.Close()
			return nil, errors.New("旧库中有重复技能 ID")
		}
		// Rebase old absolute paths when a workspace is moved to another machine.
		base := filepath.Base(central)
		if central == "" || base == "." || base == ".." || base == string(filepath.Separator) {
			rows.Close()
			return nil, fmt.Errorf("技能 %s 的内容目录无效", name)
		}
		content := filepath.Join(parent, "skills", base)
		if err = inspectContent(content, &totalBytes, &totalEntries); err != nil {
			rows.Close()
			return nil, fmt.Errorf("技能 %s：%w（数据库旁须保留 skills 文件夹）", name, err)
		}
		hash, hashErr := skillstore.HashDir(content)
		if hashErr != nil {
			rows.Close()
			return nil, hashErr
		}
		skill := skillstore.Skill{ID: stableID("skill", legacyID), Name: name, Description: description, ContentHash: hash, InstalledAt: timestamp(created), UpdatedAt: timestamp(updated)}
		if sourceType == "git" || sourceType == "skillssh" {
			repository := resolved
			if repository == "" && sourceType == "git" {
				repository = sourceRef
			}
			if repository != "" && validRepository(repository) && validSubpath(subpath) && !strings.ContainsAny(branch+revision, "\x00\r\n") {
				skill.Source, skill.Ref, skill.Subpath, skill.Revision = canonicalRepository(repository), branch, subpath, revision
				if skill.Subpath == "" {
					skill.Subpath = "."
				}
			} else {
				bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("%s 的 Git 来源不完整，技能文件会作为本地技能保留。", name))
			}
		}
		// Same content and origin can reuse a pre-existing installation, including
		// its explicit branch and richer revision metadata. No Git calls are needed.
		matches := []skillstore.Skill{}
		for _, current := range existing.Skills {
			if skill.Name != current.Name || skill.ContentHash != current.ContentHash {
				continue
			}
			if skill.Source == "" && current.Source == "" || skill.Source != "" && repositoryKey(skill.Source) == repositoryKey(current.Source) && skill.Subpath == current.Subpath && (skill.Ref == "" || skill.Ref == current.Ref) {
				matches = append(matches, current)
			}
		}
		if len(matches) == 1 {
			skill = matches[0]
		}
		if skill.Source != "" {
			source, trackable := sourceForSkill(skill, existing.Sources)
			index, found := sourceIndexes[source.ID]
			if found {
				source = bundle.Sources[index]
			}
			// Reused installations keep their existing origin spelling. Changing
			// it to an equivalent source URL would make MergeImport reject them.
			if len(matches) == 1 && source.Repository != skill.Source {
				trackable = false
			}
			if trackable {
				if !found {
					index = len(bundle.Sources)
					sourceIndexes[source.ID] = index
					source.Catalog = slices.Clone(source.Catalog)
					bundle.Sources = append(bundle.Sources, source)
				}
				registered := &bundle.Sources[index]
				skill.Source, skill.Ref, skill.SourceID = registered.Repository, registered.Ref, registered.ID
				if !slices.ContainsFunc(registered.Catalog, func(entry skillstore.SourceSkill) bool { return entry.Subpath == skill.Subpath }) {
					registered.Catalog = append(registered.Catalog, skillstore.SourceSkill{Name: skill.Name, Description: skill.Description, Subpath: skill.Subpath, Revision: skill.Revision, ContentHash: skill.ContentHash})
				}
			} else {
				bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("%s 的 Git 来源与现有来源范围重叠，保留技能和来源信息，但不新增来源关联。", name))
			}
		}
		skillIDs[legacyID] = skill.ID
		bundle.Candidates = append(bundle.Candidates, skillstore.Candidate{Skill: skill, Path: content})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query("SELECT id, name, COALESCE(description,'') FROM scenarios ORDER BY sort_order, name, id LIMIT 10001")
	if err != nil {
		return nil, fmt.Errorf("skills-manager 场景表不兼容：%w", err)
	}
	presetIndexes := map[string]int{}
	for rows.Next() {
		var legacyID, name, description string
		if err = rows.Scan(&legacyID, &name, &description); err != nil {
			rows.Close()
			return nil, err
		}
		if len(bundle.Presets) >= 10000 || legacyID == "" || strings.TrimSpace(name) == "" {
			rows.Close()
			return nil, errors.New("旧库场景 ID、名称或数量无效")
		}
		if _, exists := presetIndexes[legacyID]; exists {
			rows.Close()
			return nil, errors.New("旧库中有重复场景 ID")
		}
		presetIndexes[legacyID] = len(bundle.Presets)
		bundle.Presets = append(bundle.Presets, skillstore.Preset{ID: stableID("preset", legacyID), Name: name, Description: description, SkillIDs: []string{}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query("SELECT scenario_id, skill_id FROM scenario_skills ORDER BY scenario_id, sort_order, added_at, skill_id LIMIT 100001")
	if err != nil {
		return nil, err
	}
	memberCount := 0
	for rows.Next() {
		var presetID, skillID string
		if err = rows.Scan(&presetID, &skillID); err != nil {
			rows.Close()
			return nil, err
		}
		memberCount++
		index, presetExists := presetIndexes[presetID]
		mappedSkill, skillExists := skillIDs[skillID]
		if memberCount > 100000 || !presetExists || !skillExists {
			rows.Close()
			return nil, errors.New("旧库场景成员过多或引用了不存在的场景 / 技能")
		}
		if !slices.Contains(bundle.Presets[index].SkillIDs, mappedSkill) {
			bundle.Presets[index].SkillIDs = append(bundle.Presets[index].SkillIDs, mappedSkill)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range bundle.Presets {
		preset := &bundle.Presets[i]
		for _, current := range existing.Presets {
			if current.ID != preset.ID && current.Name == preset.Name && !sameMembers(current.SkillIDs, preset.SkillIDs) {
				preset.Name += " (skills-manager)"
				bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("同名场景的成员不同，导入为 %s，保留现有组合。", preset.Name))
				break
			}
		}
	}
	for _, omitted := range []struct{ table, label string }{
		{"projects", "项目记录"}, {"skill_targets", "工具部署记录（含软链接）"},
		{"scenario_skill_tools", "场景工具开关"}, {"active_scenario", "当前场景选择"},
		{"settings", "旧应用设置"}, {"skill_tags", "技能标签"},
		{"discovered_skills", "发现记录"}, {"audit_log", "操作历史"}, {"pending_conflicts", "待处理冲突"},
	} {
		var exists int
		if err = tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", omitted.table).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM " + omitted.table).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("%d 条%s不导入，请按需在 racomi 重新配置。", count, omitted.label))
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return bundle, nil
}

func sourceContains(scope, subpath string) bool {
	return scope == "." || scope == subpath || strings.HasPrefix(subpath, scope+"/")
}

// Reuse a containing registration, or choose the widest new scope disjoint
// from existing registrations. This keeps the result independent of row order.
func sourceForSkill(skill skillstore.Skill, existing []skillstore.Source) (skillstore.Source, bool) {
	key := repositoryKey(skill.Source) + "\x00" + skill.Ref
	related := []skillstore.Source{}
	for _, source := range existing {
		if source.ID == skill.SourceID {
			return source, true
		}
		if repositoryKey(source.Repository)+"\x00"+source.Ref == key {
			related = append(related, source)
		}
	}
	for _, source := range related {
		if sourceContains(source.Subpath, skill.Subpath) {
			return source, true
		}
	}
	overlaps := func(scope string) bool {
		return slices.ContainsFunc(related, func(source skillstore.Source) bool {
			return sourceContains(scope, source.Subpath)
		})
	}
	// A skill above a registered scope cannot get its own registration without
	// overlapping it. Its Git origin remains available as a detached skill.
	scope := skill.Subpath
	if overlaps(scope) {
		return skillstore.Source{}, false
	}
	for scope != "." {
		parent := path.Dir(scope)
		if overlaps(parent) {
			break
		}
		scope = parent
	}
	idKey := key
	if scope != "." {
		idKey += "\x00" + scope
	}
	return skillstore.Source{ID: stableID("source", idKey), Name: strings.TrimSuffix(path.Base(skill.Source), ".git"), Repository: skill.Source, Ref: skill.Ref, Subpath: scope, Mode: skillstore.SourceModeSelected, Catalog: []skillstore.SourceSkill{}}, true
}

func sameMembers(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func validRepository(repository string) bool {
	if strings.ContainsAny(repository, "\x00\r\n") || strings.HasPrefix(repository, "-") {
		return false
	}
	if githubRepo.MatchString(repository) {
		return true
	}
	u, err := url.Parse(repository)
	return err == nil && (u.Scheme == "https" || u.Scheme == "ssh") && u.Hostname() != "" && u.Path != "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func validSubpath(subpath string) bool {
	return subpath == "" || subpath != ".." && !strings.HasPrefix(subpath, "../") && !path.IsAbs(subpath) && !strings.ContainsAny(subpath, "\\\x00\r\n") && path.Clean(subpath) == subpath
}

func inspectContent(directory string, totalBytes *int64, totalEntries *int) error {
	info, err := os.Lstat(filepath.Join(directory, "SKILL.md"))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("缺少普通文件 SKILL.md")
	}
	return filepath.WalkDir(directory, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		*totalEntries++
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("技能含符号链接或特殊文件")
		}
		*totalBytes += info.Size()
		if *totalEntries > 20000 || *totalBytes > 512<<20 {
			return errors.New("技能内容超过 512 MiB / 20,000 条目上限")
		}
		return nil
	})
}
