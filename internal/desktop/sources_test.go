package desktop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	skillstore "racomi/internal/skills"
)

type sourceFixture struct {
	t          *testing.T
	repo, root string
	service    *Service
}

func newSourceFixture(t *testing.T) *sourceFixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &sourceFixture{t: t, repo: filepath.Join(base, "repository"), root: filepath.Join(base, "data")}
	if err := os.MkdirAll(f.repo, 0755); err != nil {
		t.Fatal(err)
	}
	f.git("init", "-b", "main")
	f.write("skills/alpha", "alpha", "alpha v1")
	f.write("skills/beta", "beta", "beta v1")
	f.commit("initial skills")
	f.service, err = New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *sourceFixture) git(args ...string) {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Source test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Source test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
func (f *sourceFixture) write(subpath, name, body string) {
	f.t.Helper()
	dir := filepath.Join(f.repo, filepath.FromSlash(subpath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: source fixture\n---\n"+body), 0644); err != nil {
		f.t.Fatal(err)
	}
}
func (f *sourceFixture) commit(message string) {
	f.git("add", "-A")
	f.git("-c", "core.hooksPath=/dev/null", "commit", "-m", message)
}
func sourceSkill(t *testing.T, state AppState, name string) skillstore.Skill {
	t.Helper()
	for _, skill := range state.State.Skills {
		if skill.Name == name {
			return skill
		}
	}
	t.Fatalf("missing skill %q: %+v", name, state.State.Skills)
	return skillstore.Skill{}
}

func TestSourceSelectedAllRemovalAndRelink(t *testing.T) {
	f := newSourceFixture(t)
	ctx := context.Background()
	state, err := f.service.SaveSource(skillstore.Source{Name: "Team", Repository: f.repo, Mode: "selected"})
	if err != nil {
		t.Fatal(err)
	}
	id := state.State.Sources[0].ID
	if len(state.State.Skills) != 0 || state.State.Sources[0].Revision != "" {
		t.Fatal("registration installed or scanned skills")
	}
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	source := state.State.Sources[0]
	if source.LastError != "" || source.Ref != "main" || len(source.Catalog) != 2 || len(state.State.Skills) != 0 {
		t.Fatalf("bad first refresh: %+v", source)
	}
	state, err = f.service.SyncSource(ctx, id, source.Revision, []string{"skills/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	alpha := sourceSkill(t, state, "alpha")
	if len(state.State.Skills) != 1 {
		t.Fatal("selected mode installed unselected skills")
	}
	state, err = f.service.SavePreset(skillstore.Preset{Name: "Daily", SkillIDs: []string{alpha.ID}})
	if err != nil {
		t.Fatal(err)
	}
	presetID := state.State.Presets[0].ID
	project := filepath.Join(filepath.Dir(f.root), "project")
	if err = os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	state, err = f.service.SaveTarget(skillstore.Target{Name: "Project", Kind: "project", Path: project})
	if err != nil {
		t.Fatal(err)
	}
	target := state.State.Targets[0]
	if _, err = f.service.SetTargetPresets(target.ID, []string{presetID}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ApplyTarget(target.ID); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(target.Path, "alpha", "SKILL.md")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	f.write("skills/alpha", "alpha", "alpha v2")
	f.write("skills/gamma", "gamma", "new gamma")
	f.git("rm", "-r", "skills/beta")
	f.commit("update alpha, add gamma, remove beta")
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	source = state.State.Sources[0]
	if sourceSkill(t, state, "alpha").ContentHash != alpha.ContentHash || len(state.State.Skills) != 1 || state.State.Targets[0].Pending {
		t.Fatal("refresh changed installed content")
	}
	if _, err = f.service.SyncSource(ctx, id, "stale-version", []string{"skills/alpha"}); err == nil {
		t.Fatal("accepted stale synchronization plan")
	}
	state, err = f.service.SyncSource(ctx, id, source.Revision, []string{"skills/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	updated := sourceSkill(t, state, "alpha")
	if updated.ID != alpha.ID || updated.InstalledAt != alpha.InstalledAt || updated.ContentHash == alpha.ContentHash || !state.State.Targets[0].Pending || len(state.State.Skills) != 1 {
		t.Fatal("selected update identity or pending state is incorrect")
	}
	current, _ := os.ReadFile(file)
	if string(current) != string(before) {
		t.Fatal("sync wrote target directory")
	}
	source = state.State.Sources[0]
	source.Mode = "all"
	state, err = f.service.SaveSource(source)
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.service.SyncSource(ctx, id, source.Revision, nil)
	if err != nil || len(state.State.Skills) != 2 {
		t.Fatalf("all mode did not install gamma: %v %+v", err, state.State.Skills)
	}
	f.write("skills/delta", "delta", "future addition")
	f.git("rm", "-r", "skills/alpha")
	f.commit("add delta, remove installed alpha")
	updates, err := f.service.CheckUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, update := range updates {
		if update.SkillID == alpha.ID {
			removed = update.Removed && !update.Available && update.Error == ""
		}
	}
	if !removed {
		t.Fatalf("deletion not classified: %+v", updates)
	}
	state, _ = f.service.GetState()
	if len(state.State.Skills) != 2 || !sourceSkill(t, state, "alpha").UpstreamRemoved || state.State.Presets[0].SkillIDs[0] != alpha.ID {
		t.Fatal("refresh removed local copy or preset association")
	}
	source = state.State.Sources[0]
	state, err = f.service.SyncSource(ctx, id, source.Revision, []string{})
	if err != nil || len(state.State.Skills) != 3 || !sourceSkill(t, state, "alpha").UpstreamRemoved {
		t.Fatalf("all sync failed future addition or deleted local skill: %v", err)
	}
	current, _ = os.ReadFile(file)
	if string(current) != string(before) {
		t.Fatal("upstream deletion or all sync changed target")
	}
	// A moved/renamed skill is explicitly linked to its retained local identity.
	f.write("relocated/alpha", "omega", "moved alpha")
	f.commit("move alpha to omega")
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.service.RelinkSourceSkill(ctx, alpha.ID, id, "relocated/alpha")
	if err != nil {
		t.Fatal(err)
	}
	omega := sourceSkill(t, state, "omega")
	if omega.ID != alpha.ID || omega.UpstreamRemoved || state.State.Presets[0].SkillIDs[0] != alpha.ID {
		t.Fatal("relink lost skill identity")
	}
	if _, err = f.service.ApplyTarget(target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("manual rename did not remove old deployment")
	}
	if data, err := os.ReadFile(filepath.Join(target.Path, "omega", "SKILL.md")); err != nil || !strings.Contains(string(data), "moved alpha") {
		t.Fatal("manual relink deployment failed", err)
	}
}

func TestSourcePinnedSyncFailureAndEmptyCatalog(t *testing.T) {
	f := newSourceFixture(t)
	ctx := context.Background()
	state, err := f.service.SaveSource(skillstore.Source{Name: "Scoped", Repository: f.repo, Ref: "main", Subpath: "skills", Mode: "all"})
	if err != nil {
		t.Fatal(err)
	}
	id := state.State.Sources[0].ID
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	reviewed := state.State.Sources[0]
	f.write("skills/alpha", "alpha", "unreviewed alpha v2")
	f.commit("advance after review")
	reopened, err := New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	state, err = reopened.SyncSource(ctx, id, reviewed.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	alpha := sourceSkill(t, state, "alpha")
	if alpha.Revision != reviewed.Revision {
		t.Fatal("synchronization followed newer head")
	}
	doc, err := reopened.ReadSkillDocument(alpha.ID)
	if err != nil || !strings.Contains(doc, "alpha v1") {
		t.Fatal("installed unreviewed content", err)
	}
	// Network/source failures retain the complete last successful catalog.
	lastCatalog := state.State.Sources[0].Catalog
	if err = os.Rename(f.repo, f.repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	state, err = reopened.RefreshSource(ctx, id)
	if err != nil || state.State.Sources[0].LastError == "" {
		t.Fatal("refresh failure status not persisted", err)
	}
	if !reflect.DeepEqual(state.State.Sources[0].Catalog, lastCatalog) {
		t.Fatal("failure changed catalog")
	}
	for _, skill := range state.State.Skills {
		if skill.UpstreamRemoved {
			t.Fatal("failure marked skill removed")
		}
	}
	if _, err = reopened.SyncSource(ctx, id, reviewed.Revision, nil); err == nil {
		t.Fatal("failed refresh still allowed sync")
	}
	if err = os.Rename(f.repo+"-offline", f.repo); err != nil {
		t.Fatal(err)
	}
	f.git("rm", "-r", "skills")
	f.commit("remove complete scope")
	state, err = reopened.RefreshSource(ctx, id)
	if err != nil || state.State.Sources[0].LastError != "" {
		t.Fatalf("empty scope not a valid catalog: %v %+v", err, state.State.Sources)
	}
	for _, skill := range state.State.Skills {
		if !skill.UpstreamRemoved {
			t.Fatal("empty scope did not mark removed")
		}
	}
	if len(state.State.Skills) != 2 {
		t.Fatal("empty catalog deleted local skills")
	}
	state, err = reopened.DeleteSource(id)
	if err != nil || len(state.State.Sources) != 0 || len(state.State.Skills) != 2 {
		t.Fatal("deleting source deleted content", err)
	}
	for _, skill := range state.State.Skills {
		if skill.SourceID != "" || skill.UpstreamRemoved {
			t.Fatal("deleting source did not detach skill")
		}
	}
	if _, err = reopened.UpdateSkill(ctx, alpha.ID); err == nil {
		t.Fatal("detached skill still followed deleted source")
	}
}

func TestDiscoveryReusesScopedSourceAndRejectsStalePreview(t *testing.T) {
	f := newSourceFixture(t)
	ctx := context.Background()
	state, err := f.service.SaveSource(skillstore.Source{Name: "Scoped", Repository: f.repo, Ref: "main", Subpath: "skills", Mode: "selected"})
	if err != nil {
		t.Fatal(err)
	}
	id := state.State.Sources[0].ID
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.service.Preview(ctx, f.repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.service.Install(first.ID, []string{"skills/alpha"})
	if err != nil {
		t.Fatal("discovery did not reuse scoped source", err)
	}
	if len(state.State.Sources) != 1 || sourceSkill(t, state, "alpha").SourceID != id {
		t.Fatal("discovery created a conflicting source")
	}
	stale, err := f.service.Preview(ctx, f.repo, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	f.write("skills/gamma", "gamma", "gamma v1")
	f.commit("add gamma for discovery")
	latest, err := f.service.Preview(ctx, f.repo, "main", "skills/gamma")
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.service.Install(latest.ID, []string{"skills/gamma"})
	if err != nil {
		t.Fatal("newer discovery preview could not advance catalog", err)
	}
	if len(state.State.Skills) != 2 || len(state.State.Sources[0].Catalog) != 3 || state.State.Sources[0].Mode != "selected" {
		t.Fatal("discovery lost scope inventory or changed subscription mode")
	}
	if _, err = f.service.Install(stale.ID, []string{"skills/beta"}); err == nil {
		t.Fatal("stale discovery rolled back source inventory")
	}
	state, _ = f.service.GetState()
	if len(state.State.Skills) != 2 || state.State.Sources[0].Revision != latest.Revision {
		t.Fatal("rejected stale preview changed state")
	}
}

func TestSingleSkillUpdateUsesReviewedSourceVersion(t *testing.T) {
	f := newSourceFixture(t)
	ctx := context.Background()
	preview, err := f.service.Preview(ctx, f.repo, "", "")
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.service.Install(preview.ID, []string{"skills/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	alpha := sourceSkill(t, state, "alpha")
	f.write("skills/alpha", "alpha", "reviewed alpha v2")
	f.commit("review alpha v2")
	updates, err := f.service.CheckUpdates(ctx)
	if err != nil || len(updates) != 1 || !updates[0].Available {
		t.Fatal("expected alpha update", err, updates)
	}
	f.write("skills/alpha", "alpha", "unreviewed alpha v3")
	f.commit("advance alpha after review")
	state, err = f.service.UpdateSkill(ctx, alpha.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := f.service.ReadSkillDocument(alpha.ID)
	if err != nil || !strings.Contains(doc, "reviewed alpha v2") || strings.Contains(doc, "unreviewed alpha v3") {
		t.Fatal("single skill update followed unreviewed head", err)
	}
	if sourceSkill(t, state, "alpha").Revision != updates[0].Revision {
		t.Fatal("single skill update did not pin reviewed version")
	}
}

func TestRefreshDoesNotHideStorageValidationFailureBehindOldError(t *testing.T) {
	f := newSourceFixture(t)
	ctx := context.Background()
	if _, err := f.service.SaveSource(skillstore.Source{Name: "Main", Repository: f.repo, Ref: "main", Mode: "selected"}); err != nil {
		t.Fatal(err)
	}
	state, err := f.service.SaveSource(skillstore.Source{Name: "Default", Repository: f.repo, Mode: "selected"})
	if err != nil {
		t.Fatal(err)
	}
	id := state.State.Sources[1].ID
	if err = os.Rename(f.repo, f.repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	state, err = f.service.RefreshSource(ctx, id)
	if err != nil || state.State.Sources[1].LastError == "" {
		t.Fatal("failed fetch status not recorded", err)
	}
	if err = os.Rename(f.repo+"-offline", f.repo); err != nil {
		t.Fatal(err)
	}
	// Binding the default source to main overlaps the first registration.
	// The prior LastError must not hide this new validation/storage failure.
	if _, err = f.service.RefreshSource(ctx, id); err == nil {
		t.Fatal("catalog validation failure was swallowed behind prior LastError")
	}
}
