import type { Source, Skill, SourceSkill } from "./api";

export type SourceEntry = { item: SourceSkill; local?: Skill; kind: "install" | "update" | "current" | "removed" | "skipped" };

export function catalogEntries(source: Source, skills: Skill[], locale = "zh-CN"): SourceEntry[] {
  const installed = skills.filter((skill) => skill.sourceId === source.id);
  const entries: SourceEntry[] = (source.catalog || []).map((item) => {
    const local = installed.find((skill) => skill.subpath === item.subpath);
    // A skipped item may retain its previous content and revision. It must never
    // be mistaken for an install, update or upstream removal in this refresh.
    // Imported metadata may differ from SKILL.md. Match the backend sync check:
    // only a content or deployment-name change requires an update.
    const kind = item.skipReason ? "skipped" : item.removed ? "removed" : !local ? "install" : local.contentHash !== item.contentHash || local.name !== item.name ? "update" : "current";
    return { item, local, kind };
  });
  for (const local of installed) {
    if (!entries.some((entry) => entry.item.subpath === local.subpath) && local.upstreamRemoved) entries.push({ item: { ...local, removed: true, new: false }, local, kind: "removed" });
  }
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
  };
}

export function sourceSyncPaths(source: Source, entries: SourceEntry[], selection: Set<string>): string[] {
  if (source.lastError) return [];
  return summarizeSource(entries).available.filter((entry) => source.mode === "all" || selection.has(entry.item.subpath)).map((entry) => entry.item.subpath);
}
