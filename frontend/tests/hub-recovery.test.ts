import assert from "node:assert/strict";
import test from "node:test";
import { recoverableHubSkill } from "../src/hub-recovery.ts";

const missing = 'skills.sh skill "polish" was not found in the current repository revision; the index may be outdated or the skill may have been renamed or removed; preview the repository to see available skills';

test("repository recovery is offered only for explicit missing-skill diagnostics", () => {
  const source = "https://skills.sh/pbakaus/impeccable/polish";
  assert.equal(recoverableHubSkill(source, missing), "polish");
  for (const error of [
    'skills.sh skill "polish" did not match exactly one repository skill',
    'skills.sh skill "polish" matches multiple repository skills: "skills/a", "skills/b"; specify a skill subdirectory',
    'skills.sh skill "polish" is unavailable: unsupported skill content at "skills/polish": symlink is not supported',
    "cannot clone repository: connection refused",
    "cannot clone repository: Git operation timed out: context deadline exceeded",
    '当前仓库版本中未找到 skills.sh 技能 "polish"。',
    `Error: ${missing}`,
    `${missing}\n`,
    `${missing}\nadditional diagnostic`,
  ]) assert.equal(recoverableHubSkill(source, error), "", error);
});

test("valid Git-backed Hub links retain their URL skill name", () => {
  const inputs = [
    ["https://skills.sh/pbakaus/impeccable/polish", "polish"],
    ["https://www.skills.sh/team/repo/skill-name.v2_1", "skill-name.v2_1"],
    ["skills.sh/team/repo/skill", "skill"],
    ["https://skills.sh/site/repo/skill", "skill"],
    ["https://skills.sh/site/repo_with.dots/skill", "skill"],
    ["https://skills.sh/owner.with.dots/repo/skill", "skill"],
  ];
  for (const [source, name] of inputs) assert.equal(recoverableHubSkill(source, missing), name, source);
  const untrustedErrorName = missing.replace('"polish"', '"<script> other skill"');
  assert.equal(recoverableHubSkill("https://skills.sh/team/repo/from-url", untrustedErrorName), "from-url");
});

test("hosted catalogs and ambiguous, normalized, or malformed URLs cannot recover as Git repositories", () => {
  const sources = [
    "https://skills.sh/open.feishu.cn/lark-doc",
    "https://skills.sh/site/open.feishu.cn/lark-doc",
    "https://skills.sh/site/OPEN.FEISHU.CN/lark-doc",
    "https://open.feishu.cn/.well-known/skills",
    "https://github.com/pbakaus/impeccable",
    "owner/repo",
    "http://skills.sh/owner/repo/skill",
    "ssh://skills.sh/owner/repo/skill",
    "//skills.sh/owner/repo/skill",
    "https://skills.sh.evil.test/owner/repo/skill",
    "https://user:secret@skills.sh/owner/repo/skill",
    "https://skills.sh@evil.test/owner/repo/skill",
    "https://skills.sh:443/owner/repo/skill",
    "https://skills.sh:8443/owner/repo/skill",
    "https://skills.sh/owner/repo/skill?ref=main",
    "https://skills.sh/owner/repo/skill?",
    "https://skills.sh/owner/repo/skill#",
    "https://skills.sh/owner/repo/skill#other",
    "https://skills.sh/owner/repo/%73kill",
    "https://skills.sh/owner/repo/a%2fb",
    "https://skills.sh/owner/repo/../skill",
    "https://skills.sh/owner/./repo/skill",
    "https://skills.sh//owner/repo/skill",
    "https://skills.sh/owner/repo/skill/",
    "https://skills.sh/owner/repo\\skill",
    "https://skills.sh/owner/repo/skill\n",
    " https://skills.sh/owner/repo/skill",
    "https://skills.sh/owner/repo/skill name",
    "https://skills.sh/owner/repo/<script>",
    `https://skills.sh/owner/repo/${"a".repeat(129)}`,
    "https://skills.sh/owner/repo/",
  ];
  for (const source of sources) assert.equal(recoverableHubSkill(source, missing), "", source);
});
