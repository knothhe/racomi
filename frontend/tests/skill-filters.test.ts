import assert from "node:assert/strict";
import test from "node:test";
import { defaultSkillFilters, filterSkills, pendingSkills } from "../src/skill-filters.ts";

const sources = [{ id: "a", name: "Design source", repository: "owner/repo" }, { id: "b", name: "Code source", repository: "owner/other" }] as any;
const skills = [
  { id: "1", name: "skill10", description: "Design", source: "owner/repo", sourceId: "a" },
  { id: "2", name: "skill2", description: "Code", source: "owner/other", sourceId: "b" },
  { id: "3", name: "local", description: "Detached", source: "owner/repo", sourceId: "" },
  { id: "4", name: "orphan", description: "Orphan", source: "owner/repo", sourceId: "missing" },
] as any;
const ids = (overrides = {}, updates = new Set<string>(), pending = new Set<string>()) => filterSkills(skills, sources, { ...defaultSkillFilters(), ...overrides }, updates, pending).map((skill) => skill.id);

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
