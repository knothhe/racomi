export function homeRelativePath(path: string, homeDir?: string): string {
  const home = homeDir?.replace(/[\\/]+$/, "");
  if (!home) return path;
  const normalizedPath = path.replace(/\\/g, "/");
  const normalizedHome = home.replace(/\\/g, "/");
  if (normalizedPath === normalizedHome) return "~";
  return normalizedPath.startsWith(`${normalizedHome}/`) ? `~${path.slice(home.length)}` : path;
}
