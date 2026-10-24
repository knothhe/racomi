import type { Source, Skill, SourceSkill } from "./api";
import { distributionKey, distributionPriority, distributionTier } from "./skill-variants.ts";

export type SourceEntry = { key?: string; item: SourceSkill; variants?: SourceSkill[]; local?: Skill; kind: "install" | "update" | "current" | "removed" | "skipped" };

export function catalogEntries(source: Source, skills: Skill[], locale = "zh-CN", choices?: ReadonlyMap<string, string>): SourceEntry[] {
  const installed = skills.filter((skill) => skill.sourceId === source.id);
  const catalog = [...(source.catalog || [])];
  for (const local of installed) {
    if (!catalog.some((item) => item.subpath === local.subpath) && local.upstreamRemoved) catalog.push({ ...local, removed: true, new: false });
  }
  const ambiguous = new Set<string>();
  const tiers = new Map<string, Map<string, string>>();
  for (const item of catalog) {
    if (item.removed) continue;
    const key = distributionKey(item.name, item.subpath);
    const tier = distributionTier(item.subpath);
    const paths = tiers.get(key) || new Map<string, string>();
    if (paths.has(tier) && paths.get(tier) !== item.subpath) ambiguous.add(key);
    paths.set(tier, item.subpath);
    tiers.set(key, paths);
  }
  const groups = new Map<string, SourceSkill[]>();
  for (const item of catalog) {
    const family = distributionKey(item.name, item.subpath);
    const key = ambiguous.has(family) ? `path\0${item.subpath}` : family;
    const group = groups.get(key) || [];
    group.push(item);
    groups.set(key, group);
  }
  const entries: SourceEntry[] = [...groups.values()].map((group) => {
    group.sort((a, b) => distributionPriority(a.subpath) - distributionPriority(b.subpath) || a.subpath.localeCompare(b.subpath, "en"));
    const key = group[0].subpath;
    const locals = installed.filter((skill) => group.some((item) => skill.subpath === item.subpath));
    const preferred = choices?.get(key) || locals[0]?.subpath;
    const item = group.find((item) => item.subpath === preferred) || group.find((item) => !item.removed && !item.skipReason) || group[0];
    const local = locals.find((skill) => skill.subpath === item.subpath) || locals[0];
    // Removed/skipped snapshots remain distinct from usable copies, including
    // when they retain the same hash. This lets an explicit choice recover an
    // installed path without silently replacing it on refresh.
    const seen = new Set<string>();
    const options = [item, ...group.filter((variant) => variant !== item)].filter((variant) => {
      const content = variant.contentHash ? `hash\0${variant.contentHash}` : `path\0${variant.subpath}`;
      const identity = `${content}\0${!!variant.removed}\0${variant.skipReason || ""}`;
      if (seen.has(identity)) return false;
      seen.add(identity);
      return true;
    });
    // A skipped item may retain its previous content and revision. It must never
    // be mistaken for an install, update or upstream removal in this refresh.
    // Imported metadata may differ from SKILL.md. Match the backend sync check:
    // content, deployment-name or an explicit physical-variant change requires
    // an update.
    // A sync also consolidates historical duplicate records into the selected
    // variant while preserving their project and preset references.
    const kind = item.skipReason ? "skipped" : item.removed ? "removed" : !local ? "install" : locals.length > 1 || local.contentHash !== item.contentHash || local.name !== item.name || local.subpath !== item.subpath ? "update" : "current";
    return { key, item, local, kind, ...(options.length > 1 ? { variants: options.slice(1) } : {}) };
  });
  return entries.sort((a, b) => Number(a.kind === "removed") - Number(b.kind === "removed") || a.item.name.localeCompare(b.item.name, locale, { numeric: true }));
}

export function summarizeSource(entries: SourceEntry[]) {
  const available = entries.filter((entry) => entry.kind === "install" || entry.kind === "update");
  return {
    entries, available,
    installs: entries.filter((entry) => entry.kind === "install").length,
    changed: entries.filter((entry) => entry.kind === "update").length,
    removed: entries.filter((entry) => entry.kind === "removed" && entry.local).length,
    skipped: entries.filter((entry) => entry.kind === "skipped").length,
    installed: entries.filter((entry) => entry.local).length,
  };
}

// Optional catalog entries are not pending changes for a selected subscription.
export function sourceStatus(source: Source, entries: SourceEntry[]) {
  const summary = summarizeSource(entries);
  if (source.lastError) return { label: "刷新失败", tone: "error", count: 0 };
  if (!source.revision) return { label: "尚未刷新", tone: "", count: 0 };
  if (summary.skipped) return { label: "部分刷新", tone: "amber", count: summary.skipped };
  if (summary.changed) return { label: "{0} 个可更新", tone: "amber", count: summary.changed };
  if (summary.removed) return { label: "{0} 个上游移除", tone: "amber", count: summary.removed };
  if (source.mode === "all" && summary.installs) return { label: "{0} 个待安装", tone: "amber", count: summary.installs };
  return { label: summary.installed ? "已同步" : "未安装", tone: "", count: 0 };
}

export function sourceStatusDetails(source: Source, entries: SourceEntry[]): { key: string; count?: number }[] {
  // Failed refreshes retain the old catalog. Explain its age instead of
  // presenting its counts as the result of the failed request.
  if (source.lastError) return [{ key: "来源刷新失败，当前显示上次保存的清单；请刷新成功后再同步。" }];
  if (!source.revision) return [{ key: "刷新来源后，才能检查已安装技能并浏览可安装技能。" }];
  const summary = summarizeSource(entries);
  const skippedInstalled = entries.some((entry) => entry.kind === "skipped" && entry.local);
  const details: { key: string; count?: number }[] = [];
  if (summary.installed) details.push({ key: !summary.changed && !summary.removed && !skippedInstalled ? "已安装的 {0} 个技能与当前清单一致。" : "此来源已安装 {0} 个技能。", count: summary.installed });
  else details.push({ key: "尚未安装此来源的技能。" });
  if (summary.changed) details.push({ key: "已安装的 {0} 个技能可更新，同步后会更新本地版本。", count: summary.changed });
  if (summary.removed) details.push({ key: "已安装的 {0} 个技能已被上游移除，本地版本仍保留。", count: summary.removed });
  if (summary.skipped) details.push({ key: "本次刷新跳过 {0} 个技能，已有本地版本仍保留。", count: summary.skipped });
  if (summary.installs) details.push({ key: source.mode === "all" ? "全部订阅中还有 {0} 个技能待安装。" : "另有 {0} 个技能可按需安装。", count: summary.installs });
  return details;
}

export function sourceSyncPaths(source: Source, entries: SourceEntry[], selection: Set<string>): string[] {
  if (source.lastError) return [];
  return summarizeSource(entries).available.filter((entry) => source.mode === "all" || selection.has(entry.item.subpath)).map((entry) => entry.item.subpath);
}
