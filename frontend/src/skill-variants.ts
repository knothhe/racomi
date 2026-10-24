// Keep this order and family matching in sync with internal/skills/distributions.go.
const distributionDirectories = [
  "skills", "skills/.curated", "skills/.experimental", "skills/.system",
  ".agents/skills", ".claude/skills", ".cline/skills", ".codebuddy/skills",
  ".codex/skills", ".commandcode/skills", ".continue/skills", ".factory/skills",
  ".github/skills", ".goose/skills", ".grok/skills", ".iflow/skills",
  ".junie/skills", ".kilo/skills", ".kilocode/skills", ".kimchi/skills",
  ".kiro/skills", ".minimax/skills", ".mux/skills", ".neovate/skills",
  ".opencode/skills", ".openhands/skills", ".pi/skills", ".posit/assistant/skills",
  ".qoder/skills", ".roo/skills", ".trae/skills", ".windsurf/skills",
  ".zcode/skills", ".zencoder/skills",
];

function distributionLocation(subpath: string) {
  const parts = subpath.split("/");
  for (let start = 0; start < parts.length; start++) {
    let priority = -1;
    let length = 0;
    for (const [index, directory] of distributionDirectories.entries()) {
      const size = directory.split("/").length;
      const end = start + size;
      if (end >= parts.length || size <= length || parts.slice(start, end).join("/") !== directory) continue;
      priority = index;
      length = size;
    }
    if (priority >= 0) return { scope: parts.slice(0, start).join("/"), parent: parts.slice(start + length, -1).join("/") || ".", tier: distributionDirectories[priority], priority };
  }
  return undefined;
}

export function distributionKey(name: string, subpath: string): string {
  const location = distributionLocation(subpath);
  return location ? `distribution\0${location.scope}\0${location.parent}\0${name}` : `path\0${subpath}`;
}

export function distributionTier(subpath: string): string {
  const location = distributionLocation(subpath);
  return location ? `${location.scope}\0${location.tier}` : `path\0${subpath}`;
}

export function distributionPriority(subpath: string): number {
  return distributionLocation(subpath)?.priority ?? distributionDirectories.length;
}

// A canonical preview includes alternatives only. Retain the requested physical
// path even when its content also appears at the canonical location.
export function selectedVariant<T extends { subpath: string; variants?: T[] }>(item: T, subpath?: string): T {
  return [item, ...(item.variants || [])].find((variant) => variant.subpath === subpath) || item;
}

export function variantOptions<T extends { subpath: string; contentHash?: string; variants?: T[] }>(item: T, preferredPath?: string): T[] {
  const variants = [item, ...(item.variants || [])];
  const preferred = variants.find((variant) => variant.subpath === preferredPath);
  const options: T[] = [];
  const seen = new Set<string>();
  for (const variant of variants) {
    // Unknown content cannot safely be treated as an identical distribution.
    const key = variant.contentHash ? `hash\0${variant.contentHash}` : `path\0${variant.subpath}`;
    if (seen.has(key)) continue;
    seen.add(key);
    options.push(preferred?.contentHash && preferred.contentHash === variant.contentHash ? preferred : variant);
  }
  return options;
}
