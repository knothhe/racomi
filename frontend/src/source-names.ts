import type { SearchResult, Skill, Source } from "./api";

// Hosted sources use an origin or a well-known catalog, including catalogs
// below a service-specific prefix. Git URLs continue to use their repo path.
export function hostedSourceDomain(source: string): string {
  try {
    const url = new URL(source);
    if (!["https:", "http:"].includes(url.protocol) || url.username || url.password || url.search || url.hash) return "";
    return url.pathname === "/" || /\/\.well-known\/(?:agent-)?skills(?:\/index\.json)?\/?$/.test(url.pathname)
      ? url.hostname
      : "";
  } catch {
    return "";
  }
}

export function hubResultInstalled(result: SearchResult, skills: Pick<Skill, "name" | "source" | "subpath">[]): boolean {
  const slug = result.slug.split("/").filter(Boolean).at(-1) || result.name;
  return skills.some((skill) => {
    const sameSkill = skill.subpath.split("/").filter(Boolean).at(-1) === slug || skill.name === slug || skill.name === result.name;
    if (!sameSkill) return false;
    const hosted = hostedSourceDomain(skill.source);
    return hosted
      ? hosted === result.source.toLowerCase() || hosted === hostedSourceDomain(result.source)
      : skill.source === result.source || repositoryName(skill.source) === repositoryName(result.source) && !!repositoryName(result.source);
  });
}

// Saved sources can contain a normalized URL, an SSH address, or the original
// owner/repo shorthand. Local paths have no remote namespace to display.
export function repositoryName(repository: string): string {
  const value = repository.trim();
  if (!value || /^(?:[./~]|[A-Za-z]:[\\/])/.test(value)) return "";
  let path = "";
  if (value.includes("://")) {
    try {
      const url = new URL(value);
      if (!["https:", "http:", "ssh:", "git:"].includes(url.protocol)) return "";
      path = url.pathname;
    } catch {
      return "";
    }
  } else {
    const ssh = value.match(/^[^/@:\s]+@[^/:\s]+:(.+)$/);
    if (ssh) path = ssh[1];
    else if (/^[A-Za-z0-9][A-Za-z0-9._-]*\/[A-Za-z0-9][A-Za-z0-9._-]*$/.test(value)) path = value;
    else return "";
  }
  const parts = path.replace(/^\/+|\/+$/g, "").split("/");
  if (parts.length < 2 || parts.some((part) => !part || part === "." || part === "..")) return "";
  parts[parts.length - 1] = parts[parts.length - 1].replace(/\.git$/i, "");
  return parts[parts.length - 1] ? parts.join("/") : "";
}

export function sourceDisplayName(source: Pick<Source, "name" | "repository">): string {
  const hosted = hostedSourceDomain(source.repository);
  const repository = hosted || repositoryName(source.repository);
  const name = source.name.trim();
  if (!repository) return name || source.repository;
  const basename = repository.split("/").at(-1);
  // Keep custom names visible, while replacing the old repo-only default name.
  return !name || name === basename || name === repository || name === source.repository.trim() || !!hosted && ["skills", "agent-skills", "index.json"].includes(name)
    ? repository
    : `${repository} · ${name}`;
}
