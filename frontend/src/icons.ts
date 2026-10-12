// Small, local SVG set shared by navigation, controls and feedback.
// Agent brand artwork is kept separately in agent-icons.ts.
const paths = {
  "layout-auto": '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M9 9v11"/><path d="m14 12-2 2 2 2m3-4 2 2-2 2"/>',
  "layout-side": '<rect x="3" y="4" width="18" height="16" rx="2"/><path class="st-pane" d="M4 5h5v14H4Z" stroke="none"/><path d="M10 4v16"/>',
  "layout-top": '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M7 7h2m3 0h2m3 0h1"/>',
  book: '<path d="M12 6c-3-2-6-2-9-1v15c3-1 6-1 9 1 3-2 6-2 9-1V5c-3-1-6-1-9 1Zm0 0v15"/>',
  settings: '<path d="M10 3h4l.5 2.2 1.7 1 2.2-.6 2 3.4-1.6 1.6v2l1.6 1.6-2 3.4-2.2-.6-1.7 1L14 21h-4l-.5-2.2-1.7-1-2.2.6-2-3.4L5.2 13v-2L3.6 9.4l2-3.4 2.2.6 1.7-1Z"/><circle cx="12" cy="12" r="3"/>',
  copy: '<rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>',
  link: '<path d="M10 13a5 5 0 0 0 7 .1l3-3a5 5 0 0 0-7.1-7.1l-1.7 1.7M14 11a5 5 0 0 0-7-.1l-3 3a5 5 0 0 0 7.1 7.1l1.7-1.7"/>',
  skills: '<rect x="3" y="3" width="6" height="18" rx="1.5"/><rect x="12" y="3" width="9" height="18" rx="1.5"/><path d="M6 7v3M15 7h3M15 11h3"/>',
  compass: '<circle cx="12" cy="12" r="9"/><path d="m16 8-2.5 5.5L8 16l2.5-5.5Z"/>',
  layers: '<path d="m12 3 10 5-10 5L2 8Zm-9 9 9 5 9-5M3 16l9 5 9-5"/>',
  folder: '<path d="M3 7V5a2 2 0 0 1 2-2h4l2 3h8a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/>',
  archive: '<rect x="3" y="3" width="18" height="5" rx="1"/><path d="M5 8v12h14V8M9 12h6"/>',
  code: '<path d="m8 6-6 6 6 6m8-12 6 6-6 6M14 3l-4 18"/>',
  terminal: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m6 8 4 4-4 4m7 0h5"/>',
  "git-branch": '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="5" r="2"/><path d="M6 7v10m12-10a8 8 0 0 1-8 8H6"/>',
  bug: '<rect x="8" y="7" width="8" height="14" rx="4"/><path d="m9 3 1 4m5-4-1 4M8 10H5L3 8m13 2h3l2-2M8 14H3m13 0h5M8 18H5l-2 2m13-2h3l2 2M12 11v10"/>',
  shield: '<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z"/><path d="m8 12 3 3 5-6"/>',
  database: '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.7 4 3 9 3s9-1.3 9-3V5M3 12c0 1.7 4 3 9 3s9-1.3 9-3"/>',
  server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.1M7 17.5h.1M11 6.5h6M11 17.5h6"/>',
  cloud: '<path d="M7 19a5 5 0 1 1 1-9.9A7 7 0 0 1 21 12a3.5 3.5 0 0 1-1.5 7H7Z"/>',
  globe: '<circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18M5 6h14M5 18h14"/>',
  rocket: '<path d="M9 15c-3-6 2-11 12-12 0 10-6 15-12 12Zm0 0-3 3m1-9-4 1-1 5 6-1m6 2-1 6 5-1 1-4M5 17c-2 0-3 2-3 5 3 0 5-1 5-3"/><circle cx="16" cy="8" r="2"/>',
  zap: '<path d="m13 2-10 12h8l-1 8 11-13h-8l1-7Z"/>',
  sparkles: '<path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3ZM20 2v4m-2-2h4M4 18v4m-2-2h4"/>',
  cpu: '<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9" y="9" width="6" height="6" rx="1"/><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4"/>',
  wrench: '<path d="M14 4a6 6 0 0 0-7 7l-5 5a3 3 0 0 0 6 6l5-5a6 6 0 0 0 7-7l-4 4-4-4 4-4Z"/>',
  palette: '<path d="M12 3a9 9 0 1 0 0 18h1a2.5 2.5 0 0 0 1.8-4.2 1.5 1.5 0 0 1 1.1-2.6H18a3 3 0 0 0 3-3C21 6.5 17 3 12 3Z"/><circle cx="7" cy="10" r=".75"/><circle cx="10" cy="6.5" r=".75"/><circle cx="15" cy="7" r=".75"/><circle cx="6.5" cy="15" r=".75"/>',
  pen: '<path d="m14 3 7 7-8 10-10 1 1-10 10-8Zm-11 18 7-7m0 0a2 2 0 1 0 4-4 2 2 0 0 0-4 4ZM12 5l7 7"/>',
  "file-text": '<path d="M14 2H5a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V9l-7-7Zm0 0v7h7M7 13h10M7 17h7"/>',
  chart: '<path d="M3 3v18h18M7 16v-5m5 5V7m5 9V4"/>',
  flask: '<path d="M9 3h6m-5 0v6l-7 11a1 1 0 0 0 1 1h16a1 1 0 0 0 1-1L14 9V3M7 15h10"/>',
  lightbulb: '<path d="M9 18h6m-5 3h4M8 14a7 7 0 1 1 8 0c-1 .8-1 2-1 4H9c0-2 0-3.2-1-4Z"/>',
  target: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1"/>',
  puzzle: '<path d="M9 3H4a1 1 0 0 0-1 1v5h2a3 3 0 1 1 0 6H3v5a1 1 0 0 0 1 1h5v-2a3 3 0 1 1 6 0v2h5a1 1 0 0 0 1-1v-5h-2a3 3 0 1 1 0-6h2V4a1 1 0 0 0-1-1h-5v2a3 3 0 1 1-6 0V3Z"/>',
  heart: '<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0l-1 1-1-1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z"/>',
  users: '<circle cx="9" cy="7" r="4"/><path d="M2 21v-3a7 7 0 0 1 14 0v3M17 3a4 4 0 0 1 0 8m2 3a6 6 0 0 1 3 5v2"/>',
  search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4.5 4.5"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  edit: '<path d="m16 3 5 5M4 20l4-1 13-13a2.1 2.1 0 0 0-3-3L5 16l-1 4Z"/>',
  trash: '<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>',
  arrow: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
  "chevron-down": '<path d="m6 9 6 6 6-6"/>',
  "chevron-right": '<path d="m9 6 6 6-6 6"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>',
  external: '<path d="M14 3h7v7m0-7L10 14M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5"/>',
  restore: '<path d="M3 4v5h5M3.8 9a9 9 0 1 1-.3 5M12 7v5l3 2"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  "check-square": '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="m7 12 3 3 7-7"/>',
  refresh: '<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 6a8 8 0 0 1 13 3M5 15a8 8 0 0 0 13 3"/>',
  close: '<path d="m6 6 12 12M18 6 6 18"/>',
  box: '<path d="m12 3 9 5v10l-9 5-9-5V8Zm0 10 9-5M3 8l9 5v10M7.5 5.5l9 5"/>',
  download: '<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>',
  upload: '<path d="M12 16V4m-5 5 5-5 5 5M4 16v5h16v-5"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M20.5 13a8.5 8.5 0 0 1-9.5-9.5A8.5 8.5 0 1 0 20.5 13Z"/>',
  monitor: '<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8m-4-4v4"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7v.1"/>',
  alert: '<path d="m12 3 10 18H2L12 3Z"/><path d="M12 9v5M12 17v.1"/>',
} as const;

export type IconName = keyof typeof paths;

export const projectIcons = ["folder", "monitor", "layers", "book", "compass", "box", "skills", "archive", "link", "settings"] as const satisfies readonly IconName[];
export const projectIconName = (name?: string) => projectIcons.find((candidate) => candidate === name) || "folder";
export const presetIcons = [
  "layers", ...projectIcons.filter((name) => name !== "layers"),
  "code", "terminal", "git-branch", "bug", "shield", "database", "server", "cloud", "globe", "rocket", "zap", "sparkles",
  "cpu", "wrench", "palette", "pen", "file-text", "chart", "flask", "lightbulb", "target", "puzzle", "heart", "users",
] as const satisfies readonly IconName[];
export const presetIconName = (name?: string) => presetIcons.find((candidate) => candidate === name) || "layers";

// Icons are decorative; the surrounding control supplies its accessible name.
// Keep the fallback for callers that provide a dynamic icon name.
export const icon = (name: string, cls = "") => `<svg class="icon ${cls}" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">${Object.hasOwn(paths, name) ? paths[name as IconName] : paths.box}</svg>`;
