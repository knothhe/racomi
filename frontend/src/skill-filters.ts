import type { Source, Preset, Skill, Target, Update } from "./api";

export type SkillStatusFilter = "all" | "installed" | "updates" | "pending" | "removed" | "error";
export type SkillStatuses = Record<Exclude<SkillStatusFilter, "all">, Set<string>>;
export interface SkillFilters {
  query: string;
  source: string;
  status: SkillStatusFilter;
  sort: "asc" | "desc";
}

export const defaultSkillFilters = (): SkillFilters => ({ query: "", source: "all", status: "all", sort: "asc" });

export function skillSource(skill: Skill, sources: Source[]): Source | undefined {
  return sources.find((source) => source.id === skill.sourceId);
}

export function skillStatusIDs(skills: Skill[], updates: Update[] | null, pending: Set<string>): SkillStatuses {
  const statuses: SkillStatuses = { installed: new Set(), updates: new Set(), pending: new Set(), removed: new Set(), error: new Set() };
  const results = new Map((updates || []).map((update) => [update.skillId, update]));
  for (const skill of skills) {
    const update = results.get(skill.id);
    const removed = !!(skill.upstreamRemoved || update?.removed);
    const failed = !!update?.error;
    const available = !!update?.available && !removed && !failed;
    const unapplied = pending.has(skill.id);
    if (removed) statuses.removed.add(skill.id);
    if (failed) statuses.error.add(skill.id);
    if (available) statuses.updates.add(skill.id);
    if (unapplied) statuses.pending.add(skill.id);
    // Installed is the neutral list badge; actionable states may overlap.
    if (!removed && !failed && !available && !unapplied) statuses.installed.add(skill.id);
  }
  return statuses;
}

// Source membership comes from the saved association, never an origin URL. A
// detached local skill may still retain historical repository metadata.
export function filterSkills(skills: Skill[], sources: Source[], filters: SkillFilters, statuses: SkillStatuses, locale = "zh-CN"): Skill[] {
  const query = filters.query.trim().toLocaleLowerCase(locale);
  return skills.filter((skill) => {
    const source = skillSource(skill, sources);
    const matchesSource = filters.source === "all" || (filters.source === "local" ? !source : filters.source === "linked" ? !!source : source?.id === filters.source);
    const matchesStatus = filters.status === "all" || statuses[filters.status].has(skill.id);
    return matchesSource && matchesStatus && `${skill.name} ${skill.description} ${skill.source} ${source?.name || ""} ${source?.repository || ""}`.toLocaleLowerCase(locale).includes(query);
  }).sort((a, b) => a.name.localeCompare(b.name, locale, { numeric: true }) * (filters.sort === "asc" ? 1 : -1));
}

export function pendingSkills(skills: Skill[], presets: Preset[], targets: Target[], mode: "copy" | "symlink"): Set<string> {
  const result = new Set<string>();
  for (const target of targets) {
    const paths = target.paths?.length ? target.paths : [target.path];
    const desired = new Set([...(target.enabledSkillIds || []), ...presets.filter((preset) => (target.enabledPresetIds || []).includes(preset.id)).flatMap((preset) => preset.skillIds || [])]);
    for (const skill of skills) {
      if (skill.sourceId && target.enabledSourceIds?.includes(skill.sourceId)) desired.add(skill.id);
    }
    const deployments = new Map<string, Target["deployments"]>();
    for (const deployment of target.deployments || []) deployments.set(deployment.skillId, [...(deployments.get(deployment.skillId) || []), deployment]);
    for (const skill of skills) {
      const owned = deployments.get(skill.id) || [];
      const matches = (deployment: Target["deployments"][number]) => deployment.name === skill.name && deployment.contentHash === skill.contentHash && (deployment.mode || "copy") === mode;
      const missing = desired.has(skill.id) && paths.some((path) => !owned.some((deployment) => (deployment.path || target.path) === path && matches(deployment)));
      const obsolete = owned.some((deployment) => !desired.has(skill.id) || !paths.includes(deployment.path || target.path) || !matches(deployment));
      if (missing || obsolete) result.add(skill.id);
    }
  }
  return result;
}
