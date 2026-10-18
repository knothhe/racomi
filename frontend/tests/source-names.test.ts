import assert from "node:assert/strict";
import test from "node:test";
import { applicationSourceDisplayName, hostedSourceDomain, hubResultInstalled, repositoryName, sourceDisplayName } from "../src/source-names.ts";

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

test("application source choices qualify generic skills names and retain custom labels", () => {
  const sources = [
    { name: "skills", repository: "https://github.com/team/skills.git" },
    { name: " SKILLS ", repository: "git@github.com:other/skills.git" },
    { name: "团队设计", repository: "https://github.com/team/design.git" },
    { name: "skills", repository: "/Users/team/skills" },
  ];
  const original = structuredClone(sources);
  assert.deepEqual(sources.map(applicationSourceDisplayName), ["team/skills", "other/skills", "团队设计", "skills"]);
  assert.deepEqual(sources, original);
});

test("local paths and invalid addresses retain their source name", () => {
  for (const repository of ["/Users/team/skills", "./team/skills", "../team/skills", "~/team/skills", "C:\\team\\skills", "C:/team/skills", "file:///Users/team/skills", "https://", "not a repository", "https://example.com/skills.git"]) {
    assert.equal(repositoryName(repository), "", repository);
    assert.equal(sourceDisplayName({ name: "Local skills", repository }), "Local skills", repository);
  }
  assert.equal(sourceDisplayName({ name: "", repository: "/Users/team/skills" }), "/Users/team/skills");
});

test("hosted catalogs display their domain and preserve custom names", () => {
  for (const repository of ["https://open.feishu.cn", "https://open.feishu.cn/.well-known/skills", "https://open.feishu.cn/.well-known/skills/index.json", "https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills", "https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/index.json"]) {
    assert.equal(hostedSourceDomain(repository), "open.feishu.cn", repository);
    assert.equal(sourceDisplayName({ name: "open.feishu.cn", repository }), "open.feishu.cn", repository);
    assert.equal(sourceDisplayName({ name: "团队技能", repository }), "open.feishu.cn · 团队技能", repository);
  }
  assert.equal(sourceDisplayName({ name: "skills", repository: "https://open.feishu.cn/.well-known/skills" }), "open.feishu.cn");
  for (const repository of ["https://github.com/team/skills.git", "git@github.com:team/skills.git", "https://example.com/skills.git", "https://user:password@example.com", "https://example.com/.well-known/skills?token=test"]) {
    assert.equal(hostedSourceDomain(repository), "", repository);
  }
});

test("Hub installed state matches hosted domains and slugs even when names differ", () => {
  const result = { name: "Lark Docs", slug: "open.feishu.cn/lark-doc", source: "open.feishu.cn", installs: 1, url: "https://skills.sh/open.feishu.cn/lark-doc" };
  for (const source of ["https://open.feishu.cn", "https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills"]) {
    assert.equal(hubResultInstalled(result, [{ name: "Feishu Document", source, subpath: "lark-doc" }]), true, source);
    assert.equal(hubResultInstalled(result, [{ name: "Lark Sheets", source, subpath: "lark-sheet" }]), false, source);
  }
  assert.equal(hubResultInstalled(result, [{ name: "Lark Docs", source: "https://other.example.com", subpath: "lark-doc" }]), false);
  assert.equal(hubResultInstalled(result, []), false);
});

test("Hub installed state retains Git matching for shorthand, HTTPS and SSH", () => {
  const result = { name: "Friendly name", slug: "team/skills/review", source: "team/skills", installs: 1, url: "https://skills.sh/team/skills/review" };
  for (const source of ["team/skills", "https://github.com/team/skills.git", "git@github.com:team/skills.git"]) {
    assert.equal(hubResultInstalled(result, [{ name: "review", source, subpath: "skills/review" }]), true, source);
  }
  assert.equal(hubResultInstalled(result, [{ name: "review", source: "https://github.com/other/skills.git", subpath: "skills/review" }]), false);
});
