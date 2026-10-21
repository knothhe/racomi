package backup

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"racomi/internal/testutil"
	skillstore "racomi/internal/skills"
)

func fixture(t *testing.T) (*skillstore.Store, skillstore.State) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := skillstore.Open(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "skill")
	if err := os.MkdirAll(filepath.Join(source, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: backup-test\ndescription: Backup fixture\n---\ncontent"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "run.sh"), []byte("#!/bin/sh\ntrue\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "resource.zip"), []byte("original skill archive content"), 0600); err != nil {
		t.Fatal(err)
	}
	skills, err := store.Install([]skillstore.Candidate{{Skill: skillstore.Skill{Name: "backup-test", Source: "owner/repo", Ref: "main", Subpath: "skills/backup-test", Revision: strings.Repeat("a", 40)}, Path: source}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SavePreset(skillstore.Preset{Name: "开发", SkillIDs: []string{skills[0].ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveTarget(skillstore.Target{Name: "私有路径", Kind: "global", Path: filepath.Join(root, "private-target")}); err != nil {
		t.Fatal(err)
	}
	state, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	return store, state
}

func TestPortableRoundTripPreservesObjectsAndExcludesTargets(t *testing.T) {
	store, state := fixture(t)
	filename := filepath.Join(t.TempDir(), "backup.zip")
	if err := Export(store.Root(), state, filename); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != "manifest.json" {
			continue
		}
		in, _ := file.Open()
		var data manifest
		err = json.NewDecoder(in).Decode(&data)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(data.State.Targets) != 0 {
			t.Fatal("portable backup contains target paths")
		}
	}
	reader.Close()
	restored, err := skillstore.Open(filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	restrictUmask(t)
	if err = Import(restored, filename); err != nil {
		t.Fatal(err)
	}
	actual, err := restored.State()
	if err != nil {
		t.Fatal(err)
	}
	if len(actual.Skills) != 1 || len(actual.Presets) != 1 || len(actual.Targets) != 0 {
		t.Fatalf("unexpected restored state: %+v", actual)
	}
	if actual.Skills[0].ContentHash != state.Skills[0].ContentHash {
		t.Fatal("content version changed")
	}
	hash, err := skillstore.HashDir(filepath.Join(restored.Root(), "objects", actual.Skills[0].ContentHash))
	if err != nil || hash != actual.Skills[0].ContentHash {
		t.Fatalf("file contents or executable bits lost: %s %v", hash, err)
	}
}

func TestPortableRoundTripPreservesSourcesAndRemovedLocalSkills(t *testing.T) {
	store, state := fixture(t)
	state.Skills[0].SourceID = "source-installed"
	state.Skills[0].UpstreamRemoved = true
	state.Sources = []skillstore.Source{
		{
			ID: "source-installed", Name: "已安装来源", Repository: state.Skills[0].Source, Ref: state.Skills[0].Ref, Subpath: ".", Mode: "all",
			Revision: strings.Repeat("b", 40), RefreshedAt: "2026-10-11T01:00:00Z",
			Warnings: []string{"ignored repository entry: AGENTS.md"},
			Catalog: []skillstore.SourceSkill{
				{Name: state.Skills[0].Name, Description: state.Skills[0].Description, Subpath: state.Skills[0].Subpath, Revision: state.Skills[0].Revision, ContentHash: state.Skills[0].ContentHash, Removed: true},
				{Name: "new-remote-skill", Description: "待同步", Subpath: "skills/new-remote-skill", Revision: strings.Repeat("b", 40), ContentHash: strings.Repeat("b", 64), New: true},
				{Name: "old-skipped", Subpath: "skills/old-skipped", Revision: strings.Repeat("a", 40), ContentHash: strings.Repeat("a", 64), SkipReason: "symlink is not allowed in skill content: helper"},
				{Name: "unreadable-skill", Subpath: "skills/broken", SkipReason: "missing YAML frontmatter"},
			},
		},
		{
			ID: "source-uninstalled", Name: "尚未安装来源", Repository: "owner/another-repo", Ref: "main", Subpath: "skills", Mode: "selected",
			Revision: strings.Repeat("c", 40), RefreshedAt: "2026-10-11T02:00:00Z", LastError: "最近刷新失败，保留清单",
			Catalog: []skillstore.SourceSkill{{Name: "catalog-only", Description: "只有清单，没有本地对象", Subpath: "skills/catalog-only", Revision: strings.Repeat("c", 40), ContentHash: strings.Repeat("c", 64)}},
		},
		{ID: "source-unrefreshed", Name: "仅保存的来源", Repository: "owner/future-repo", Subpath: ".", Mode: "all", Catalog: []skillstore.SourceSkill{}},
	}
	filename := filepath.Join(t.TempDir(), "sources.zip")
	if err := Export(store.Root(), state, filename); err != nil {
		t.Fatal(err)
	}
	restored, err := skillstore.Open(filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Import(restored, filename); err != nil {
		t.Fatal(err)
	}
	actual, err := restored.State()
	if err != nil {
		t.Fatal(err)
	}
	if actual.Schema != 2 || !reflect.DeepEqual(actual.Sources, state.Sources) {
		t.Fatalf("source configuration or catalog lost: %+v", actual)
	}
	if len(actual.Skills) != 1 || !actual.Skills[0].UpstreamRemoved || actual.Skills[0].SourceID != "source-installed" || actual.Skills[0].ContentHash != state.Skills[0].ContentHash {
		t.Fatalf("removed upstream skill was not retained: %+v", actual.Skills)
	}
	if !reflect.DeepEqual(actual.Presets, state.Presets) || len(actual.Targets) != 0 {
		t.Fatalf("preset reference or portable target policy changed: %+v", actual)
	}
	if _, err = os.Stat(filepath.Join(restored.Root(), "objects", strings.Repeat("c", 64))); !os.IsNotExist(err) {
		t.Fatalf("catalog-only skill unexpectedly needs a local object: %v", err)
	}
}

func TestPortableSourceOnlyCatalogCanExceedOldManifestLimit(t *testing.T) {
	store, err := skillstore.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	source := skillstore.Source{ID: "source-catalog", Name: "未安装的大目录", Repository: "owner/catalog", Ref: "main", Subpath: ".", Mode: "selected", Catalog: []skillstore.SourceSkill{}}
	for i := 0; i < 4096; i++ {
		name := fmt.Sprintf("skill-%04d", i)
		source.Catalog = append(source.Catalog, skillstore.SourceSkill{Name: name, Description: strings.Repeat("catalog ", 64), Subpath: "skills/" + name, Revision: strings.Repeat("a", 40), ContentHash: strings.Repeat("a", 64)})
	}
	state.Sources = []skillstore.Source{source}
	data, err := json.MarshalIndent(manifest{Format: "racomi-backup", Version: 1, State: state}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 2<<20 || len(data) > maxManifestBytes {
		t.Fatalf("fixture is outside intended manifest size range: %d", len(data))
	}
	filename := filepath.Join(t.TempDir(), "catalog.zip")
	if err = Export(store.Root(), state, filename); err != nil {
		t.Fatal(err)
	}
	restored, err := skillstore.Open(filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Import(restored, filename); err != nil {
		t.Fatal(err)
	}
	actual, err := restored.State()
	if err != nil {
		t.Fatal(err)
	}
	if len(actual.Skills) != 0 || !reflect.DeepEqual(actual.Sources, state.Sources) {
		t.Fatal("source-only catalog did not survive backup")
	}
}

func TestLegacyPortableBackupMigratesSourceAndPreservesLocalTarget(t *testing.T) {
	store, state := fixture(t)
	state.Schema = 1
	state.Sources = nil
	state.Skills[0].SourceID = ""
	state.Skills[0].UpstreamRemoved = false
	state.Targets = []skillstore.Target{}
	filename := filepath.Join(t.TempDir(), "export.zip")
	if err := Export(store.Root(), state, filename); err != nil {
		t.Fatal(err)
	}
	// Use the actual schema 1 shape: source metadata only lived on each skill.
	data, err := json.Marshal(manifest{Format: "racomi-backup", Version: 1, State: state})
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err = json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	legacyState := legacy["state"].(map[string]any)
	delete(legacyState, "sources")
	for _, raw := range legacyState["skills"].([]any) {
		skill := raw.(map[string]any)
		delete(skill, "sourceId")
		delete(skill, "upstreamRemoved")
	}
	data, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyFilename := filepath.Join(t.TempDir(), "legacy.zip")
	replaceManifest(t, filename, legacyFilename, data)
	restored, before := fixture(t)
	if err = restored.SetTargetPresets(before.Targets[0].ID, []string{before.Presets[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err = restored.Apply(before.Targets[0].ID); err != nil {
		t.Fatal(err)
	}
	before, err = restored.State()
	if err != nil {
		t.Fatal(err)
	}
	if err = Import(restored, legacyFilename); err != nil {
		t.Fatal(err)
	}
	actual, err := restored.State()
	if err != nil {
		t.Fatal(err)
	}
	if actual.Schema != 2 || len(actual.Sources) != 1 || actual.Sources[0].Mode != "selected" || actual.Sources[0].Repository != state.Skills[0].Source || actual.Sources[0].Ref != state.Skills[0].Ref {
		t.Fatalf("old backup source was not migrated: %+v", actual)
	}
	if len(actual.Skills) != 1 || actual.Skills[0].SourceID != actual.Sources[0].ID || actual.Skills[0].ID != state.Skills[0].ID || !reflect.DeepEqual(actual.Presets, state.Presets) {
		t.Fatal("migration changed skill identity or preset references")
	}
	if len(actual.Targets) != 1 || actual.Targets[0].ID != before.Targets[0].ID || len(actual.Targets[0].EnabledPresetIDs) != 0 || !reflect.DeepEqual(actual.Targets[0].Deployments, before.Targets[0].Deployments) {
		t.Fatalf("restore did not preserve local target ownership: %+v", actual.Targets)
	}
	hash, err := skillstore.HashDir(filepath.Join(actual.Targets[0].Path, before.Skills[0].Name))
	if err != nil || hash != before.Skills[0].ContentHash {
		t.Fatalf("restore changed the target directory: %s %v", hash, err)
	}
}

func replaceManifest(t *testing.T, source, destination string, data []byte) {
	t.Helper()
	reader, err := zip.OpenReader(source)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for _, entry := range reader.File {
		out, err := writer.CreateHeader(&entry.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == "manifest.json" {
			if _, err = out.Write(data); err != nil {
				t.Fatal(err)
			}
			continue
		}
		in, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnsafeArchiveIsRejectedWithoutChangingState(t *testing.T) {
	store, before := fixture(t)
	for _, unsafeName := range []string{"../escape", "objects/../../escape", "/absolute", "objects\\escape", "unknown/file"} {
		t.Run(unsafeName, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "bad.zip")
			file, _ := os.Create(filename)
			writer := zip.NewWriter(file)
			entry, _ := writer.Create("manifest.json")
			data, _ := json.Marshal(manifest{Format: "racomi-backup", Version: 1, State: skillstore.State{Schema: 1, Skills: []skillstore.Skill{}, Presets: []skillstore.Preset{}, Targets: []skillstore.Target{}}})
			entry.Write(data)
			entry, _ = writer.Create(unsafeName)
			entry.Write([]byte("bad"))
			writer.Close()
			file.Close()
			if err := Import(store, filename); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			after, err := store.State()
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Skills) != len(before.Skills) || after.Skills[0].ContentHash != before.Skills[0].ContentHash {
				t.Fatal("failed import altered state")
			}
		})
	}
}

func TestTamperedObjectRejected(t *testing.T) {
	store, state := fixture(t)
	state.Targets = []skillstore.Target{}
	filename := filepath.Join(t.TempDir(), "bad.zip")
	file, _ := os.Create(filename)
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("manifest.json")
	data, _ := json.Marshal(manifest{Format: "racomi-backup", Version: 1, State: state})
	entry.Write(data)
	entry, _ = writer.Create("objects/" + state.Skills[0].ContentHash + "/SKILL.md")
	entry.Write([]byte("tampered"))
	writer.Close()
	file.Close()
	if err := Import(store, filename); err == nil {
		t.Fatal("tampered content was accepted")
	}
	actual, _ := store.State()
	if len(actual.Skills) != 1 || actual.Skills[0].ContentHash != state.Skills[0].ContentHash {
		t.Fatal("failed validation changed index")
	}
}

func TestExportCannotWriteIntoDataThroughSymlink(t *testing.T) {
	store, state := fixture(t)
	link := filepath.Join(t.TempDir(), "linked-data")
	if err := testutil.Symlink(t, store.Root(), link); err != nil {
		t.Fatal(err)
	}
	if err := Export(store.Root(), state, filepath.Join(link, "backup.zip")); err == nil {
		t.Fatal("export wrote into its data directory")
	}
}

func TestInsideConservativelyHandlesCaseOnDesktopVolumes(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("case-insensitive desktop path policy")
	}
	base := filepath.Join(t.TempDir(), "protected")
	alias := filepath.Join(filepath.Dir(base), "PROTECTED", "backup.zip")
	if !inside(base, alias) {
		t.Fatal("case variant bypasses protected path")
	}
	if inside(base, filepath.Join(filepath.Dir(base), "protected-other", "backup.zip")) {
		t.Fatal("unrelated sibling was treated as a protected path")
	}
}

func TestExportRejectsTargetCaseAliasWithoutChangingDeployment(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("case-insensitive desktop path policy")
	}
	store, state := fixture(t)
	target := state.Targets[0]
	if err := store.SetTargetPresets(target.ID, []string{state.Presets[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(target.ID); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(target.Path), strings.ToUpper(filepath.Base(target.Path)))
	if _, err := os.Stat(alias); err != nil {
		t.Skip("test volume is case-sensitive")
	}
	state, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(alias, state.Skills[0].Name, "resource.zip")
	if err := Export(store.Root(), state, destination); err == nil {
		t.Fatal("export overwrote deployed content through a case alias")
	} else if !strings.Contains(err.Error(), "技能目标目录") {
		t.Fatalf("export failed for an unrelated reason: %v", err)
	}
	actual, err := skillstore.HashDir(filepath.Join(target.Path, state.Skills[0].Name))
	if err != nil || actual != state.Skills[0].ContentHash {
		t.Fatalf("rejected export altered deployed content: %s %v", actual, err)
	}
}

func TestExportRejectsDataCaseAliasWithoutDamagingSnapshotObjects(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("case-insensitive desktop path policy")
	}
	store, state := fixture(t)
	oldSkill := state.Skills[0]
	source := filepath.Join(filepath.Dir(store.Root()), "skill")
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: backup-test\ndescription: updated\n---\nnew version"), 0600); err != nil {
		t.Fatal(err)
	}
	updatedSkill := oldSkill
	var err error
	updatedSkill.ContentHash, err = skillstore.HashDir(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Install([]skillstore.Candidate{{Skill: updatedSkill, Path: source}}); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(store.Root()), strings.ToUpper(filepath.Base(store.Root())))
	if _, err := os.Stat(alias); err != nil {
		t.Skip("test volume is case-sensitive")
	}
	state, err = store.State()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(alias, "objects", oldSkill.ContentHash, "resource.zip")
	if err := Export(store.Root(), state, destination); err == nil {
		t.Fatal("export overwrote a snapshot-only object through a case alias")
	} else if !strings.Contains(err.Error(), "racomi 数据目录") {
		t.Fatalf("export failed for an unrelated reason: %v", err)
	}
	actual, err := skillstore.HashDir(filepath.Join(store.Root(), "objects", oldSkill.ContentHash))
	if err != nil || actual != oldSkill.ContentHash {
		t.Fatalf("rejected export damaged snapshot content: %s %v", actual, err)
	}
}

func TestExportRejectsSecondaryAndRetiredDeploymentDirectories(t *testing.T) {
	store, initial := fixture(t)
	root := filepath.Join(filepath.Dir(store.Root()), "repository")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(skillstore.Target{Name: "multi directory", Kind: "project", Path: root, DirectoryIDs: []string{"agents", "claude", "codex"}, EnabledSkillIDs: []string{initial.Skills[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Apply(target.ID); err != nil {
		t.Fatal(err)
	}
	// Claude remains owned until the next explicit application removes it.
	target.DirectoryIDs = []string{"agents", "codex"}
	if _, err = store.SaveTarget(target); err != nil {
		t.Fatal(err)
	}
	current, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{".agents", ".codex", ".claude"} {
		destination := filepath.Join(root, directory, "skills", "backup.zip")
		if err = os.WriteFile(destination, []byte("existing user file"), 0644); err != nil {
			t.Fatal(err)
		}
		if err = Export(store.Root(), current, destination); err == nil || !strings.Contains(err.Error(), "目标目录之外") {
			t.Fatalf("export wrote into %s directory: %v", directory, err)
		}
		data, readErr := os.ReadFile(destination)
		if readErr != nil || string(data) != "existing user file" {
			t.Fatalf("rejected export changed an existing file: %s %v", directory, readErr)
		}
	}
}

func TestPortableBackupPreservesCurrentCustomPathsAndMachineDefaultsOnImport(t *testing.T) {
	store, initial := fixture(t)
	root := filepath.Join(filepath.Dir(store.Root()), "custom-project")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target, err := store.SaveTarget(skillstore.Target{Name: "custom project", Kind: "project", Path: root, DirectoryIDs: []string{}, CustomPaths: []string{"private/skills", "../external/skills"}, EnabledSkillIDs: []string{initial.Skills[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Apply(target.ID); err != nil {
		t.Fatal(err)
	}
	settings := initial.Settings
	settings.DefaultGlobalDirectories = []string{initial.Targets[0].ID}
	settings.CustomProjectDirectories = []skillstore.ProjectDirectory{{ID: "project-dir-team", Name: "Team", Path: "team/skills"}}
	settings.DefaultProjectDirectories = []string{"project-dir-team"}
	if err = store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	before, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(filepath.Dir(store.Root()), "custom-backup.zip")
	if err = Export(store.Root(), before, destination); err != nil {
		t.Fatal(err)
	}
	// Export strips target metadata from its private manifest, never from its input.
	if !reflect.DeepEqual(before.Targets[len(before.Targets)-1].CustomPaths, target.CustomPaths) {
		t.Fatal("export mutated the caller's custom path arrays")
	}
	reader, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		if entry.Name != "manifest.json" {
			continue
		}
		file, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		var manifestData manifest
		decodeErr := json.NewDecoder(file).Decode(&manifestData)
		file.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if len(manifestData.State.Targets) != 0 {
			t.Fatal("portable export exposed custom project paths")
		}
		if !reflect.DeepEqual(manifestData.State.Settings.CustomProjectDirectories, settings.CustomProjectDirectories) {
			t.Fatal("portable export lost project directory templates")
		}
	}
	reader.Close()
	settings.CustomProjectDirectories[0].Path = "current/skills"
	settings.CustomProjectDirectories[0].Name = "Current machine preference"
	if err = store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err = Import(store, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	current := restored.Targets[len(restored.Targets)-1]
	if !reflect.DeepEqual(current.CustomPaths, target.CustomPaths) || !reflect.DeepEqual(current.Paths, target.Paths) || len(current.DirectoryIDs) != 0 || len(current.Deployments) != 2 || !current.Pending {
		t.Fatalf("import lost local custom-only project ownership: %+v", current)
	}
	if !reflect.DeepEqual(restored.Settings, settings) {
		t.Fatalf("import lost current custom directory preferences: %+v", restored.Settings)
	}
	for _, path := range target.Paths {
		if _, err = os.Stat(filepath.Join(path, initial.Skills[0].Name, "SKILL.md")); err != nil {
			t.Fatal("import changed deployed files:", err)
		}
	}
	restored.Targets[len(restored.Targets)-1].CustomPaths[0] = "changed-read"
	reopened, err := skillstore.Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.State()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted.Targets[len(persisted.Targets)-1].CustomPaths, target.CustomPaths) {
		t.Fatal("import or read shared mutable custom path arrays")
	}
}
