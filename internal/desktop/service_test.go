package desktop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestImportUpdateDeployAndPortableRestore(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := filepath.Join(base, "source")
	if err := os.MkdirAll(repository, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repository
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeSkill := func(name, body string) {
		t.Helper()
		directory := filepath.Join(repository, "skills", name)
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: fixture\n---\n"+body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "racomi test")
	git("config", "user.email", "test@example.invalid")
	writeSkill("alpha", "version one")
	writeSkill("beta", "stable")
	git("add", ".")
	git("-c", "core.hooksPath=/dev/null", "commit", "-m", "initial")
	service, err := New(filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	preview, err := service.Preview(ctx, repository, "", "")
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, candidate := range preview.Candidates {
		paths = append(paths, candidate.Subpath)
	}
	app, err := service.Install(preview.ID, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.State.Skills) != 2 {
		t.Fatal("repository skills not imported")
	}
	ids := map[string]string{}
	for _, skill := range app.State.Skills {
		ids[skill.Name] = skill.ID
	}
	app, err = service.SavePreset(skillstore.Preset{Name: "基础", SkillIDs: []string{ids["alpha"]}})
	if err != nil {
		t.Fatal(err)
	}
	first := app.State.Presets[0].ID
	app, err = service.SavePreset(skillstore.Preset{Name: "开发", SkillIDs: []string{ids["alpha"], ids["beta"]}})
	if err != nil {
		t.Fatal(err)
	}
	second := app.State.Presets[1].ID
	workspace := filepath.Join(base, "workspace")
	os.MkdirAll(filepath.Join(workspace, ".agents", "skills", "unmanaged"), 0755)
	app, err = service.SaveTarget(skillstore.Target{Name: "测试仓库", Kind: "project", Path: workspace})
	if err != nil {
		t.Fatal(err)
	}
	target := app.State.Targets[0]
	if target.Path != filepath.Join(workspace, ".agents", "skills") {
		t.Fatalf("unexpected target path %s", target.Path)
	}
	if _, err = service.SetTargetPresets(target.ID, []string{first, second}); err != nil {
		t.Fatal(err)
	}
	plan, err := service.PlanTarget(target.ID)
	if err != nil || len(plan.Add) != 2 {
		t.Fatalf("unexpected add plan: %+v %v", plan, err)
	}
	if _, err = service.ApplyTarget(target.ID); err != nil {
		t.Fatal(err)
	}
	alphaFile := filepath.Join(target.Path, "alpha", "SKILL.md")
	before, _ := os.ReadFile(alphaFile)
	writeSkill("alpha", "version two")
	git("add", ".")
	git("-c", "core.hooksPath=/dev/null", "commit", "-m", "update alpha")
	updates, err := service.CheckUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	available := 0
	for _, update := range updates {
		if update.Error != "" {
			t.Fatal(update.Error)
		}
		if update.Available {
			available++
			if update.SkillID != ids["alpha"] {
				t.Fatal("unrelated skill marked for update")
			}
		}
	}
	if available != 1 {
		t.Fatalf("expected one skill update: %+v", updates)
	}
	if _, err = service.UpdateSkill(ctx, ids["alpha"]); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(alphaFile)
	if string(after) != string(before) {
		t.Fatal("store update automatically changed target")
	}
	plan, err = service.PlanTarget(target.ID)
	if err != nil || len(plan.Update) != 1 {
		t.Fatalf("unexpected update plan: %+v %v", plan, err)
	}
	if _, err = service.ApplyTarget(target.ID); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(alphaFile)
	if !strings.Contains(string(after), "version two") {
		t.Fatal("manual application did not update target")
	}
	archive := filepath.Join(base, "portable.zip")
	if _, err = service.ExportBackup(archive); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SetTargetPresets(target.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(alphaFile, []byte("local modification"), 0644); err != nil {
		t.Fatal(err)
	}
	plan, err = service.PlanTarget(target.ID)
	if err != nil || len(plan.Conflicts) == 0 {
		t.Fatalf("local change not detected: %+v %v", plan, err)
	}
	if _, err = service.ApplyTarget(target.ID); err == nil {
		t.Fatal("conflicting deployment was accepted")
	}
	if _, err = os.Stat(filepath.Join(target.Path, "beta", "SKILL.md")); err != nil {
		t.Fatal("failed preflight partially removed target")
	}
	if err = os.WriteFile(alphaFile, after, 0644); err != nil {
		t.Fatal(err)
	}
	app, err = service.ImportBackup(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.State.Skills) != 2 || len(app.State.Presets) != 2 || len(app.State.Targets) != 1 {
		t.Fatal("backup lost store or local target settings")
	}
	if len(app.State.Targets[0].EnabledPresetIDs) != 0 {
		t.Fatal("restoration silently re-enabled presets")
	}
	still, _ := os.ReadFile(alphaFile)
	if string(still) != string(after) {
		t.Fatal("backup restore changed deployed files")
	}
	if _, err = service.ApplyTarget(target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(target.Path, "alpha")); !os.IsNotExist(err) {
		t.Fatal("disabling all presets did not remove managed skill")
	}
	if _, err = os.Stat(filepath.Join(target.Path, "unmanaged")); err != nil {
		t.Fatal("unmanaged content was removed")
	}
}

func TestImportUsesCanonicalDataRootWhenConfiguredThroughSymlink(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	physicalRoot := filepath.Join(base, "physical-data")
	if err := os.Mkdir(physicalRoot, 0700); err != nil {
		t.Fatal(err)
	}
	configuredRoot := filepath.Join(base, "configured-data")
	if err := os.Symlink(physicalRoot, configuredRoot); err != nil {
		t.Fatal(err)
	}
	repository := filepath.Join(base, "repository")
	if err := os.Mkdir(repository, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", repository}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "racomi test")
	git("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "SKILL.md"), []byte("---\nname: alpha\ndescription: linked data root\n---\ncontent"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "SKILL.md")
	git("-c", "core.hooksPath=/dev/null", "commit", "-m", "initial")
	service, err := New(configuredRoot)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(context.Background(), repository, "", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.Install(preview.ID, []string{"."})
	if err != nil {
		t.Fatalf("preview succeeds but install rejects the configured data alias: %v", err)
	}
	if app.DataDir != physicalRoot || len(app.State.Skills) != 1 {
		t.Fatalf("import did not use the canonical data directory: %+v", app)
	}
}

func TestReadSkillDocumentVerifiesIndexedObject(t *testing.T) {
	const document = "---\nname: document\ndescription: 阅读技能说明\n---\n# 使用说明\n安全阅读。\n"
	tests := []struct {
		name    string
		content string
		mutate  func(t *testing.T, object string)
		wantErr string
	}{
		{name: "valid UTF-8 document", content: document},
		{name: "size limit", content: strings.Repeat("x", (1<<20)+1), wantErr: "1 MiB"},
		{name: "tampered document", content: document, mutate: func(t *testing.T, object string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(object, "SKILL.md"), []byte("changed"), 0644); err != nil {
				t.Fatal(err)
			}
		}, wantErr: "校验失败"},
		{name: "tampered supporting file", content: document, mutate: func(t *testing.T, object string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(object, "extra.txt"), []byte("changed"), 0644); err != nil {
				t.Fatal(err)
			}
		}, wantErr: "校验失败"},
		{name: "symlink document", content: document, mutate: func(t *testing.T, object string) {
			t.Helper()
			filename := filepath.Join(object, "SKILL.md")
			if err := os.Remove(filename); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/etc/hosts", filename); err != nil {
				t.Fatal(err)
			}
		}, wantErr: "普通文件"},
		{name: "invalid UTF-8", content: "\xff", wantErr: "UTF-8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(base, "skill")
			if err = os.Mkdir(source, 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(test.content), 0644); err != nil {
				t.Fatal(err)
			}
			service, err := New(filepath.Join(base, "data"))
			if err != nil {
				t.Fatal(err)
			}
			skills, err := service.store.Install([]skillstore.Candidate{{Path: source, Skill: skillstore.Skill{Name: "document", Source: "https://github.com/example/skills", Ref: "main", Subpath: "."}}})
			if err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(t, filepath.Join(service.store.Root(), "objects", skills[0].ContentHash))
			}
			content, err := service.ReadSkillDocument(skills[0].ID)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) || content != "" {
					t.Fatalf("expected %q and no document, got %q, %v", test.wantErr, content, err)
				}
				return
			}
			if err != nil || content != test.content {
				t.Fatalf("unexpected document %q, %v", content, err)
			}
		})
	}
}

func TestReadSkillDocumentRejectsUnknownIDs(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "unknown", "../../etc/hosts", "/etc/hosts"} {
		content, err := service.ReadSkillDocument(id)
		if err == nil || content != "" {
			t.Fatalf("unknown skill %q was read: %q, %v", id, content, err)
		}
	}
}

func TestSetThemeWithoutAttachedApplication(t *testing.T) {
	service := &Service{}
	for _, theme := range []string{"system", "light", "dark"} {
		if err := service.SetTheme(theme); err != nil {
			t.Fatalf("valid theme %q: %v", theme, err)
		}
		if service.theme != theme {
			t.Fatalf("theme was not remembered: %q", service.theme)
		}
	}
	for _, theme := range []string{"", "Dark", " light ", "unknown"} {
		if err := service.SetTheme(theme); err == nil {
			t.Fatalf("invalid theme %q accepted", theme)
		}
		if service.theme != "dark" {
			t.Fatal("invalid input changed theme")
		}
	}
	if err := service.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
}
