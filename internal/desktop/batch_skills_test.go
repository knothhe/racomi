package desktop

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBatchUpdateUsesReviewedRevisionsAndOnlySelectedSkills(t *testing.T) {
	f, other := newSourceFixture(t), newSourceFixture(t)
	ctx := context.Background()
	for i, fixture := range []*sourceFixture{f, other} {
		preview, err := f.service.Preview(ctx, fixture.repo, "", "")
		if err != nil {
			t.Fatal(err)
		}
		path := []string{"skills/alpha", "skills/beta"}[i]
		if _, err = f.service.Install(preview.ID, []string{path}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := f.service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	alpha, beta := sourceSkill(t, state, "alpha"), sourceSkill(t, state, "beta")
	for i, fixture := range []*sourceFixture{f, other} {
		source := state.State.Sources[i]
		source.Mode = "all"
		if _, err = f.service.SaveSource(source); err != nil {
			t.Fatal(err)
		}
		fixture.write("skills/alpha", "alpha", "alpha reviewed")
		fixture.write("skills/beta", "beta", "beta reviewed")
		fixture.commit("reviewed changes")
		if _, err = f.service.RefreshSource(ctx, source.ID); err != nil {
			t.Fatal(err)
		}
		fixture.write("skills/alpha", "alpha", "alpha newer unreviewed")
		fixture.write("skills/beta", "beta", "beta newer unreviewed")
		fixture.commit("newer unreviewed changes")
	}
	before, err := f.service.GetState()
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.service.UpdateSkills(ctx, []string{alpha.ID, beta.ID, alpha.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.State.Skills) != 2 || len(state.Snapshots) != len(before.Snapshots)+1 {
		t.Fatal("batch installed unselected skills or created multiple snapshots")
	}
	for _, skill := range state.State.Skills {
		document, err := f.service.ReadSkillDocument(skill.ID)
		if err != nil || !strings.Contains(document, skill.Name+" reviewed") || strings.Contains(document, "unreviewed") {
			t.Fatalf("batch update followed an unreviewed upstream revision: %q %v", document, err)
		}
	}
	if sourceSkill(t, state, "alpha").ID != alpha.ID || sourceSkill(t, state, "beta").ID != beta.ID || !reflect.DeepEqual(before.State.Presets, state.State.Presets) {
		t.Fatal("batch update changed skill identity or presets")
	}
	index, err := os.ReadFile(filepath.Join(f.root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.UpdateSkills(ctx, []string{alpha.ID, "missing"}); err == nil {
		t.Fatal("batch accepted a missing selection")
	}
	after, err := os.ReadFile(filepath.Join(f.root, "state.json"))
	if err != nil || string(index) != string(after) {
		t.Fatal("failed batch changed the index", err)
	}
	state, err = f.service.UpdateSkills(ctx, []string{alpha.ID, beta.ID})
	if err != nil || len(state.Snapshots) != len(before.Snapshots)+1 {
		t.Fatal("repeating an unchanged update created another snapshot", err)
	}
}
