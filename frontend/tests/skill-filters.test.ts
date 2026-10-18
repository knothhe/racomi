import assert from "node:assert/strict";
import test from "node:test";
import { defaultSkillFilters, filterSkills, pendingSkills, skillStatusIDs, type SkillStatusFilter } from "../src/skill-filters.ts";
import type { Skill, Update } from "../src/api.ts";

const sources = [{ id: "a", name: "Design source", repository: "owner/repo" }, { id: "b", name: "Code source", repository: "owner/other" }] as any;
const skills = [
  { id: "1", name: "skill10", description: "Design", source: "owner/repo", sourceId: "a" },
  { id: "2", name: "skill2", description: "Code", source: "owner/other", sourceId: "b" },
  { id: "3", name: "local", description: "Detached", source: "owner/repo", sourceId: "" },
  { id: "4", name: "orphan", description: "Orphan", source: "owner/repo", sourceId: "missing" },
] as any;
const update = (skillId: string, overrides: Partial<Update> = {}): Update => ({ skillId, revision: "next", summary: "", available: false, removed: false, error: "", ...overrides });
const ids = (overrides = {}, updates = new Set<string>(), pending = new Set<string>()) => filterSkills(skills, sources, { ...defaultSkillFilters(), ...overrides }, skillStatusIDs(skills, [...updates].map((id) => update(id, { available: true })), pending)).map((skill) => skill.id);

test("source filtering distinguishes saved associations from historical origin metadata", () => {
  assert.deepEqual(ids({ source: "a" }), ["1"]);
  assert.deepEqual(ids({ source: "local" }), ["3", "4"]);
  assert.deepEqual(ids({ source: "linked" }), ["2", "1"]);
  assert.deepEqual(ids({ source: "missing" }), []);
});

test("source, search, update/pending status and natural sort compose", () => {
  assert.deepEqual(ids({ source: "linked", query: "source" }), ["2", "1"]);
  assert.deepEqual(ids({ source: "a", query: "design", status: "updates" }, new Set(["1"])), ["1"]);
  assert.deepEqual(ids({ source: "a", status: "pending" }, new Set(), new Set(["2"])), []);
  assert.deepEqual(ids({ source: "linked", sort: "desc" }), ["1", "2"]);
});

test("removed filtering includes saved and freshly detected removals without an update check", () => {
  const library: Skill[] = skills.map((skill: Skill) => ({ ...skill, upstreamRemoved: skill.id === "1" }));
  const filter = (updates: Update[] | null) => filterSkills(library, sources, { ...defaultSkillFilters(), status: "removed" }, skillStatusIDs(library, updates, new Set())).map((skill) => skill.id);
  assert.deepEqual(filter(null), ["1"]);
  assert.deepEqual(filter([update("2", { removed: true })]), ["2", "1"]);
  assert.deepEqual(filter([update("1", { error: "offline" })]), ["1"]);
  const stale = skillStatusIDs(library, [update("1", { available: true }), update("2", { available: true, removed: true })], new Set());
  assert.deepEqual([...stale.updates], []);
  assert.deepEqual([...stale.removed], ["1", "2"]);
});

test("every list status filters consistently, overlapping states stay discoverable and stale IDs are ignored", () => {
  const library: Skill[] = skills.map((skill: Skill) => ({ ...skill, upstreamRemoved: skill.id === "1" }));
  const statuses = skillStatusIDs(library, [
    update("1", { available: true, error: "offline" }),
    update("2", { available: true }),
    update("deleted", { available: true, removed: true, error: "stale" }),
  ], new Set(["1", "2", "deleted"]));
  const expected: Record<SkillStatusFilter, string[]> = {
    all: ["3", "4", "2", "1"], installed: ["3", "4"], updates: ["2"], pending: ["2", "1"], removed: ["1"], error: ["1"],
  };
  for (const status of Object.keys(expected) as SkillStatusFilter[]) {
    const filtered = filterSkills(library, sources, { ...defaultSkillFilters(), status }, statuses);
    assert.deepEqual(filtered.map((skill) => skill.id), expected[status], status);
    if (status !== "all") assert.equal(statuses[status].size, filtered.length, `${status}: count matches the list`);
  }
});

test("failed checks suppress update actions and compose with source, search and ordering", () => {
  const statuses = skillStatusIDs(skills, [update("1", { error: "offline", available: true }), update("2", { error: "skipped" })], new Set());
  const filtered = filterSkills(skills, sources, { ...defaultSkillFilters(), status: "error", source: "linked", query: "source", sort: "desc" }, statuses);
  assert.deepEqual(filtered.map((skill) => skill.id), ["1", "2"]);
  assert.equal(statuses.updates.size, 0);
  assert.deepEqual([...statuses.installed], ["3", "4"]);
  assert.deepEqual(filterSkills(skills, sources, { ...defaultSkillFilters(), status: "removed" }, statuses), []);
});

test("pending skills account for every path, mode, name and retired deployment", () => {
  const skill = { ...skills[0], contentHash: "hash" };
  const deployment = { skillId: skill.id, name: skill.name, contentHash: skill.contentHash, path: "/a", mode: "copy" };
  const target = { path: "/a", paths: ["/a"], enabledSkillIds: [skill.id], enabledPresetIds: [], deployments: [deployment] } as any;
  const pending = (overrides = {}, mode: "copy" | "symlink" = "copy", current = skill) => [...pendingSkills([current], [], [{ ...target, ...overrides }], mode)];
  assert.deepEqual(pending(), []);
  assert.deepEqual(pending({ paths: ["/a", "/b"] }), [skill.id]);
  assert.deepEqual(pending({ paths: ["/b"] }), [skill.id]);
  assert.deepEqual(pending({}, "symlink"), [skill.id]);
  assert.deepEqual(pending({}, "copy", { ...skill, name: "renamed" }), [skill.id]);
  assert.deepEqual(pending({ enabledSkillIds: [] }), [skill.id]);
  assert.deepEqual(pending({ paths: ["/a", "/b"], deployments: [deployment, { ...deployment, path: "/b" }] }), []);
  assert.deepEqual(pending({ deployments: [{ ...deployment, path: undefined, mode: undefined }] }), []);
});
