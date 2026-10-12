import assert from "node:assert/strict";
import test from "node:test";
import { repositoryName, sourceDisplayName } from "../src/source-names.ts";

test("repository labels distinguish owners across shorthand, HTTPS and SSH addresses", () => {
  for (const repository of ["team/skills", "team/skills.git", "https://github.com/team/skills.git", "git@github.com:team/skills.git", "ssh://git@example.com:2222/team/skills.git/"]) {
    assert.equal(repositoryName(repository), "team/skills", repository);
    assert.equal(sourceDisplayName({ name: "skills", repository }), "team/skills", repository);
  }
  assert.equal(sourceDisplayName({ name: "skills", repository: "https://github.com/other/skills.git" }), "other/skills");
  assert.equal(repositoryName("https://git.example.com/group/subgroup/skills.git"), "group/subgroup/skills");
});

test("custom source names remain visible without changing saved metadata", () => {
  const source = { name: "团队技能", repository: "https://github.com/team/skills.git" };
  assert.equal(sourceDisplayName(source), "team/skills · 团队技能");
  assert.deepEqual(source, { name: "团队技能", repository: "https://github.com/team/skills.git" });
  assert.equal(sourceDisplayName({ ...source, name: "team/skills" }), "team/skills");
  assert.equal(sourceDisplayName({ ...source, name: "" }), "team/skills");
});

test("local paths and invalid addresses retain their source name", () => {
  for (const repository of ["/Users/team/skills", "./team/skills", "../team/skills", "~/team/skills", "C:\\team\\skills", "C:/team/skills", "file:///Users/team/skills", "https://", "not a repository", "https://example.com/skills.git"]) {
    assert.equal(repositoryName(repository), "", repository);
    assert.equal(sourceDisplayName({ name: "Local skills", repository }), "Local skills", repository);
  }
  assert.equal(sourceDisplayName({ name: "", repository: "/Users/team/skills" }), "/Users/team/skills");
});
