package skills

import (
	"reflect"
	"strings"
	"testing"
)

func TestSkippedCatalogEntryCannotBeSynchronizedOrRelinked(t *testing.T) {
	store, base := newStore(t)
	c := candidate(t, base, "alpha", "installed version")
	source, err := store.SaveSource(Source{Name: "Team", Repository: c.Skill.Source, Ref: c.Skill.Ref, Subpath: ".", Mode: SourceModeAll})
	if err != nil {
		t.Fatal(err)
	}
	source = refreshedSource(t, store, source, strings.Repeat("a", 40), c)
	candidates := sourceCandidates(source, c)
	if err = store.SyncSource(source.ID, source.Revision, candidates); err != nil {
		t.Fatal(err)
	}
	source.Catalog[0].SkipReason = "symlink is not allowed in skill content: helper"
	source.Warnings = []string{"ignored repository entry: AGENTS.md"}
	source.Catalog = append(source.Catalog, SourceSkill{Name: "unreadable-skill", Subpath: "broken", SkipReason: "missing YAML frontmatter"})
	if err = store.RecordSourceRefresh(source); err != nil {
		t.Fatal(err)
	}
	before := state(t, store)
	if err = store.SyncSource(source.ID, source.Revision, candidates); err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatal("skipped skill accepted through direct store sync", err)
	}
	if err = store.RelinkSourceSkill(before.Skills[0].ID, source.ID, candidates[0]); err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatal("skipped skill accepted through relinking", err)
	}
	if after := state(t, store); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected synchronization changed store state")
	}
	if before.Skills[0].UpstreamRemoved {
		t.Fatal("skipped skill was marked removed")
	}
	copy := cloneState(before)
	copy.Sources[0].Warnings[0] = "changed"
	if before.Sources[0].Warnings[0] != source.Warnings[0] {
		t.Fatal("snapshot warnings alias live state")
	}
}
