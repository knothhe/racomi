import assert from "node:assert/strict";
import { test } from "node:test";
import { matchesProject, matchesProjectSkill, projectSkillApplied } from "../src/project-browser.ts";

test("project search matches all terms across names, roots, directories and selected skills or presets", () => {
  const project = { name: "Web Console", projectRoot: "/Workspace/Team/console", path: "/Workspace/Team/console/.agents/skills", paths: ["/Workspace/Team/console/.claude/skills"] };
  assert.equal(matchesProject(project, "  TEAM   console ", [], []), true);
  assert.equal(matchesProject(project, ".claude", [], []), true);
  assert.equal(matchesProject(project, "WEB review", ["Code review"], []), true);
  assert.equal(matchesProject(project, "web daily", [], ["Daily development"]), true);
  assert.equal(matchesProject(project, "web mobile", [], []), false);
  assert.equal(matchesProject(project, "   ", [], []), true);
});

test("selected skill filtering includes skills supplied by presets and never mutates the selection", () => {
  const selected = new Set(["direct", "preset"]);
  const skills = [
    { id: "direct", name: "Review", description: "Code correctness" },
    { id: "preset", name: "Design", description: "Accessible web interface" },
    { id: "other", name: "Testing", description: "Web browser checks" },
  ];
  assert.deepEqual(skills.filter((skill) => matchesProjectSkill(skill, "WEB", true, selected)).map((skill) => skill.id), ["preset"]);
  assert.deepEqual(skills.filter((skill) => matchesProjectSkill(skill, "web", false, selected)).map((skill) => skill.id), ["preset", "other"]);
  assert.deepEqual(skills.filter((skill) => matchesProjectSkill(skill, "missing", false, selected)), []);
  assert.deepEqual([...selected], ["direct", "preset"]);
});

test("a project skill is applied only when every selected directory has its current version", () => {
  const skill = { id: "review", name: "Code review", contentHash: "current" };
  const paths = ["/project/.agents/skills", "/project/.claude/skills"];
  const deployed = paths.map((path) => ({ skillId: skill.id, name: skill.name, contentHash: skill.contentHash, path, mode: "copy" as const }));
  assert.equal(projectSkillApplied(skill, paths, deployed, "copy"), true);
  assert.equal(projectSkillApplied(skill, paths, deployed.slice(0, 1), "copy"), false);
  assert.equal(projectSkillApplied(skill, paths, [deployed[0], { ...deployed[1], contentHash: "previous" }], "copy"), false);
  assert.equal(projectSkillApplied(skill, paths, [deployed[0], { ...deployed[1], skillId: "other" }], "copy"), false);
  assert.equal(projectSkillApplied(skill, paths, [deployed[0], { ...deployed[1], path: "/other/project/skills" }], "copy"), false);
  // A retired directory is handled by the project plan; it does not erase a
  // current deployment in each directory that remains selected.
  assert.equal(projectSkillApplied(skill, paths, [...deployed, { ...deployed[0], path: "/retired/skills", contentHash: "previous" }], "copy"), true);
});

test("changing deployment mode leaves the skill pending until every directory uses that mode", () => {
  const skill = { id: "review", name: "Code review", contentHash: "current" };
  const paths = ["/project/.agents/skills", "/project/.codex/skills"];
  const copied = paths.map((path) => ({ skillId: skill.id, name: skill.name, contentHash: skill.contentHash, path, mode: "copy" as const }));
  const linked = copied.map((item) => ({ ...item, mode: "symlink" as const }));
  assert.equal(projectSkillApplied(skill, paths, copied, "symlink"), false);
  assert.equal(projectSkillApplied(skill, paths, [linked[0], copied[1]], "symlink"), false);
  assert.equal(projectSkillApplied(skill, paths, linked, "symlink"), true);
  assert.equal(projectSkillApplied(skill, paths, linked, "copy"), false);
});

test("a skill rename needs applying even when its content hash stays the same", () => {
  const skill = { id: "review", name: "review-v2", contentHash: "unchanged" };
  const paths = ["/project/.agents/skills", "/project/.claude/skills"];
  const deployed = paths.map((path) => ({ skillId: skill.id, name: "review-v1", contentHash: skill.contentHash, path }));
  assert.equal(projectSkillApplied(skill, paths, deployed, "copy"), false);
  assert.equal(projectSkillApplied(skill, paths, [{ ...deployed[0], name: skill.name }, deployed[1]], "copy"), false);
  assert.equal(projectSkillApplied(skill, paths, deployed.map((item) => ({ ...item, name: skill.name })), "copy"), true);
});

test("legacy deployments default to a copy in the first selected directory", () => {
  const skill = { id: "review", name: "Code review", contentHash: "current" };
  const first = "/project/.agents/skills", second = "/project/.claude/skills";
  const legacy = { skillId: skill.id, name: skill.name, contentHash: skill.contentHash };
  assert.equal(projectSkillApplied(skill, [first], [legacy], "copy"), true);
  assert.equal(projectSkillApplied(skill, [first], [legacy], "symlink"), false);
  assert.equal(projectSkillApplied(skill, [first, second], [legacy], "copy"), false);
  assert.equal(projectSkillApplied(skill, [first, second], [legacy, { ...legacy, path: second }], "copy"), true);
});

test("an empty directory selection or missing deployment cannot report an applied skill", () => {
  const skill = { id: "review", name: "Code review", contentHash: "current" };
  assert.equal(projectSkillApplied(skill, [], [], "copy"), false);
  assert.equal(projectSkillApplied(skill, [], [{ skillId: skill.id, name: skill.name, contentHash: skill.contentHash }], "copy"), false);
  assert.equal(projectSkillApplied(skill, ["/project/.agents/skills"], [], "copy"), false);
});

test("project sort preserves manual order, sorts names naturally and uses newest timestamps first", async () => {
  const { sortProjects } = await import("../src/project-browser.ts");
  const projects = [
    { name: "Project 10", createdAt: "2026-10-01T00:00:00Z", updatedAt: "2026-10-20T00:00:00Z" },
    { name: "Project 2", createdAt: "2026-10-02T00:00:00Z", updatedAt: "2026-10-19T00:00:00Z" },
    { name: "Alpha", updatedAt: "invalid" },
  ];
  assert.deepEqual(sortProjects(projects, "manual", "en"), projects);
  assert.deepEqual(sortProjects(projects, "name-asc", "en").map((item) => item.name), ["Alpha", "Project 2", "Project 10"]);
  assert.deepEqual(sortProjects(projects, "name-desc", "en").map((item) => item.name), ["Project 10", "Project 2", "Alpha"]);
  assert.deepEqual(sortProjects(projects, "created", "en").map((item) => item.name), ["Project 2", "Project 10", "Alpha"]);
  assert.deepEqual(sortProjects(projects, "updated", "en"), projects);
  assert.equal(projects[0].name, "Project 10");
});

test("moving a project retains hidden projects and ignores invalid drops", async () => {
  const { moveProject } = await import("../src/project-browser.ts");
  const order = ["a", "hidden", "b", "c"];
  assert.deepEqual(moveProject(order, "c", "a"), ["c", "a", "hidden", "b"]);
  assert.deepEqual(moveProject(order, "a", "b", true), ["hidden", "b", "a", "c"]);
  assert.deepEqual(moveProject(order, "a", "missing"), order);
  assert.deepEqual(moveProject(order, "a", "a"), order);
  assert.deepEqual(order, ["a", "hidden", "b", "c"]);
});
