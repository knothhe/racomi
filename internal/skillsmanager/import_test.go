package skillsmanager

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	skillstore "racomi/internal/skills"
)

type legacyFixture struct {
	t        *testing.T
	db       *sql.DB
	root     string
	filename string
}

type legacySkill struct {
	id, name, description, sourceType string
	source, resolved, subpath, branch string
	revision, centralPath, storedHash string
	createdAt, updatedAt              int64
}

func newLegacyFixture(t *testing.T) *legacyFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &legacyFixture{t: t, root: root, filename: filepath.Join(root, "skills-manager.db")}
	f.db, err = sql.Open("sqlite3", f.filename)
	if err != nil {
		t.Fatal(err)
	}
	f.db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = f.db.Close() })
	f.exec(`PRAGMA journal_mode=WAL`)
	f.exec(`PRAGMA wal_autocheckpoint=0`)
	f.exec(`CREATE TABLE skills (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT,
		source_type TEXT NOT NULL, source_ref TEXT, source_ref_resolved TEXT,
		source_subpath TEXT, source_branch TEXT, source_revision TEXT, remote_revision TEXT,
		central_path TEXT NOT NULL UNIQUE, content_hash TEXT, enabled INTEGER DEFAULT 1,
		created_at INTEGER, updated_at INTEGER, status TEXT DEFAULT 'ok',
		update_status TEXT DEFAULT 'unknown', last_checked_at INTEGER, last_check_error TEXT
	)`)
	f.exec(`CREATE TABLE scenarios (
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT, icon TEXT,
		sort_order INTEGER DEFAULT 0, created_at INTEGER, updated_at INTEGER
	)`)
	f.exec(`CREATE TABLE scenario_skills (
		scenario_id TEXT NOT NULL, skill_id TEXT NOT NULL, added_at INTEGER,
		sort_order INTEGER DEFAULT 0, PRIMARY KEY(scenario_id, skill_id)
	)`)
	if err = os.Mkdir(filepath.Join(root, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *legacyFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		f.t.Fatalf("fixture SQL: %v", err)
	}
}

func (f *legacyFixture) content(name, body string) string {
	f.t.Helper()
	dir := filepath.Join(f.root, "skills", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: file description\n---\n"+body+"\n"), 0644); err != nil {
		f.t.Fatal(err)
	}
	return dir
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (f *legacyFixture) skill(s legacySkill) {
	f.t.Helper()
	if s.sourceType == "" {
		s.sourceType = "import"
	}
	if s.centralPath == "" {
		s.centralPath = filepath.Join(f.root, "skills", s.name)
	}
	f.exec(`INSERT INTO skills (
		id, name, description, source_type, source_ref, source_ref_resolved,
		source_subpath, source_branch, source_revision, central_path, content_hash,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.id, s.name, nullString(s.description), s.sourceType, nullString(s.source),
		nullString(s.resolved), nullString(s.subpath), nullString(s.branch),
		nullString(s.revision), s.centralPath, nullString(s.storedHash), s.createdAt, s.updatedAt)
}

func (f *legacyFixture) scenario(id, name, description string, members ...string) {
	f.t.Helper()
	f.exec(`INSERT INTO scenarios(id, name, description, sort_order) VALUES (?, ?, ?, 0)`, id, name, nullString(description))
	for i, member := range members {
		f.exec(`INSERT INTO scenario_skills(scenario_id, skill_id, added_at, sort_order) VALUES (?, ?, ?, ?)`, id, member, 1000+i, i)
	}
}

func readLegacy(t *testing.T, f *legacyFixture, existing skillstore.State) *Bundle {
	t.Helper()
	bundle, err := Read(f.filename, existing)
	if err != nil {
		t.Fatal(err)
	}
	if bundle == nil {
		t.Fatal("Read returned a nil bundle")
	}
	return bundle
}

func candidateNamed(t *testing.T, bundle *Bundle, name string) skillstore.Candidate {
	t.Helper()
	for _, candidate := range bundle.Candidates {
		if candidate.Skill.Name == name {
			return candidate
		}
	}
	t.Fatalf("missing candidate %q: %+v", name, bundle.Candidates)
	return skillstore.Candidate{}
}

func presetNamed(t *testing.T, bundle *Bundle, name string) skillstore.Preset {
	t.Helper()
	for _, preset := range bundle.Presets {
		if preset.Name == name {
			return preset
		}
	}
	t.Fatalf("missing preset %q: %+v", name, bundle.Presets)
	return skillstore.Preset{}
}

func assertLegacyTime(t *testing.T, actual string, millis int64) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, actual)
	if err != nil || !parsed.Equal(time.UnixMilli(millis)) {
		t.Fatalf("timestamp %q does not preserve Unix milliseconds %d: %v", actual, millis, err)
	}
}

func TestReadMapsLegacyContentAndScenarios(t *testing.T) {
	f := newLegacyFixture(t)
	alphaPath := f.content("alpha", "alpha content")
	f.content("beta", "beta content")
	f.content("local", "local content")
	if err := os.Mkdir(filepath.Join(alphaPath, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alphaPath, "scripts", "run.sh"), []byte("#!/bin/sh\ntrue\n"), 0755); err != nil {
		t.Fatal(err)
	}
	const created, updated = int64(1788350907638), int64(1791677203003)
	const revision = "9ce4002ed592a98f3dcb14182c2dd47a95cdd51f"
	f.skill(legacySkill{id: "legacy-alpha", name: "alpha", description: "Database description", sourceType: "skillssh", source: "team/repo/alpha", resolved: "https://github.com/team/repo.git", subpath: "skills/alpha", branch: "main", revision: revision, storedHash: strings.Repeat("f", 64), createdAt: created, updatedAt: updated})
	f.skill(legacySkill{id: "legacy-beta", name: "beta", sourceType: "git", source: "https://github.com/team/repo.git", subpath: "skills/beta", branch: "main", revision: revision, createdAt: created, updatedAt: updated})
	f.skill(legacySkill{id: "legacy-local", name: "local", createdAt: created, updatedAt: updated})
	f.scenario("legacy-daily", "Daily", "Daily work", "legacy-beta", "legacy-alpha", "legacy-local")
	f.scenario("legacy-empty", "Empty", "No members")

	bundle := readLegacy(t, f, skillstore.State{})
	if bundle.DatabasePath != f.filename || len(bundle.Candidates) != 3 || len(bundle.Presets) != 2 || len(bundle.Sources) != 1 {
		t.Fatalf("incorrect imported counts or path: %+v", bundle)
	}
	alpha := candidateNamed(t, bundle, "alpha")
	wantHash, err := skillstore.HashDir(alphaPath)
	if err != nil {
		t.Fatal(err)
	}
	if alpha.Skill.Description != "Database description" || alpha.Skill.ContentHash != wantHash || alpha.Skill.ContentHash == strings.Repeat("f", 64) || alpha.Path != alphaPath {
		t.Fatalf("content or database description was not mapped: %+v", alpha)
	}
	if alpha.Skill.Source != "https://github.com/team/repo.git" || alpha.Skill.Ref != "main" || alpha.Skill.Subpath != "skills/alpha" || alpha.Skill.Revision != revision {
		t.Fatalf("source metadata was not preserved: %+v", alpha.Skill)
	}
	assertLegacyTime(t, alpha.Skill.InstalledAt, created)
	assertLegacyTime(t, alpha.Skill.UpdatedAt, updated)
	local := candidateNamed(t, bundle, "local").Skill
	if local.Source != "" || local.SourceID != "" || local.Ref != "" || local.Subpath != "" {
		t.Fatalf("local import acquired invented source metadata: %+v", local)
	}
	source := bundle.Sources[0]
	if source.ID == "" || alpha.Skill.SourceID != source.ID || source.Repository != alpha.Skill.Source || source.Ref != "main" || source.Subpath != "." || source.Mode != skillstore.SourceModeSelected || len(source.Catalog) != 2 {
		t.Fatalf("repository grouping or catalog is incorrect: %+v", source)
	}
	for _, entry := range source.Catalog {
		candidate := candidateNamed(t, bundle, entry.Name)
		if entry.Subpath != candidate.Skill.Subpath || entry.Description != candidate.Skill.Description || entry.ContentHash != candidate.Skill.ContentHash || entry.Revision != candidate.Skill.Revision {
			t.Fatalf("catalog diverged from imported content: %+v", entry)
		}
	}
	daily := presetNamed(t, bundle, "Daily")
	wantMembers := []string{candidateNamed(t, bundle, "beta").Skill.ID, alpha.Skill.ID, local.ID}
	if daily.ID == "" || daily.Description != "Daily work" || !reflect.DeepEqual(daily.SkillIDs, wantMembers) {
		t.Fatalf("scenario membership or order was not preserved: %+v", daily)
	}
	if len(presetNamed(t, bundle, "Empty").SkillIDs) != 0 {
		t.Fatal("empty scenario gained members")
	}

	again := readLegacy(t, f, skillstore.State{})
	if !reflect.DeepEqual(bundle.Candidates, again.Candidates) || !reflect.DeepEqual(bundle.Presets, again.Presets) || !reflect.DeepEqual(bundle.Sources, again.Sources) {
		t.Fatal("unchanged legacy records did not produce stable identities and metadata")
	}
}

func TestReadIncludesUncheckpointedWALRecords(t *testing.T) {
	f := newLegacyFixture(t)
	f.exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	f.content("wal-only", "committed in WAL")
	f.skill(legacySkill{id: "legacy-wal", name: "wal-only"})
	f.scenario("scenario-wal", "WAL", "", "legacy-wal")
	info, err := os.Stat(f.filename + "-wal")
	if err != nil || info.Size() == 0 {
		t.Fatalf("fixture did not leave committed WAL data: %v", err)
	}
	bundle := readLegacy(t, f, skillstore.State{})
	if len(bundle.Candidates) != 1 || len(bundle.Presets) != 1 || candidateNamed(t, bundle, "wal-only").Skill.ID == "" {
		t.Fatalf("committed WAL records were omitted: %+v", bundle)
	}
}

func TestReadReusesExistingGitHubSkillWithDefaultLegacyBranch(t *testing.T) {
	f := newLegacyFixture(t)
	dir := f.content("alpha", "same existing content")
	f.skill(legacySkill{id: "legacy-alpha", name: "alpha", description: "Older description", sourceType: "git", source: "https://github.com/Team/Repo", subpath: "skills/alpha"})
	f.scenario("legacy-team", "Team", "", "legacy-alpha")
	hash, err := skillstore.HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	existingSkill := skillstore.Skill{ID: "skill-existing", Name: "alpha", Description: "Existing description", Source: "git@github.com:Team/Repo.git", Ref: "main", Subpath: "skills/alpha", Revision: strings.Repeat("a", 40), ContentHash: hash, InstalledAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-01T00:00:00Z", SourceID: "source-existing", UpstreamRemoved: true}
	existingSource := skillstore.Source{ID: existingSkill.SourceID, Name: "Existing repository", Repository: existingSkill.Source, Ref: "main", Subpath: ".", Mode: skillstore.SourceModeAll, Revision: existingSkill.Revision, Catalog: []skillstore.SourceSkill{{Name: "alpha", Description: existingSkill.Description, Subpath: "skills/alpha", Revision: existingSkill.Revision, ContentHash: hash}}}
	existing := skillstore.State{Skills: []skillstore.Skill{existingSkill}, Sources: []skillstore.Source{existingSource}}
	bundle := readLegacy(t, f, existing)
	for _, candidate := range bundle.Candidates {
		if candidate.Skill.Name == "alpha" && !reflect.DeepEqual(candidate.Skill, existingSkill) {
			t.Fatalf("duplicate import replaced complete existing metadata: %+v", candidate.Skill)
		}
		if candidate.Skill.Name == "alpha" && candidate.Skill.ID != existingSkill.ID {
			t.Fatal("same repository path and content created a duplicate skill")
		}
	}
	if members := presetNamed(t, bundle, "Team").SkillIDs; !reflect.DeepEqual(members, []string{existingSkill.ID}) {
		t.Fatalf("scenario did not reuse the existing skill ID: %v", members)
	}
	for _, source := range bundle.Sources {
		if source.ID != existingSource.ID || source.Repository != existingSource.Repository || source.Ref != existingSource.Ref || source.Mode != existingSource.Mode {
			t.Fatalf("same GitHub repository created or replaced an existing source: %+v", source)
		}
	}
}

func migrationStore(t *testing.T) *skillstore.Store {
	t.Helper()
	store, err := skillstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func installScopedLegacySkill(t *testing.T, f *legacyFixture, store *skillstore.Store, name, subpath, scope string) (skillstore.Skill, skillstore.Source) {
	t.Helper()
	installed, err := store.Install([]skillstore.Candidate{{Skill: skillstore.Skill{
		ID: "existing-" + name, Name: name, Description: "Existing " + name,
		Source: "git@github.com:Team/Repo.git", Ref: "main", Subpath: subpath,
		Revision: strings.Repeat("a", 40),
	}, Path: filepath.Join(f.root, "skills", name)}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.SaveSource(skillstore.Source{ID: "source-" + name, Name: "Existing " + name + " source", Repository: installed[0].Source, Ref: "main", Subpath: scope, Mode: skillstore.SourceModeAll})
	if err != nil {
		t.Fatal(err)
	}
	source.Revision, source.RefreshedAt = installed[0].Revision, "2026-10-01T00:00:00Z"
	source.Catalog = []skillstore.SourceSkill{{Name: name, Description: installed[0].Description, Subpath: subpath, Revision: installed[0].Revision, ContentHash: installed[0].ContentHash}}
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	installed[0].SourceID = source.ID
	return installed[0], source
}

func TestReadMergesScopedSourcesRegardlessOfSkillOrder(t *testing.T) {
	for _, test := range []struct{ name, existing, added string }{
		{"existing first", "alpha", "beta"},
		{"new first", "zulu", "alpha"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLegacyFixture(t)
			for _, name := range []string{test.existing, test.added} {
				f.content(name, name+" content")
				f.skill(legacySkill{id: "legacy-" + name, name: name, sourceType: "git", source: "https://github.com/Team/Repo.git", branch: "main", subpath: "skills/" + name})
			}
			f.scenario("legacy-team", "Team", "", "legacy-"+test.existing, "legacy-"+test.added)
			store := migrationStore(t)
			originalSkill, originalSource := installScopedLegacySkill(t, f, store, test.existing, "skills/"+test.existing, "skills/"+test.existing)
			before, err := store.State()
			if err != nil {
				t.Fatal(err)
			}
			bundle := readLegacy(t, f, before)
			if len(bundle.Sources) != 2 || !reflect.DeepEqual(candidateNamed(t, bundle, test.existing).Skill, originalSkill) {
				t.Fatalf("scoped import changed an existing installation or grouped unrelated paths: %+v", bundle)
			}
			added := candidateNamed(t, bundle, test.added).Skill
			for _, source := range bundle.Sources {
				if source.ID == originalSource.ID && !reflect.DeepEqual(source, originalSource) {
					t.Fatalf("Read changed existing source metadata: %+v", source)
				}
				if source.ID == added.SourceID && source.Subpath != added.Subpath {
					t.Fatalf("new source overlaps existing scope: %+v", source)
				}
			}
			result, err := store.MergeImport(bundle.Candidates, bundle.Presets, bundle.Sources)
			if err != nil {
				t.Fatal(err)
			}
			if result.AddedSkills != 1 || result.SkippedSkills != 1 || result.AddedSources != 1 || result.SkippedSources != 1 || result.AddedPresets != 1 {
				t.Fatalf("unexpected merge counts: %+v", result)
			}
			after, err := store.State()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.Sources[0], originalSource) || !reflect.DeepEqual(after.Skills[0], originalSkill) || !reflect.DeepEqual(before.Sources[0], originalSource) {
				t.Fatal("migration modified existing metadata or the input state")
			}
			if members := after.Presets[0].SkillIDs; !reflect.DeepEqual(members, []string{originalSkill.ID, added.ID}) {
				t.Fatalf("scenario references lost installed identities: %v", members)
			}
			snapshots, err := store.Snapshots()
			if err != nil {
				t.Fatal(err)
			}
			again := readLegacy(t, f, after)
			result, err = store.MergeImport(again.Candidates, again.Presets, again.Sources)
			if err != nil {
				t.Fatal(err)
			}
			if result.AddedSkills+result.AddedPresets+result.AddedSources != 0 {
				t.Fatalf("repeat import added duplicate objects: %+v", result)
			}
			repeated, err := store.State()
			if err != nil || !reflect.DeepEqual(after, repeated) {
				t.Fatalf("repeat import changed state: %v", err)
			}
			repeatedSnapshots, err := store.Snapshots()
			if err != nil || len(snapshots) != len(repeatedSnapshots) {
				t.Fatalf("repeat import created a snapshot: %v", err)
			}
		})
	}
}

func TestReadMergesMultipleDisjointSourceScopes(t *testing.T) {
	f := newLegacyFixture(t)
	paths := map[string]string{
		"alpha": "skills/backend/alpha", "zulu": "skills/frontend/zulu",
		"beta": "skills/backend/beta", "gamma": "skills/other/gamma",
		"delta": "extras/delta", "epsilon": "extras/nested/epsilon",
	}
	for name, subpath := range paths {
		f.content(name, name+" content")
		f.skill(legacySkill{id: "legacy-" + name, name: name, sourceType: "git", source: "https://github.com/Team/Repo.git", branch: "main", subpath: subpath})
	}
	store := migrationStore(t)
	_, backend := installScopedLegacySkill(t, f, store, "alpha", paths["alpha"], "skills/backend")
	_, frontend := installScopedLegacySkill(t, f, store, "zulu", paths["zulu"], "skills/frontend")
	before, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	bundle := readLegacy(t, f, before)
	if len(bundle.Sources) != 4 || candidateNamed(t, bundle, "beta").Skill.SourceID != backend.ID || candidateNamed(t, bundle, "zulu").Skill.SourceID != frontend.ID {
		t.Fatalf("existing disjoint sources were not reused by scope: %+v", bundle.Sources)
	}
	delta, epsilon := candidateNamed(t, bundle, "delta").Skill, candidateNamed(t, bundle, "epsilon").Skill
	if delta.SourceID != epsilon.SourceID {
		t.Fatal("new skills in the same disjoint scope acquired separate sources")
	}
	for _, source := range bundle.Sources {
		if source.ID == delta.SourceID && source.Subpath != "extras" {
			t.Fatalf("new source was not scoped outside existing registrations: %+v", source)
		}
	}
	result, err := store.MergeImport(bundle.Candidates, bundle.Presets, bundle.Sources)
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedSkills != 4 || result.SkippedSkills != 2 || result.AddedSources != 2 || result.SkippedSources != 2 {
		t.Fatalf("unexpected merge counts: %+v", result)
	}
	after, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Sources[:2], before.Sources) || !reflect.DeepEqual(after.Skills[:2], before.Skills) {
		t.Fatal("migration modified existing disjoint sources or installations")
	}
}

func TestReadRetainsAncestorSkillWithoutOverlappingSource(t *testing.T) {
	f := newLegacyFixture(t)
	for _, name := range []string{"alpha", "root"} {
		f.content(name, name+" content")
		subpath := "skills/alpha"
		if name == "root" {
			subpath = "."
		}
		f.skill(legacySkill{id: "legacy-" + name, name: name, sourceType: "git", source: "https://github.com/Team/Repo.git", branch: "main", subpath: subpath})
	}
	store := migrationStore(t)
	_, source := installScopedLegacySkill(t, f, store, "alpha", "skills/alpha", "skills/alpha")
	before, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	bundle := readLegacy(t, f, before)
	root := candidateNamed(t, bundle, "root").Skill
	if root.SourceID != "" || root.Source != "https://github.com/Team/Repo.git" || root.Subpath != "." || len(bundle.Warnings) != 1 || len(bundle.Sources) != 1 || bundle.Sources[0].ID != source.ID {
		t.Fatalf("ancestor skill lost its origin or created an overlapping source: %+v", bundle)
	}
	if _, err = store.MergeImport(bundle.Candidates, bundle.Presets, bundle.Sources); err != nil {
		t.Fatal(err)
	}
}

func TestReadPreservesDetachedOriginRegardlessOfSkillOrder(t *testing.T) {
	for _, test := range []struct{ name, existing, added string }{
		{"existing first", "alpha", "beta"},
		{"new first", "zulu", "alpha"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLegacyFixture(t)
			for _, name := range []string{test.existing, test.added} {
				f.content(name, name+" content")
				f.skill(legacySkill{id: "legacy-" + name, name: name, sourceType: "git", source: "https://github.com/Team/Repo.git", branch: "main", subpath: "skills/" + name})
			}
			store := migrationStore(t)
			_, source := installScopedLegacySkill(t, f, store, test.existing, "skills/"+test.existing, "skills/"+test.existing)
			if err := store.DeleteSource(source.ID); err != nil {
				t.Fatal(err)
			}
			before, err := store.State()
			if err != nil {
				t.Fatal(err)
			}
			bundle := readLegacy(t, f, before)
			if candidateNamed(t, bundle, test.existing).Skill.Source != before.Skills[0].Source {
				t.Fatal("migration rewrote a detached installation's origin")
			}
			result, err := store.MergeImport(bundle.Candidates, bundle.Presets, bundle.Sources)
			if err != nil {
				t.Fatal(err)
			}
			if result.AddedSkills != 1 || result.SkippedSkills != 1 || result.AddedSources != 1 {
				t.Fatalf("unexpected merge counts: %+v", result)
			}
			after, err := store.State()
			if err != nil || !reflect.DeepEqual(after.Skills[0], before.Skills[0]) {
				t.Fatalf("migration modified detached installation metadata: %v", err)
			}
		})
	}
}

func TestReadKeepsUnrelatedPresetWithSameName(t *testing.T) {
	f := newLegacyFixture(t)
	f.content("alpha", "imported content")
	f.skill(legacySkill{id: "legacy-alpha", name: "alpha"})
	f.scenario("legacy-h2", "H2", "Imported H2", "legacy-alpha")
	existingPreset := skillstore.Preset{ID: "preset-existing", Name: "H2", Description: "Existing H2", SkillIDs: []string{"skill-other"}}
	existing := skillstore.State{Presets: []skillstore.Preset{existingPreset}}
	bundle := readLegacy(t, f, existing)
	imported := presetNamed(t, bundle, "H2 (skills-manager)")
	if imported.ID == existingPreset.ID || imported.Description != "Imported H2" || !reflect.DeepEqual(imported.SkillIDs, []string{candidateNamed(t, bundle, "alpha").Skill.ID}) {
		t.Fatalf("unrelated same-name preset was overwritten: %+v", imported)
	}
	if !reflect.DeepEqual(existing.Presets[0], existingPreset) {
		t.Fatal("Read mutated its existing state input")
	}
	existing.Presets = append(existing.Presets, imported)
	existing.Skills = append(existing.Skills, candidateNamed(t, bundle, "alpha").Skill)
	again := readLegacy(t, f, existing)
	for _, preset := range again.Presets {
		if preset.ID != imported.ID || preset.Name != imported.Name {
			t.Fatalf("repeated import created another preset: %+v", preset)
		}
	}
}

func TestReadNeverTrustsCentralPathOutsideAdjacentSkills(t *testing.T) {
	f := newLegacyFixture(t)
	external, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	external = filepath.Join(external, "alpha")
	if err = os.Mkdir(external, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(external, "SKILL.md"), []byte("outside trusted root"), 0644); err != nil {
		t.Fatal(err)
	}
	f.skill(legacySkill{id: "legacy-alpha", name: "alpha", centralPath: external})
	if _, err = Read(f.filename, skillstore.State{}); err == nil {
		t.Fatal("import read an external central_path without adjacent trusted content")
	}

	trusted := f.content("alpha", "trusted adjacent content")
	bundle, err := Read(f.filename, skillstore.State{})
	if err != nil {
		return // Rejecting an external legacy path is also safe.
	}
	candidate := candidateNamed(t, bundle, "alpha")
	wantHash, err := skillstore.HashDir(trusted)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Path != trusted || candidate.Skill.ContentHash != wantHash {
		t.Fatalf("external central_path overrode trusted adjacent content: %+v", candidate)
	}
}

func TestReadRebasesMovedWorkspaceByCentralDirectoryName(t *testing.T) {
	f := newLegacyFixture(t)
	content := f.content("archived-alpha", "moved workspace content")
	f.skill(legacySkill{id: "legacy-alpha", name: "alpha", centralPath: "/old-home/.skills-manager/skills/archived-alpha"})
	bundle := readLegacy(t, f, skillstore.State{})
	candidate := candidateNamed(t, bundle, "alpha")
	wantHash, err := skillstore.HashDir(content)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Path != content || candidate.Skill.ContentHash != wantHash {
		t.Fatalf("moved content was not resolved within the adjacent skills root: %+v", candidate)
	}
}

func TestReadReportsUnmappedLegacyRecords(t *testing.T) {
	f := newLegacyFixture(t)
	for _, table := range []string{"projects", "skill_targets", "scenario_skill_tools", "active_scenario", "skill_tags", "discovered_skills", "audit_log", "pending_conflicts"} {
		f.exec("CREATE TABLE " + table + " (id TEXT)")
		f.exec("INSERT INTO " + table + " (id) VALUES ('legacy-record')")
	}
	f.exec(`CREATE TABLE settings(key TEXT, value TEXT)`)
	f.exec(`INSERT INTO settings(key, value) VALUES ('proxy_url', 'private-setting-do-not-copy')`)
	bundle := readLegacy(t, f, skillstore.State{})
	warnings := strings.Join(bundle.Warnings, "\n")
	for _, label := range []string{"项目", "部署", "工具开关", "当前场景", "设置", "标签", "发现", "历史", "冲突"} {
		if !strings.Contains(warnings, label) {
			t.Errorf("missing warning for unmapped %s: %s", label, warnings)
		}
	}
	if strings.Contains(warnings, "private-setting-do-not-copy") || len(bundle.Candidates) != 0 || len(bundle.Presets) != 0 || len(bundle.Sources) != 0 {
		t.Fatal("unmapped legacy records or private settings leaked into imported content")
	}
}

func TestReadRejectsMissingSkillDocument(t *testing.T) {
	f := newLegacyFixture(t)
	if err := os.Mkdir(filepath.Join(f.root, "skills", "alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	f.skill(legacySkill{id: "legacy-alpha", name: "alpha"})
	if _, err := Read(f.filename, skillstore.State{}); err == nil {
		t.Fatal("accepted a skill directory without SKILL.md")
	}
}

func TestReadRejectsUnknownSchemaAndDanglingMembers(t *testing.T) {
	t.Run("unknown schema", func(t *testing.T) {
		f := newLegacyFixture(t)
		f.exec(`DROP TABLE skills`)
		f.exec(`CREATE TABLE skills(id TEXT PRIMARY KEY)`)
		if _, err := Read(f.filename, skillstore.State{}); err == nil {
			t.Fatal("accepted an unknown legacy skills schema")
		}
	})
	t.Run("dangling skill", func(t *testing.T) {
		f := newLegacyFixture(t)
		f.scenario("legacy-daily", "Daily", "", "missing-skill")
		if _, err := Read(f.filename, skillstore.State{}); err == nil {
			t.Fatal("silently dropped a scenario member that refers to a missing skill")
		}
	})
	t.Run("dangling scenario", func(t *testing.T) {
		f := newLegacyFixture(t)
		f.content("alpha", "content")
		f.skill(legacySkill{id: "legacy-alpha", name: "alpha"})
		f.exec(`INSERT INTO scenario_skills(scenario_id, skill_id) VALUES ('missing-scenario', 'legacy-alpha')`)
		if _, err := Read(f.filename, skillstore.State{}); err == nil {
			t.Fatal("silently dropped a member that refers to a missing scenario")
		}
	})
}
