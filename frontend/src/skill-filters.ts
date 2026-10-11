import type { Source, Preset, Skill, Target } from "./api";

export type SkillStatusFilter = "all" | "updates" | "pending";
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

// Source membership comes from the saved association, never an origin URL. A
// detached local skill may still retain historical repository metadata.
export function filterSkills(skills: Skill[], sources: Source[], filters: SkillFilters, updates: Set<string>, pending: Set<string>, locale = "zh-CN"): Skill[] {
  const query = filters.query.trim().toLocaleLowerCase(locale);
  return skills.filter((skill) => {
    const source = skillSource(skill, sources);
    const matchesSource = filters.source === "all" || (filters.source === "local" ? !source : filters.source === "linked" ? !!source : source?.id === filters.source);
    const matchesStatus = filters.status === "all" || (filters.status === "updates" ? updates.has(skill.id) : pending.has(skill.id));
    return matchesSource && matchesStatus && `${skill.name} ${skill.description} ${skill.source} ${source?.name || ""} ${source?.repository || ""}`.toLocaleLowerCase(locale).includes(query);
  }).sort((a, b) => a.name.localeCompare(b.name, locale, { numeric: true }) * (filters.sort === "asc" ? 1 : -1));
}

export function pendingSkills(skills: Skill[], presets: Preset[], targets: Target[], mode: "copy" | "symlink"): Set<string> {
  const result = new Set<string>();
  for (const target of targets) {
    const paths = target.paths?.length ? target.paths : [target.path];
    const desired = new Set([...(target.enabledSkillIds || []), ...presets.filter((preset) => (target.enabledPresetIds || []).includes(preset.id)).flatMap((preset) => preset.skillIds || [])]);
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
