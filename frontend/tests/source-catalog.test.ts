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
