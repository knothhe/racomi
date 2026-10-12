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
