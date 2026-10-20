type ProjectSummary = { name: string; projectRoot?: string; path: string; paths?: string[] };

function matchesTerms(text: string, query: string) {
  const haystack = text.toLocaleLowerCase();
  return query.trim().toLocaleLowerCase().split(/\s+/).every((term) => haystack.includes(term));
}

export function matchesProject(project: ProjectSummary, query: string, skillNames: string[], presetNames: string[]) {
  return matchesTerms([project.name, project.projectRoot, project.path, ...(project.paths || []), ...skillNames, ...presetNames].join(" "), query);
}

export function matchesProjectSkill(skill: { id: string; name: string; description: string }, query: string, selectedOnly: boolean, selected: ReadonlySet<string>) {
  return (!selectedOnly || selected.has(skill.id)) && matchesTerms(`${skill.name} ${skill.description}`, query);
}

// A skill is current only when every selected directory has the selected name,
// version and deployment mode. An applied copy can still need an update.
export function projectSkillApplied(
  skill: { id: string; name: string; contentHash: string },
  paths: string[],
  deployments: { skillId: string; name: string; contentHash: string; path?: string; mode?: "copy" | "symlink" }[],
  mode: "copy" | "symlink",
) {
  return paths.length > 0 && paths.every((path) => deployments.some((item) => item.skillId === skill.id && item.name === skill.name && item.contentHash === skill.contentHash && (item.path || paths[0]) === path && (item.mode || "copy") === mode));
}

export type ProjectSort = "manual" | "name-asc" | "name-desc" | "updated" | "created";

export function sortProjects<T extends { name: string; createdAt?: string; updatedAt?: string }>(projects: T[], sort: ProjectSort, locale: string): T[] {
  if (sort === "manual") return [...projects];
  const timestamp = (value?: string) => value && Number.isFinite(Date.parse(value)) ? Date.parse(value) : 0;
  return [...projects].sort((a, b) => {
    if (sort === "created" || sort === "updated") return timestamp(sort === "created" ? b.createdAt : b.updatedAt) - timestamp(sort === "created" ? a.createdAt : a.updatedAt);
    const byName = a.name.localeCompare(b.name, locale, { numeric: true, sensitivity: "base" });
    return sort === "name-desc" ? -byName : byName;
  });
}

// Move relative to a visible row while preserving projects hidden by filtering.
export function moveProject(ids: string[], dragged: string, anchor: string, after = false): string[] {
  if (dragged === anchor || !ids.includes(dragged) || !ids.includes(anchor)) return [...ids];
  const result = ids.filter((id) => id !== dragged);
  result.splice(result.indexOf(anchor) + (after ? 1 : 0), 0, dragged);
  return result;
}
