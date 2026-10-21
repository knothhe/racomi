package desktop

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	skillstore "racomi/internal/skills"
)

type skillsManagerFixture struct {
	root, filename string
	paths          map[string]string
	service        *Service
}

func newSkillsManagerFixture(t *testing.T) *skillsManagerFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &skillsManagerFixture{root: root, filename: filepath.Join(root, "skills-manager.db"), paths: map[string]string{}}
	db, err := sql.Open("sqlite3", f.filename)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("skills-manager fixture SQL: %v", err)
		}
	}
	exec(`PRAGMA journal_mode=WAL`)
	exec(`PRAGMA wal_autocheckpoint=0`)
	exec(`CREATE TABLE skills (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT,
		source_type TEXT NOT NULL, source_ref TEXT, source_ref_resolved TEXT,
		source_subpath TEXT, source_branch TEXT, source_revision TEXT,
		central_path TEXT NOT NULL UNIQUE, created_at INTEGER, updated_at INTEGER
	)`)
	exec(`CREATE TABLE scenarios (
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT,
		sort_order INTEGER DEFAULT 0, created_at INTEGER, updated_at INTEGER
	)`)
	exec(`CREATE TABLE scenario_skills (
		scenario_id TEXT NOT NULL, skill_id TEXT NOT NULL, added_at INTEGER,
		sort_order INTEGER DEFAULT 0, PRIMARY KEY(scenario_id, skill_id)
	)`)
	for _, name := range []string{"alpha", "local"} {
		directory := filepath.Join(root, "skills", name)
		if err = os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		f.paths[name] = filepath.Join(directory, "SKILL.md")
		if err = os.WriteFile(f.paths[name], []byte("---\nname: "+name+"\ndescription: fixture\n---\n"+name+" original content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO skills(id, name, description, source_type, source_ref, source_subpath, source_branch, source_revision, central_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "legacy-alpha", "alpha", "Database alpha description", "git", "https://github.com/team/repo.git", "skills/alpha", "main", "fixture-revision", filepath.Dir(f.paths["alpha"]), 1788350907638, 1791677203003)
	exec(`INSERT INTO skills(id, name, description, source_type, central_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "legacy-local", "local", "Database local description", "import", filepath.Dir(f.paths["local"]), 1788350907638, 1791677203003)
	exec(`INSERT INTO scenarios(id, name, description, sort_order) VALUES ('legacy-daily', 'Daily', 'Daily work', 0)`)
	exec(`INSERT INTO scenario_skills(scenario_id, skill_id, added_at, sort_order) VALUES ('legacy-daily', 'legacy-local', 1000, 0)`)
	exec(`INSERT INTO scenario_skills(scenario_id, skill_id, added_at, sort_order) VALUES ('legacy-daily', 'legacy-alpha', 1001, 1)`)
	exec(`CREATE TABLE settings(key TEXT, value TEXT)`)
	exec(`INSERT INTO settings(key, value) VALUES ('proxy_url', 'private-setting-do-not-copy')`)
	f.service, err = New(filepath.Join(root, "racomi-data"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func skillsManagerState(t *testing.T, service *Service) AppState {
	t.Helper()
	app, err := service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func skillsManagerIndex(t *testing.T, service *Service) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(service.store.Root(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertSkillsManagerUnchanged(t *testing.T, service *Service, before AppState, index string) {
	t.Helper()
	if actual := skillsManagerState(t, service); !reflect.DeepEqual(actual, before) {
		t.Fatalf("cancelled import changed application state: %+v", actual)
	}
	if skillsManagerIndex(t, service) != index {
		t.Fatal("cancelled import wrote the store index")
	}
}

func TestSkillsManagerPreviewImportAndRepeatedImport(t *testing.T) {
	f := newSkillsManagerFixture(t)
	service := f.service
	before := skillsManagerState(t, service)
	indexBefore := skillsManagerIndex(t, service)
	preview, err := service.PreviewSkillsManager("  " + f.filename + "  ")
	if err != nil {
		t.Fatal(err)
	}
	if preview.ID == "" || preview.DatabasePath != f.filename || preview.SkillCount != 2 || preview.PresetCount != 1 || preview.SourceCount != 1 {
		t.Fatalf("incorrect preview identity or counts: %+v", preview)
	}
	if !reflect.DeepEqual(preview.SkillNames, []string{"alpha", "local"}) || !reflect.DeepEqual(preview.PresetNames, []string{"Daily"}) {
		t.Fatalf("incorrect preview names: %+v", preview)
	}
	warnings := strings.Join(preview.Warnings, "\n")
	if !strings.Contains(warnings, "设置") || strings.Contains(warnings, "private-setting-do-not-copy") {
		t.Fatalf("preview omitted migration warnings or exposed private settings: %v", preview.Warnings)
	}
	assertSkillsManagerUnchanged(t, service, before, indexBefore)

	imported, err := service.ImportSkillsManager(preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Result != (skillstore.ImportResult{AddedSkills: 2, AddedPresets: 1, AddedSources: 1}) {
		t.Fatalf("incorrect first import counts: %+v", imported.Result)
	}
	app := skillsManagerState(t, service)
	if !reflect.DeepEqual(imported.App, app) || app.DataDir != service.store.Root() || len(app.State.Skills) != 2 || len(app.State.Presets) != 1 || len(app.State.Sources) != 1 || len(app.State.Targets) != 0 || len(app.Snapshots) != 1 {
		t.Fatalf("import did not return the complete committed application state: %+v", app)
	}
	ids := map[string]string{}
	for _, skill := range app.State.Skills {
		ids[skill.Name] = skill.ID
		document, readErr := service.ReadSkillDocument(skill.ID)
		if readErr != nil || !strings.Contains(document, skill.Name+" original content") {
			t.Fatalf("imported skill object is unreadable: %q, %v", document, readErr)
		}
		created, parseErr := time.Parse(time.RFC3339Nano, skill.InstalledAt)
		if parseErr != nil || created.UnixMilli() != 1788350907638 {
			t.Fatalf("import lost the legacy installation time: %q, %v", skill.InstalledAt, parseErr)
		}
		if skill.Name == "alpha" && (skill.SourceID != app.State.Sources[0].ID || skill.Source != "https://github.com/team/repo.git" || skill.Ref != "main") {
			t.Fatalf("Git source association was lost: %+v", skill)
		}
		if skill.Name == "local" && (skill.Source != "" || skill.SourceID != "") {
			t.Fatalf("local skill acquired a Git source: %+v", skill)
		}
	}
	if !reflect.DeepEqual(app.State.Presets[0].SkillIDs, []string{ids["local"], ids["alpha"]}) {
		t.Fatalf("scenario members or their order were lost: %+v", app.State.Presets[0])
	}
	var snapshot struct {
		State skillstore.State `json:"state"`
	}
	data, err := os.ReadFile(filepath.Join(service.store.Root(), "snapshots", app.Snapshots[0].ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &snapshot); err != nil || !reflect.DeepEqual(snapshot.State, before.State) {
		t.Fatalf("first import snapshot did not preserve the prior store: %+v, %v", snapshot.State, err)
	}
	indexCommitted := skillsManagerIndex(t, service)
	if _, err = service.ImportSkillsManager(preview.ID); err == nil {
		t.Fatal("a successfully used preview token was accepted again")
	}
	assertSkillsManagerUnchanged(t, service, app, indexCommitted)

	again, err := service.PreviewSkillsManager(f.filename)
	if err != nil || again.ID == "" || again.ID == preview.ID {
		t.Fatalf("a new preview did not receive a fresh token: %+v, %v", again, err)
	}
	repeated, err := service.ImportSkillsManager(again.ID)
	if err != nil || repeated.Result != (skillstore.ImportResult{SkippedSkills: 2, SkippedPresets: 1, SkippedSources: 1}) {
		t.Fatalf("repeated import did not reuse the existing store: %+v, %v", repeated.Result, err)
	}
	assertSkillsManagerUnchanged(t, service, app, indexCommitted)
}

func TestSkillsManagerRejectsUnknownAndExpiredPreviewTokens(t *testing.T) {
	f := newSkillsManagerFixture(t)
	before := skillsManagerState(t, f.service)
	index := skillsManagerIndex(t, f.service)
	if _, err := f.service.ImportSkillsManager("unknown-before-preview"); err == nil {
		t.Fatal("accepted an import without a preview")
	}
	preview, err := f.service.PreviewSkillsManager(f.filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "unknown-token"} {
		if _, err = f.service.ImportSkillsManager(token); err == nil {
			t.Fatalf("accepted invalid token %q", token)
		}
		assertSkillsManagerUnchanged(t, f.service, before, index)
	}
	f.service.legacyPreview.expires = time.Now().Add(-time.Second)
	if _, err = f.service.ImportSkillsManager(preview.ID); err == nil {
		t.Fatal("accepted an expired preview token")
	}
	assertSkillsManagerUnchanged(t, f.service, before, index)
	fresh, err := f.service.PreviewSkillsManager(f.filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ImportSkillsManager(preview.ID); err == nil {
		t.Fatal("accepted a token replaced by a fresh preview")
	}
	if _, err = f.service.ImportSkillsManager(fresh.ID); err != nil {
		t.Fatalf("rejecting an old token invalidated the current preview: %v", err)
	}
}

func TestSkillsManagerRejectsContentChangesAfterPreview(t *testing.T) {
	f := newSkillsManagerFixture(t)
	before := skillsManagerState(t, f.service)
	index := skillsManagerIndex(t, f.service)
	preview, err := f.service.PreviewSkillsManager(f.filename)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.paths["local"], []byte("---\nname: local\ndescription: changed\n---\ncontent changed after preview\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ImportSkillsManager(preview.ID); err == nil {
		t.Fatal("import accepted content that changed after inspection")
	}
	assertSkillsManagerUnchanged(t, f.service, before, index)
	fresh, err := f.service.PreviewSkillsManager(f.filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ImportSkillsManager(fresh.ID); err != nil {
		t.Fatalf("re-previewing the changed skill did not recover the import: %v", err)
	}
	for _, skill := range skillsManagerState(t, f.service).State.Skills {
		if skill.Name == "local" {
			document, readErr := f.service.ReadSkillDocument(skill.ID)
			if readErr != nil || !strings.Contains(document, "content changed after preview") {
				t.Fatalf("fresh import did not use the freshly inspected content: %q, %v", document, readErr)
			}
		}
	}
}

func TestFailedSkillsManagerPreviewInvalidatesPreviousToken(t *testing.T) {
	for _, failure := range []string{"empty-path", "relative-path", "missing-database", "invalid-database"} {
		t.Run(failure, func(t *testing.T) {
			f := newSkillsManagerFixture(t)
			before := skillsManagerState(t, f.service)
			index := skillsManagerIndex(t, f.service)
			preview, err := f.service.PreviewSkillsManager(f.filename)
			if err != nil {
				t.Fatal(err)
			}
			filename := " "
			switch failure {
			case "relative-path":
				filename = "skills-manager.db"
			case "missing-database":
				filename = filepath.Join(f.root, "missing.db")
			case "invalid-database":
				filename = filepath.Join(f.root, "invalid.db")
				if err = os.WriteFile(filename, []byte("not a SQLite database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = f.service.PreviewSkillsManager(filename); err == nil {
				t.Fatalf("accepted invalid preview input %q", filename)
			}
			if _, err = f.service.ImportSkillsManager(preview.ID); err == nil {
				t.Fatal("failed new preview left the old token usable")
			}
			assertSkillsManagerUnchanged(t, f.service, before, index)
		})
	}
}

func TestDefaultSkillsManagerPathFollowsHome(t *testing.T) {
	service := &Service{}
	for _, name := range []string{"first-home", "second-home"} {
		home := filepath.Join(t.TempDir(), name)
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		filename, err := service.DefaultSkillsManagerPath()
		if err != nil || filename != filepath.Join(home, ".skills-manager", "skills-manager.db") {
			t.Fatalf("default path did not follow HOME: %q, %v", filename, err)
		}
	}
}

func TestChooseSkillsManagerFileRequiresNativeApplication(t *testing.T) {
	filename, err := (&Service{}).ChooseSkillsManagerFile()
	if err == nil || filename != "" || !strings.Contains(err.Error(), "桌面应用") {
		t.Fatalf("native file choice without an app did not fail clearly: %q, %v", filename, err)
	}
}
