import assert from "node:assert/strict";
import test from "node:test";
import type { Source, Skill, SourceSkill } from "../src/api.ts";
import { catalogEntries, sourceStatus, sourceStatusDetails, sourceSyncPaths, summarizeSource } from "../src/source-catalog.ts";

const entry = (path: string, overrides: Partial<SourceSkill> = {}): SourceSkill => ({ name: path, description: path, subpath: path, revision: "latest", contentHash: "latest", removed: false, new: false, ...overrides });
const local = (path: string, overrides: Partial<Skill> = {}): Skill => ({ ...entry(path), id: path, source: "owner/repo", sourceId: "source", ref: "main", installedAt: "", updatedAt: "", upstreamRemoved: false, ...overrides });
const source = (catalog: SourceSkill[], overrides: Partial<Source> = {}): Source => ({ id: "source", name: "Source", repository: "owner/repo", ref: "main", subpath: ".", mode: "selected", revision: "latest", refreshedAt: "", lastError: "", catalog, ...overrides });

test("optional uninstalled skills do not make a selected source look out of date", () => {
  const selected = source([entry("installed"), entry("optional"), entry("new-optional", { new: true })]);
  const entries = catalogEntries(selected, [local("installed")]);
  assert.equal(summarizeSource(entries).installs, 2);
  assert.equal(sourceStatus(selected, entries).label, "已同步");
  assert.equal(sourceStatus(selected, entries).tone, "");
  assert.deepEqual(sourceStatus({ ...selected, mode: "all" }, entries), { label: "{0} 个待安装", tone: "amber", count: 2 });
  const updates = catalogEntries(selected, [local("installed", { contentHash: "old" })]);
  assert.deepEqual(sourceStatus(selected, updates), { label: "{0} 个可更新", tone: "amber", count: 1 });
  assert.equal(sourceStatus(selected, catalogEntries(selected, [])).label, "未安装");
});

test("source errors, skipped content and installed removals remain explicit", () => {
  const selected = source([entry("removed", { removed: true }), entry("optional")]);
  const entries = catalogEntries(selected, [local("removed")]);
  assert.deepEqual(sourceStatus(selected, entries), { label: "{0} 个上游移除", tone: "amber", count: 1 });
  assert.equal(sourceStatus({ ...selected, lastError: "network failure" }, entries).label, "刷新失败");
  assert.equal(sourceStatus({ ...selected, revision: "" }, entries).label, "尚未刷新");
  const skipped = catalogEntries(source([entry("bad", { skipReason: "unsafe" })]), []);
  assert.equal(sourceStatus(selected, skipped).label, "部分刷新");
});

test("hover details distinguish installed skills from optional and subscribed installs", () => {
  const selected = source([entry("installed"), entry("optional"), entry("another")]);
  const entries = catalogEntries(selected, [local("installed")]);
  const details = sourceStatusDetails(selected, entries);
  assert.deepEqual(details, [
    { key: "已安装的 {0} 个技能与当前清单一致。", count: 1 },
    { key: "另有 {0} 个技能可按需安装。", count: 2 },
  ]);
  assert.deepEqual(sourceStatusDetails({ ...selected, mode: "all" }, entries)[1], { key: "全部订阅中还有 {0} 个技能待安装。", count: 2 });
});

test("hover details cover simultaneous issues without presenting skipped content as synced", () => {
  const selected = source([entry("update"), entry("removed", { removed: true }), entry("skipped", { skipReason: "unsafe" }), entry("optional")]);
  const entries = catalogEntries(selected, [local("update", { contentHash: "old" }), local("removed"), local("skipped")]);
  const details = sourceStatusDetails(selected, entries);
  assert.deepEqual(details.map((detail) => detail.count), [3, 1, 1, 1, 1]);
  assert.equal(details.some((detail) => detail.key.includes("与当前清单一致")), false);
  const onlySkipped = catalogEntries(source([entry("skipped", { skipReason: "unsafe" })]), [local("skipped")]);
  assert.equal(sourceStatusDetails(selected, onlySkipped).some((detail) => detail.key.includes("与当前清单一致")), false);
  assert.deepEqual(sourceStatusDetails({ ...selected, lastError: "fetch failed" }, entries), [{ key: "来源刷新失败，当前显示上次保存的清单；请刷新成功后再同步。" }]);
  assert.deepEqual(sourceStatusDetails({ ...selected, revision: "" }, entries), [{ key: "刷新来源后，才能检查已安装技能并浏览可安装技能。" }]);
});

test("partial refresh keeps skipped skills separate from installs, updates and removals", () => {
  const data = source([
    entry("skipped-installed", { contentHash: "old", revision: "old", skipReason: "symlink is not supported" }),
    entry("skipped-new", { contentHash: "", revision: "", skipReason: "missing YAML frontmatter" }),
    entry("update"), entry("install", { new: true }), entry("removed", { removed: true }), entry("current"),
  ]);
  const installed = [local("skipped-installed", { contentHash: "older", upstreamRemoved: true }), local("update", { contentHash: "old" }), local("removed"), local("current")];
  const summary = summarizeSource(catalogEntries(data, installed));
  assert.deepEqual({ installs: summary.installs, changed: summary.changed, removed: summary.removed, skipped: summary.skipped }, { installs: 1, changed: 1, removed: 1, skipped: 2 });
  assert.deepEqual(summary.available.map((item) => item.item.subpath), ["install", "update"]);
  const skipped = summary.entries.filter((item) => item.kind === "skipped");
  assert.equal(skipped[0].local?.id, "skipped-installed");
  assert.equal(skipped[0].item.revision, "old");
  assert.equal(skipped[1].local, undefined);
});

test("both selection modes exclude skipped skills and stale selections from sync", () => {
  const data = source([entry("skipped", { skipReason: "unsafe content" }), entry("install"), entry("update"), entry("removed", { removed: true }), entry("current")]);
  const entries = catalogEntries(data, [local("update", { contentHash: "old" }), local("removed"), local("current")]);
  const selection = new Set(["skipped", "install", "removed", "current", "stale"]);
  assert.deepEqual(sourceSyncPaths(data, entries, selection), ["install"]);
  assert.deepEqual(sourceSyncPaths({ ...data, mode: "all" }, entries, new Set()), ["install", "update"]);
  assert.deepEqual(sourceSyncPaths({ ...data, lastError: "fetch failed" }, entries, selection), []);
  assert.deepEqual(sourceSyncPaths({ ...data, mode: "all", lastError: "fetch failed" }, entries, selection), []);
});

test("imported description differences stay current while content and name changes remain updates", () => {
  const data = source([entry("metadata-only"), entry("content-change"), entry("name-change", { name: "renamed" })]);
  const installed = [
    local("metadata-only", { description: "Description preserved from the legacy database", revision: "older" }),
    local("content-change", { contentHash: "old" }),
    local("name-change"),
  ];
  const entries = catalogEntries(data, installed);
  assert.equal(entries.find((entry) => entry.item.subpath === "metadata-only")?.kind, "current");
  assert.equal(summarizeSource(entries).changed, 2);
  const selection = new Set(installed.map((skill) => skill.subpath));
  assert.deepEqual(sourceSyncPaths(data, entries, selection).sort(), ["content-change", "name-change"]);
  assert.deepEqual(sourceSyncPaths({ ...data, mode: "all" }, entries, new Set()).sort(), ["content-change", "name-change"]);
  assert.equal(installed[0].description, "Description preserved from the legacy database");
});

test("a corrected skill becomes selectable on the next successful refresh", () => {
  const installed = [local("recovered", { contentHash: "old" })];
  const invalid = source([entry("recovered", { skipReason: "invalid YAML frontmatter" })]);
  assert.equal(catalogEntries(invalid, installed)[0].kind, "skipped");
  const corrected = source([entry("recovered")]);
  assert.equal(catalogEntries(corrected, installed)[0].kind, "update");
  assert.deepEqual(sourceSyncPaths(corrected, catalogEntries(corrected, installed), new Set(["recovered"])), ["recovered"]);
});

test("skipped status takes precedence over retained removal flags and old snapshots remain valid", () => {
  const data = source([entry("skipped", { removed: true, skipReason: "unreadable" }), entry("current")]);
  const entries = catalogEntries(data, [local("skipped"), local("current"), local("legacy-removed", { upstreamRemoved: true }), local("foreign", { sourceId: "other" })]);
  assert.equal(entries.find((item) => item.item.subpath === "skipped")?.kind, "skipped");
  assert.equal(entries.find((item) => item.item.subpath === "legacy-removed")?.kind, "removed");
  assert.equal(entries.find((item) => item.item.subpath === "current")?.kind, "current");
  assert.equal(entries.some((item) => item.item.subpath === "foreign"), false);
});

test("distribution copies count as one catalog skill and identical content has one choice", () => {
  const data = source([
    entry(".claude/skills/captions-overlay", { name: "captions-overlay", contentHash: "same" }),
    entry(".agents/skills/captions-overlay", { name: "captions-overlay", contentHash: "same" }),
  ]);
  const entries = catalogEntries(data, []);
  assert.equal(entries.length, 1);
  assert.equal(entries[0].key, ".agents/skills/captions-overlay");
  assert.equal(entries[0].item.subpath, ".agents/skills/captions-overlay");
  assert.equal(entries[0].variants, undefined);
  assert.equal(summarizeSource(entries).installs, 1);
  assert.deepEqual(sourceSyncPaths({ ...data, mode: "all" }, entries, new Set()), [".agents/skills/captions-overlay"]);
});

test("different distributions keep selectable content variants and the installed physical default", () => {
  const canonical = entry(".agents/skills/captions-overlay", { name: "captions-overlay", contentHash: "agents" });
  const alternate = entry(".claude/skills/copy", { name: "captions-overlay", contentHash: "claude" });
  const data = source([alternate, canonical]);
  const installed = [local(alternate.subpath, { name: alternate.name, contentHash: alternate.contentHash })];
  const entries = catalogEntries(data, installed);
  assert.equal(entries.length, 1);
  assert.equal(entries[0].item.subpath, alternate.subpath);
  assert.equal(entries[0].kind, "current");
  assert.equal(entries[0].local?.id, alternate.subpath);
  assert.deepEqual(entries[0].variants?.map((item) => item.subpath), [canonical.subpath]);
  assert.equal(summarizeSource(entries).installed, 1);
  assert.equal(summarizeSource(entries).installs, 0);

  const chosen = catalogEntries(data, installed, "zh-CN", new Map([[canonical.subpath, canonical.subpath]]));
  assert.equal(chosen[0].item.subpath, canonical.subpath);
  assert.equal(chosen[0].kind, "update");
  assert.equal(chosen[0].local?.id, alternate.subpath);
  assert.deepEqual(sourceSyncPaths(data, chosen, new Set([canonical.subpath])), [canonical.subpath]);
  assert.deepEqual(sourceSyncPaths({ ...data, mode: "all" }, chosen, new Set()), [canonical.subpath]);
});

test("identical installed variants stay at their selected path through refresh", () => {
  const data = source([
    entry(".agents/skills/shared", { name: "shared", contentHash: "same" }),
    entry(".claude/skills/shared", { name: "shared", contentHash: "same" }),
  ]);
  const entries = catalogEntries(data, [local(".claude/skills/shared", { name: "shared", contentHash: "same" })]);
  assert.equal(entries[0].item.subpath, ".claude/skills/shared");
  assert.equal(entries[0].kind, "current");
  assert.equal(entries[0].variants, undefined);
});

test("same-tier ambiguity and separate repository scopes remain separate catalog skills", () => {
  const ambiguous = catalogEntries(source([
    entry("skills/first", { name: "shared" }),
    entry("skills/second", { name: "shared" }),
    entry(".agents/skills/shared", { name: "shared" }),
  ]), []);
  assert.equal(ambiguous.length, 3);
  assert.equal(ambiguous.every((entry) => !entry.variants), true);
  const scoped = catalogEntries(source([
    entry("skills/one/shared", { name: "shared" }),
    entry("skills/two/shared", { name: "shared" }),
    entry("tests/.agents/skills/shared", { name: "shared" }),
    entry(".agents/skills/shared", { name: "shared" }),
  ]), []);
  assert.equal(scoped.length, 4);
  const firstSource = source([entry(".agents/skills/shared", { name: "shared" })]);
  const secondSource = source([entry(".claude/skills/shared", { name: "shared" })], { id: "another", repository: "another/repo" });
  const installed = [local(".agents/skills/shared", { name: "shared" })];
  assert.equal(catalogEntries(firstSource, installed)[0].kind, "current");
  assert.equal(catalogEntries(secondSource, installed)[0].kind, "install");
});

test("removed and skipped installed variants are preserved until a usable copy is chosen", () => {
  for (const retained of [{ removed: true }, { skipReason: "unsafe" }]) {
    const canonical = entry(".agents/skills/shared", { name: "shared", contentHash: "same" });
    const alternate = entry(".claude/skills/shared", { name: "shared", contentHash: "same", ...retained });
    const data = source([canonical, alternate]);
    const installed = [local(alternate.subpath, { name: "shared", contentHash: "same", upstreamRemoved: !!alternate.removed })];
    const entries = catalogEntries(data, installed);
    assert.equal(entries.length, 1);
    assert.equal(entries[0].item.subpath, alternate.subpath);
    assert.equal(entries[0].kind, alternate.skipReason ? "skipped" : "removed");
    assert.deepEqual(entries[0].variants?.map((item) => item.subpath), [canonical.subpath]);
    assert.deepEqual(sourceSyncPaths({ ...data, mode: "all" }, entries, new Set()), []);
    const chosen = catalogEntries(data, installed, "zh-CN", new Map([[canonical.subpath, canonical.subpath]]));
    assert.equal(chosen[0].kind, "update");
    assert.deepEqual(sourceSyncPaths(data, chosen, new Set([canonical.subpath])), [canonical.subpath]);
  }
});

test("missing removed physical paths join their retained distribution family", () => {
  const canonical = entry(".agents/skills/shared", { name: "shared", contentHash: "new" });
  const installed = local(".claude/skills/shared", { name: "shared", contentHash: "old", upstreamRemoved: true });
  const entries = catalogEntries(source([canonical]), [installed]);
  assert.equal(entries.length, 1);
  assert.equal(entries[0].kind, "removed");
  assert.equal(entries[0].local?.id, installed.id);
  assert.deepEqual(entries[0].variants?.map((item) => item.subpath), [canonical.subpath]);
});

test("historical duplicate installations remain a single selectable update for consolidation", () => {
  const canonical = entry(".agents/skills/shared", { name: "shared", contentHash: "same" });
  const alternate = entry(".claude/skills/shared", { name: "shared", contentHash: "same" });
  const data = source([canonical, alternate]);
  const installed = [local(canonical.subpath, { name: "shared", contentHash: "same" }), local(alternate.subpath, { name: "shared", contentHash: "same" })];
  const entries = catalogEntries(data, installed);
  assert.equal(entries.length, 1);
  assert.equal(entries[0].kind, "update");
  assert.equal(summarizeSource(entries).installed, 1);
  assert.equal(summarizeSource(entries).changed, 1);
  assert.deepEqual(sourceSyncPaths(data, entries, new Set([canonical.subpath])), [canonical.subpath]);
  assert.deepEqual(sourceSyncPaths({ ...data, mode: "all" }, entries, new Set()), [canonical.subpath]);

  const chosen = catalogEntries(data, installed, "zh-CN", new Map([[canonical.subpath, alternate.subpath]]));
  assert.equal(chosen[0].kind, "update");
  assert.equal(chosen[0].local?.id, alternate.subpath);
  assert.equal(chosen[0].item.subpath, alternate.subpath);
});

test("two same-tier installations remain independent current skills", () => {
  const data = source([entry("skills/first", { name: "shared" }), entry("skills/second", { name: "shared" })]);
  const installed = [local("skills/first", { name: "shared" }), local("skills/second", { name: "shared" })];
  const entries = catalogEntries(data, installed);
  assert.equal(entries.length, 2);
  assert.equal(entries.every((entry) => entry.kind === "current"), true);
  assert.deepEqual(sourceSyncPaths({ ...data, mode: "all" }, entries, new Set()), []);
});
