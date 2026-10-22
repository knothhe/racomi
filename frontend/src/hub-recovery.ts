const missingSkillError = /^skills\.sh skill "(?:\\.|[^"\\])*" was not found in the current repository revision; the index may be outdated or the skill may have been renamed or removed; preview the repository to see available skills$/;
const repositoryPart = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const skillPart = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

// Recovery previews the repository behind a Git-backed Hub link. Validate the
// original URL before parsing because URL() normalizes ports and dot segments.
// The caller sends that original URL to the backend; this only decides whether
// to offer recovery and supplies its display name from the URL's final segment.
export function gitBackedHubSkill(source: string): { repository: string; skill: string } | null {
  if (/[\s\\%]/.test(source)) return null;
  const input = source.startsWith("skills.sh/") ? `https://${source}` : source;
  const raw = input.match(/^https:\/\/(skills\.sh|www\.skills\.sh)(\/[^?#]*)$/);
  if (!raw) return null;
  try {
    const url = new URL(input);
    if (url.protocol !== "https:" || url.username || url.password || url.port || url.search || url.hash || url.pathname !== raw[2]) return null;
    const parts = url.pathname.split("/");
    if (parts.length !== 4 || parts[0] !== "" || !repositoryPart.test(parts[1]) || !repositoryPart.test(parts[2]) || !skillPart.test(parts[3])) return null;
    // /site/example.com/skill identifies a hosted catalog, while the GitHub
    // owner named "site" remains valid with an ordinary repository name.
    const repositoryIsDomain = parts[2].length <= 253 && parts[2].includes(".") && parts[2].split(".").every((label) => label.length <= 63 && /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(label));
    if (parts[1] === "site" && repositoryIsDomain) return null;
    return { repository: `${parts[1]}/${parts[2]}`.toLowerCase(), skill: parts[3] };
  } catch {
    return null;
  }
}

export function recoverableHubSkill(source: string, error: string): string {
  return missingSkillError.test(error) && !/[\r\n]/.test(error) ? gitBackedHubSkill(source)?.skill || "" : "";
}
